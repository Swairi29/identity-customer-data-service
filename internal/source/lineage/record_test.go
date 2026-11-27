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
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wso2/identity-customer-data-service/internal/source/model"
)

func TestChangesNameTheSourceOfTheContext(t *testing.T) {
	at := time.Date(2026, 11, 27, 9, 15, 0, 0, time.FixedZone("IST", 19800))
	source := &model.Source{SourceID: "3f6c2a1e-8b4d-4c7a-9e2f-1d5b6a7c8e90", Name: "CRM"}
	ctx := WithSource(context.Background(), source)
	source.Name = "Renamed later" // the context keeps its own copy

	before := Attributes{Traits: map[string]any{"phone": "077"}}
	after := Attributes{Traits: map[string]any{"email": "a@b.c"}}
	changes := Changes(ctx, "p-1", before, after, at)
	require.Len(t, changes, 2)
	for _, c := range changes {
		assert.Equal(t, "p-1", c.ProfileID)
		assert.Equal(t, "3f6c2a1e-8b4d-4c7a-9e2f-1d5b6a7c8e90", *c.SourceID)
		assert.Equal(t, "CRM", *c.SourceName)
		assert.Equal(t, at.UTC(), c.ChangedAt, "every change of a write has its time, in UTC")
		assert.Equal(t, time.UTC, c.ChangedAt.Location())
	}
	assert.Equal(t, "traits.email", changes[0].Attribute)
	assert.Equal(t, model.ActionSet, changes[0].Action)
	assert.Equal(t, "traits.phone", changes[1].Attribute)
	assert.Equal(t, model.ActionRemoved, changes[1].Action)
}

func TestChangesWithoutASource(t *testing.T) {
	ctx := WithSource(context.Background(), nil)
	assert.Nil(t, SourceFrom(ctx))
	changes := Changes(ctx, "p-1", Attributes{}, Attributes{Traits: map[string]any{"city": "Kandy"}}, time.Now())
	require.Len(t, changes, 1)
	assert.Nil(t, changes[0].SourceID)
	assert.Nil(t, changes[0].SourceName)
}

func TestNoChangesNoEntries(t *testing.T) {
	same := Attributes{Traits: map[string]any{"city": "Kandy"}}
	assert.Nil(t, Changes(context.Background(), "p-1", same, same, time.Now()))
}
