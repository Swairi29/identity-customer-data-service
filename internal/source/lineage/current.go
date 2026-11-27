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
	"sort"

	"github.com/wso2/identity-customer-data-service/internal/source/model"
)

// CurrentSources works out, from the history of a profile, the current state of each attribute, sorted by
// attribute:
//   - the source and updated_at come from the attribute's latest change
//   - created_at is its first SET (or MERGED) since it was last removed, so for an attribute that was
//     removed and set again it is when it was set again
//   - an attribute whose latest change is REMOVED has no value, so it is left out
//
// The history may be in any order.
func CurrentSources(history []model.AttributeChange) []model.AttributeSource {
	byAttribute := map[string][]model.AttributeChange{}
	for _, change := range history {
		byAttribute[change.Attribute] = append(byAttribute[change.Attribute], change)
	}

	current := make([]model.AttributeSource, 0, len(byAttribute))
	for attribute, changes := range byAttribute {
		// Oldest first. Two changes at the same time keep their order in the history, which is newest first,
		// so they are reversed with the rest.
		reversed := make([]model.AttributeChange, len(changes))
		for i, change := range changes {
			reversed[len(changes)-1-i] = change
		}
		sort.SliceStable(reversed, func(i, j int) bool { return reversed[i].ChangedAt.Before(reversed[j].ChangedAt) })

		latest := reversed[len(reversed)-1]
		if latest.Action == model.ActionRemoved {
			continue
		}
		createdAt := latest.ChangedAt
		for i := len(reversed) - 1; i >= 0 && reversed[i].Action != model.ActionRemoved; i-- {
			createdAt = reversed[i].ChangedAt
		}
		current = append(current, model.AttributeSource{
			Attribute:  attribute,
			SourceID:   latest.SourceID,
			SourceName: latest.SourceName,
			CreatedAt:  createdAt.UTC(),
			UpdatedAt:  latest.ChangedAt.UTC(),
		})
	}
	sort.Slice(current, func(i, j int) bool { return current[i].Attribute < current[j].Attribute })
	return current
}
