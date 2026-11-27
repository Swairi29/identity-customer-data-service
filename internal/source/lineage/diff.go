/*
 * Copyright (c) 2026, WSO2 LLC. (http://www.wso2.com).
 *
 * WSO2 LLC. licenses this file to you under the Apache License,
 * Version 2.0 (the "License"); you may not use this file except
 * in compliance with the License.
 * You may obtain a copy of the License at
 *
 * http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing,
 * software distributed under the License is distributed on an
 * "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY
 * KIND, either express or implied.  See the License for the
 * specific language governing permissions and limitations
 * under the License.
 */

// Package lineage works out which attributes of a profile a write changed, for its attribute history.
package lineage

import (
	"encoding/json"
	"reflect"
	"sort"

	"github.com/wso2/identity-customer-data-service/internal/source/model"
)

// The attribute groups the history covers. application_data is not tracked.
const (
	IdentityAttributes = "identity_attributes"
	Traits             = "traits"
)

// Attributes are the tracked attribute groups of a profile.
type Attributes struct {
	IdentityAttributes map[string]any
	Traits             map[string]any
}

// Change is one attribute a write changed, and how.
type Change struct {
	// Attribute is the profile schema attribute, such as traits.email. It is the group and a top-level key
	// of it: a complex attribute such as traits.address is one attribute, its sub-attributes are not apart.
	Attribute string
	// Kind is model.ActionSet or model.ActionRemoved; merges add model.ActionMerged themselves.
	Kind string
}

// Diff returns the attributes whose value differs between before and after, sorted by attribute:
//   - SET: the attribute has a value after, and had none or another one before
//   - REMOVED: the attribute had a value before and has none after
//
// An attribute with the same value before and after is not listed, so a write that sends values the
// profile already has changes nothing. A null value counts as no value. Values are compared as JSON, so
// 1 and 1.0 are the same, and so are two objects with their keys in another order; the order of the items
// of an array counts. For a new profile, before is empty.
func Diff(before, after Attributes) []Change {
	var changes []Change
	changes = appendGroup(changes, IdentityAttributes, before.IdentityAttributes, after.IdentityAttributes)
	changes = appendGroup(changes, Traits, before.Traits, after.Traits)
	sort.Slice(changes, func(i, j int) bool { return changes[i].Attribute < changes[j].Attribute })
	return changes
}

func appendGroup(changes []Change, group string, before, after map[string]any) []Change {
	keys := map[string]bool{}
	for key := range before {
		keys[key] = true
	}
	for key := range after {
		keys[key] = true
	}
	for key := range keys {
		old, hadValue := valueOf(before, key)
		current, hasValue := valueOf(after, key)
		switch {
		case hasValue && (!hadValue || !reflect.DeepEqual(old, current)):
			changes = append(changes, Change{Attribute: group + "." + key, Kind: model.ActionSet})
		case hadValue && !hasValue:
			changes = append(changes, Change{Attribute: group + "." + key, Kind: model.ActionRemoved})
		}
	}
	return changes
}

// valueOf returns the value of the key in its JSON form, and whether it has one. A missing key, a null and
// a typed nil (such as a nil map, which is stored as null) have none.
func valueOf(group map[string]any, key string) (any, bool) {
	value, ok := group[key]
	if !ok || value == nil {
		return nil, false
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		// A value that cannot be encoded cannot be stored either; compare it as it is.
		return value, true
	}
	var normalized any
	if err := json.Unmarshal(encoded, &normalized); err != nil {
		return value, true
	}
	return normalized, normalized != nil
}
