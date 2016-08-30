// Copyright © 2016 Asteris, LLC
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package container

import (
	"fmt"
	"path"
	"sort"
	"strconv"
	"strings"

	"github.com/asteris-llc/converge/resource"
	"github.com/asteris-llc/converge/resource/docker"
	mapset "github.com/deckarep/golang-set"
	dc "github.com/fsouza/go-dockerclient"
	"github.com/pkg/errors"
)

// Container is responsible for running docker containers
type Container struct {
	Name            string
	Image           string
	Entrypoint      string
	Command         string
	WorkingDir      string
	Env             []string
	Expose          []string
	Links           []string
	PortMappings    []string
	PublishAllPorts bool
	client          docker.APIClient
}

// Check that a docker container with the specified configuration is running
func (c *Container) Check() (resource.TaskStatus, error) {
	status := &resource.Status{Status: c.Name}

	container, err := c.client.FindContainer(c.Name)
	if err != nil {
		status.WarningLevel = resource.StatusFatal
		return status, err
	}

	if container != nil {
		c.diffContainer(container, status)
	} else {
		status.AddDifference("name", "", c.Name, "<container-missing>")
	}

	if resource.AnyChanges(status.Differences) {
		status.WillChange = true
		status.WarningLevel = resource.StatusWillChange
	}

	return status, nil
}

// Apply starts a docker container with the specified configuration
func (c *Container) Apply() error {
	config := &dc.Config{
		Image:        c.Image,
		WorkingDir:   c.WorkingDir,
		Env:          c.Env,
		ExposedPorts: toPortMap(c.Expose),
	}

	hostConfig := &dc.HostConfig{
		PublishAllPorts: c.PublishAllPorts,
		Links:           c.Links,
		PortBindings:    c.portBindingMap(),
	}

	if c.Command != "" {
		config.Cmd = strings.Split(c.Command, " ")
	}

	if c.Entrypoint != "" {
		config.Entrypoint = strings.Split(c.Entrypoint, " ")
	}

	opts := dc.CreateContainerOptions{
		Name:       c.Name,
		Config:     config,
		HostConfig: hostConfig,
	}
	_, err := c.client.CreateContainer(opts)

	if err != nil {
		return errors.Wrapf(err, "failed to run container %s", c.Name)
	}
	return nil
}

// SetClient injects a docker api client
func (c *Container) SetClient(client docker.APIClient) {
	c.client = client
}

func (c *Container) diffContainer(container *dc.Container, status *resource.Status) error {
	status.AddDifference("name", strings.TrimPrefix(container.Name, "/"), c.Name, "")
	status.AddDifference("status", container.State.Status, "running", "")
	if container.HostConfig != nil {
		status.AddDifference(
			"publish_all_ports",
			strconv.FormatBool(container.HostConfig.PublishAllPorts),
			strconv.FormatBool(c.PublishAllPorts),
			"false")
	}

	image, err := c.client.FindImage(container.Image)
	if err != nil {
		status.WarningLevel = resource.StatusFatal
		return errors.Wrapf(err, "failed to find image %s for container %s", container.Image, container.Name)
	}

	var actual, expected string
	// if Cmd is empty, compare using the default from the container Image
	actual = strings.Join(container.Config.Cmd, " ")
	if c.Command == "" {
		expected = strings.Join(image.Config.Cmd, " ")
	} else {
		expected = c.Command
	}
	status.AddDifference("command", actual, expected, "")

	// if Entrypoint is empty, compare using the default from the container Image
	actual = strings.Join(container.Config.Entrypoint, " ")
	if c.Entrypoint == "" {
		expected = strings.Join(image.Config.Entrypoint, " ")
	} else {
		expected = c.Entrypoint
	}
	status.AddDifference("entrypoint", actual, expected, "")

	// if WorkingDir is empty, compare using the default from the container Image
	actual = container.Config.WorkingDir
	if c.WorkingDir == "" {
		expected = image.Config.WorkingDir
	} else {
		expected = c.WorkingDir
	}
	status.AddDifference("working_dir", actual, expected, "")

	// Env
	actual, expected = c.compareEnv(container, image)
	status.AddDifference("env", actual, expected, "")

	// Ports
	actual, expected = c.comparePortMappings(container)
	status.AddDifference("ports", actual, expected, "")

	// Expose
	actual, expected = c.compareExposedPorts(container, image)
	status.AddDifference("expose", actual, expected, "")

	// Links
	actual, expected = c.compareLinks(container)
	status.AddDifference("links", actual, expected, "")

	// Image
	existingRepoTag := preferredRepoTag(c.Image, image)
	status.AddDifference("image", existingRepoTag, c.Image, "")

	return nil
}

func (c *Container) compareEnv(container *dc.Container, image *dc.Image) (actual, expected string) {
	toSet := func(env []string) mapset.Set {
		set := mapset.NewSet()
		for _, envvar := range env {
			set.Add(envvar)
		}
		return set
	}

	containerSet := toSet(container.Config.Env)
	imageSet := toSet(image.Config.Env)
	// we don't want to include the default image env in the diff to reduce noise
	compareSet := containerSet.Difference(imageSet)
	expectedSet := toSet(c.Env)

	actual = joinStringSet(compareSet, " ")
	expected = joinStringSet(expectedSet, " ")

	return actual, expected
}

func (c *Container) compareExposedPorts(container *dc.Container, image *dc.Image) (actual, expected string) {
	toSet := func(portMap map[dc.Port]struct{}) mapset.Set {
		portSlice := make([]string, len(portMap))
		idx := 0
		for port := range portMap {
			portSlice[idx] = fmt.Sprintf("%s/%s", port.Port(), port.Proto())
			idx++
		}
		return toStringSet(portSlice)
	}

	containerSet := toSet(container.Config.ExposedPorts)
	imageSet := toSet(image.Config.ExposedPorts)
	expectedSet := toSet(toPortMap(c.Expose)).Union(imageSet)

	actual = joinStringSet(containerSet, ", ")
	expected = joinStringSet(expectedSet, ", ")

	return actual, expected
}

func (c *Container) comparePortMappings(container *dc.Container) (actual, expected string) {
	if container.HostConfig == nil {
		return "", ""
	}

	toBindingsList := func(bindings map[dc.Port][]dc.PortBinding) []string {
		var containerBindings []string
		for port, pbindings := range bindings {
			proto := port.Proto()
			cport := fmt.Sprintf("%s/%s", port.Port(), proto)
			for _, pbinding := range pbindings {
				ip := pbinding.HostIP
				hport := pbinding.HostPort
				if hport != "" {
					hport = strings.Split(hport, "/")[0]
					hport = fmt.Sprintf("%s/%s", hport, proto)
				}
				containerBindings = append(containerBindings, fmt.Sprintf("%s:%s:%s", ip, hport, cport))
			}
		}

		sort.Strings(containerBindings)
		return containerBindings
	}

	actual = strings.Join(toBindingsList(container.HostConfig.PortBindings), ", ")
	expected = strings.Join(toBindingsList(c.portBindingMap()), ", ")

	return actual, expected
}

func (c *Container) compareLinks(container *dc.Container) (actual, expected string) {
	normalizeLink := func(link string) string {
		// internally links are stored as "/linkedcontainername:/containername/alias"
		parts := strings.Split(link, ":")
		if len(parts) == 1 {
			return strings.TrimPrefix(link, "/")
		}

		var name, alias string
		if strings.HasPrefix(parts[0], "/") {
			_, alias = path.Split(parts[1])
			name = parts[0][1:]
		} else {
			name = parts[0]
			alias = parts[1]
		}

		if strings.EqualFold(name, alias) {
			return name
		}

		return fmt.Sprintf("%s:%s", name, alias)
	}

	normalizedLinks := func(rawlinks []string) []string {
		links := make([]string, len(rawlinks))
		for i, link := range rawlinks {
			links[i] = normalizeLink(link)
		}
		return links
	}

	if container.HostConfig == nil {
		return "", ""
	}

	actualSet := toStringSet(normalizedLinks(container.HostConfig.Links))
	expectedSet := toStringSet(c.Links)

	actual = joinStringSet(actualSet, ", ")
	expected = joinStringSet(expectedSet, ", ")

	return actual, expected
}

func (c *Container) portBindingMap() map[dc.Port][]dc.PortBinding {
	// TODO: error if passed an invalid string?
	bindings := make(map[dc.Port][]dc.PortBinding)
	for _, mapping := range c.PortMappings {
		parts := strings.Split(mapping, ":")
		partslen := len(parts)
		switch {
		case partslen == 1:
			cport := dc.Port(parts[0])
			cport = dc.Port(fmt.Sprintf("%s/%s", cport.Port(), cport.Proto()))
			bindings[cport] = append(bindings[cport], dc.PortBinding{})
		case partslen == 2:
			hport := parts[0]
			cport := dc.Port(parts[1])
			cport = dc.Port(fmt.Sprintf("%s/%s", cport.Port(), cport.Proto()))
			bindings[cport] = append(bindings[cport], dc.PortBinding{HostPort: hport})
		case partslen > 2:
			ip := parts[0]
			hport := parts[1]
			cport := dc.Port(parts[2])
			cport = dc.Port(fmt.Sprintf("%s/%s", cport.Port(), cport.Proto()))
			bindings[cport] = append(bindings[cport], dc.PortBinding{HostPort: hport, HostIP: ip})
		}
	}
	return bindings
}

func toStringSet(strings []string) mapset.Set {
	set := mapset.NewSet()
	for _, val := range strings {
		set.Add(val)
	}
	return set
}

func joinStringSet(set mapset.Set, sep string) string {
	list := set.ToSlice()
	strlist := make([]string, len(list))
	for i, val := range list {
		strlist[i] = val.(string)
	}
	sort.Strings(strlist)
	return strings.Join(strlist, sep)
}

func toPortMap(portList []string) map[dc.Port]struct{} {
	portMap := make(map[dc.Port]struct{}, len(portList))
	for _, e := range portList {
		port := dc.Port(e)
		port = dc.Port(fmt.Sprintf("%s/%s", port.Port(), port.Proto()))
		portMap[port] = struct{}{}
	}
	return portMap
}

func preferredRepoTag(want string, image *dc.Image) string {
	// look for the matching repo tag in case an image has multiple tags. if there
	// is no matching, return the first if it has one
	var existingRepoTag string
	if len(image.RepoTags) > 0 {
		for _, repoTag := range image.RepoTags {
			if strings.EqualFold(want, repoTag) {
				existingRepoTag = repoTag
				break
			}
		}

		if existingRepoTag == "" {
			existingRepoTag = image.RepoTags[0]
		}
	}

	return existingRepoTag
}
