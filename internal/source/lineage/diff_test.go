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

package lineage

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/wso2/identity-customer-data-service/internal/source/model"
)

func set(attribute string) Change     { return Change{Attribute: attribute, Kind: model.ActionSet} }
func removed(attribute string) Change { return Change{Attribute: attribute, Kind: model.ActionRemoved} }

func TestANewProfileSetsEveryAttribute(t *testing.T) {
	after := Attributes{
		IdentityAttributes: map[string]any{"email": "kasun@example.com"},
		Traits:             map[string]any{"city": "Colombo", "age": 30},
	}
	assert.Equal(t, []Change{set("identity_attributes.email"), set("traits.age"), set("traits.city")},
		Diff(Attributes{}, after), "sorted by attribute")
}

func TestOnlyActualChangesAreListed(t *testing.T) {
	before := Attributes{Traits: map[string]any{"city": "Colombo", "age": 30, "tier": "gold"}}
	after := Attributes{Traits: map[string]any{"city": "Kandy", "age": 30, "tier": "gold"}}
	assert.Equal(t, []Change{set("traits.city")}, Diff(before, after))
}

func TestTheSameValuesChangeNothing(t *testing.T) {
	profile := Attributes{
		IdentityAttributes: map[string]any{"email": "a@b.c"},
		Traits:             map[string]any{"city": "Colombo", "tags": []any{"x", "y"}},
	}
	assert.Empty(t, Diff(profile, profile))
	assert.Empty(t, Diff(Attributes{}, Attributes{}))
}

func TestAddedAndRemovedAttributes(t *testing.T) {
	before := Attributes{Traits: map[string]any{"city": "Colombo", "phone": "0771234567"}}
	after := Attributes{Traits: map[string]any{"city": "Colombo", "email": "a@b.c"}}
	assert.Equal(t, []Change{set("traits.email"), removed("traits.phone")}, Diff(before, after))

	// A whole group taken away removes each of its attributes.
	assert.Equal(t, []Change{removed("traits.city"), removed("traits.phone")}, Diff(before, Attributes{}))
}

func TestANullIsNoValue(t *testing.T) {
	before := Attributes{Traits: map[string]any{"city": "Colombo", "phone": nil}}
	after := Attributes{Traits: map[string]any{"city": nil, "phone": nil, "email": nil}}
	assert.Equal(t, []Change{removed("traits.city")}, Diff(before, after),
		"null to null and missing to null change nothing")
	assert.Equal(t, []Change{set("traits.phone")},
		Diff(before, Attributes{Traits: map[string]any{"city": "Colombo", "phone": "077"}}))

	// A typed nil is stored as null, so it is no value either.
	assert.Equal(t, []Change{removed("traits.city")},
		Diff(before, Attributes{Traits: map[string]any{"city": map[string]any(nil), "phone": []string(nil)}}))
}

func TestAComplexAttributeIsOneAttribute(t *testing.T) {
	before := Attributes{Traits: map[string]any{"address": map[string]any{"city": "Colombo", "zip": "00100"}}}
	after := Attributes{Traits: map[string]any{"address": map[string]any{"city": "Kandy", "zip": "00100"}}}
	assert.Equal(t, []Change{set("traits.address")}, Diff(before, after),
		"a changed sub-attribute is a change of its parent, not of traits.address.city")
}

func TestValuesAreComparedAsJSON(t *testing.T) {
	// Stored values come back from the database as float64 and map[string]any; a request can carry ints,
	// typed maps and slices. The same JSON is the same value.
	before := Attributes{Traits: map[string]any{
		"age":     float64(30),
		"address": map[string]any{"zip": "00100", "city": "Kandy"},
		"tags":    []any{"x", "y"},
	}}
	after := Attributes{Traits: map[string]any{
		"age":     30,
		"address": map[string]string{"city": "Kandy", "zip": "00100"},
		"tags":    []string{"x", "y"},
	}}
	assert.Empty(t, Diff(before, after))

	reordered := Attributes{Traits: map[string]any{"age": 30, "address": before.Traits["address"],
		"tags": []string{"y", "x"}}}
	assert.Equal(t, []Change{set("traits.tags")}, Diff(before, reordered), "the order of an array counts")
}

func TestTheGroupsAreApart(t *testing.T) {
	before := Attributes{IdentityAttributes: map[string]any{"email": "a@b.c"}}
	after := Attributes{Traits: map[string]any{"email": "a@b.c"}}
	assert.Equal(t, []Change{removed("identity_attributes.email"), set("traits.email")}, Diff(before, after))
}

func TestTypeChangesAreChanges(t *testing.T) {
	before := Attributes{Traits: map[string]any{"age": "30", "flag": false, "score": 0}}
	after := Attributes{Traits: map[string]any{"age": 30, "flag": true, "score": 0.5}}
	assert.Equal(t, []Change{set("traits.age"), set("traits.flag"), set("traits.score")}, Diff(before, after))

	// false and 0 are values, not "no value".
	assert.Equal(t, []Change{set("traits.flag"), set("traits.score")},
		Diff(Attributes{}, Attributes{Traits: map[string]any{"flag": false, "score": 0}}))
}
