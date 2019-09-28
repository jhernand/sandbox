/*
Copyright (c) 2019 Red Hat, Inc.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

  http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package internal

import (
	"bytes"
	"fmt"
	"text/template"
)

// Template processes the given template using as data the set of name value pairs that are given as
// arguments. For example, to the following code:
//
//	result, err := Template(`
//              {
//			"name": "{{ .Name }}",
//			"flavour": {
//				"id": "{{ .Flavour }}"
//			}
//		}
//		`,
//		"Name", "mycluster",
//		"Flavour", "4",
//	)
//
// Produces the following result:
//
//      {
//              "name": "mycluster",
//              "flavour": {
//                      "id": "4"
//              }
//      }
func Template(source string, args ...interface{}) (result string, err error) {
	// Check that there is an even number of args, and that the first of each pair is an string:
	count := len(args)
	if count%2 != 0 {
		err = fmt.Errorf(
			"template '%s' should have an even number of arguments, but it has %d",
			source, count,
		)
		return
	}
	for i := 0; i < count; i = i + 2 {
		name := args[i]
		_, ok := name.(string)
		if !ok {
			err = fmt.Errorf(
				"argument %d of template '%s' is a key, so it should be a string, "+
					"but its type is %T",
				i, source, name,
			)
			return
		}
	}

	// Put the variables in the map that will be passed as the data object for the execution of
	// the template:
	data := make(map[string]interface{})
	for i := 0; i < count; i = i + 2 {
		name := args[i].(string)
		value := args[i+1]
		data[name] = value
	}

	// Parse the template:
	tmpl, err := template.New("").Parse(source)
	if err != nil {
		err = fmt.Errorf("can't parse template '%s': %v", source, err)
		return
	}

	// Execute the template:
	buffer := new(bytes.Buffer)
	err = tmpl.Execute(buffer, data)
	if err != nil {
		err = fmt.Errorf("can't execute template '%s': %v", source, err)
		return
	}

	// Return the generated text:
	result = buffer.String()

	return
}
