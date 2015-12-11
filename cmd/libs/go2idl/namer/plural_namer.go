/*
Copyright 2015 The Kubernetes Authors All rights reserved.

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

package namer

import "k8s.io/kubernetes/cmd/libs/go2idl/types"

type pluralNamer struct {
	exceptions map[string]string
	finalize   func(string) string
}

// NewPluralNamer returns a namer that returns the plural form of the input
// type's name.
func NewPluralNamer(exceptions map[string]string, public bool) *pluralNamer {
	var finalize func(string) string
	if public {
		finalize = IC
	} else {
		finalize = IL
	}
	return &pluralNamer{exceptions, finalize}
}

// Name returns the plural form of the type's name. If the type's name is found
// in the exceptions map, the map value is returned.
func (r *pluralNamer) Name(t *types.Type) string {
	singular := t.Name.Name
	var plural string
	var ok bool
	if plural, ok = r.exceptions[singular]; ok {
		return r.finalize(plural)
	}
	switch string(singular[len(singular)-1]) {
	case "s", "x":
		plural = singular + "es"
	case "y":
		plural = singular[:len(singular)-1] + "ies"
	default:
		plural = singular + "s"
	}
	return r.finalize(plural)
}
