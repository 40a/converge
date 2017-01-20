// Copyright © 2017 Asteris, LLC
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

package main

import (
	"bytes"
	"fmt"
	"sort"

	"github.com/asteris-llc/converge/resource/systemd/unit"
)

// unitTypeSlices - because sort.Sort requires that we implement sort.Interface
// to sort arbitary slices
type unitTypeSlice []unit.UnitType

// Len returns the length of the slice
func (u unitTypeSlice) Len() int {
	return len(u)
}

// Less returns true of i < j, and false otherwise
func (u unitTypeSlice) Less(i, j int) bool {
	return u[i] < u[j]
}

// Swap swaps the values at indexes i and j, in place
func (u unitTypeSlice) Swap(i, j int) {
	u[i] ^= u[j]
	u[j] ^= u[i]
	u[i] ^= u[j]
}

const copyrightNotice = `// Copyright © 2017 Asteris, LLC
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

`

const autoGeneratedNotice = `// Code generated by generate-dbus-wrappers
// DO NOT EDIT!
`

const packageName = "unit"

func main() {
	l := &unit.LinuxExecutor{unit.CoreosDbus{}}
	u, err := l.ListUnits()
	if err != nil {
		fmt.Println(err)
		return
	}

	unitMap := make(map[unit.UnitType][]*unit.Unit)
	for _, st := range u {
		var lst []*unit.Unit
		if oldLst, ok := unitMap[st.Type]; ok {
			lst = oldLst
		} else {
			lst = make([]*unit.Unit, 0)
		}
		unitMap[st.Type] = append(lst, st)
	}

	fmt.Println(copyrightNotice)
	fmt.Println(autoGeneratedNotice)
	fmt.Printf("package %s\n", packageName)

	fmt.Println(generateBindingsFromMap("Properties", unitMap[unit.UnitTypeService][0].Properties))

	unitSorted := unitTypeSlice{}
	for k := range unitMap {
		unitSorted = append(unitSorted, k)
	}
	sort.Sort(unitSorted)
	for _, u := range unitSorted {
		structName := fmt.Sprintf("%sTypeProperties", u.UnitTypeString())
		properties := generateBindingsFromMap(structName, unitMap[u][0].TypeProperties)
		fmt.Println(properties)
	}

}

func generateBindingsFromMap(structName string, m map[string]interface{}) string {
	funcDef := `
// new%sFromMap generates a new %s given a systemd property map
func (s %s) new%sFromMap(m map[string]interface{}) *%s {
`
	structDef := `
// %s wraps the dbus api for system with properties from this unit type
type %s struct {
`
	elemDef := "\t%s %T `export:\"%s\"`\n"
	assignDef := `
	if val, ok := m["%s"]; ok {
		s.%s = val.(%T)
	}
`

	var (
		sortedKeys  []string
		structLines bytes.Buffer
		funcLines   bytes.Buffer
	)

	for k := range m {
		sortedKeys = append(sortedKeys, k)
	}
	sort.Strings(sortedKeys)

	structLines.WriteString(fmt.Sprintf(structDef, structName, structName))
	funcLines.WriteString(fmt.Sprintf(funcDef, structName, structName, structName, structName, structName))

	for _, k := range sortedKeys {
		v := m[k]
		structLines.WriteString(fmt.Sprintf(elemDef, k, v, k))
		funcLines.WriteString(fmt.Sprintf(assignDef, k, k, v))
	}
	structLines.WriteString("}\n")
	funcLines.WriteString("\treturn s\n}\n")
	return structLines.String() + "\n" + funcLines.String()
}
