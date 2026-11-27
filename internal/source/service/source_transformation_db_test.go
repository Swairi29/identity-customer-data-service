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

package service

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wso2/identity-customer-data-service/internal/source/model"
	errors2 "github.com/wso2/identity-customer-data-service/internal/system/errors"
)

// withTransformations makes the service check links against a fake that has the given transformations of
// the env's org.
func (e *sourceEnv) withTransformations(ids ...string) *fakeTransformations {
	fake := &fakeTransformations{}
	for _, id := range ids {
		fake.add(e.org.ID, id, true)
	}
	e.svc = NewSourceService(fake, notConfiguredDestinations{})
	return fake
}

func linkTo(id *string) model.SourcePatch {
	return model.SourcePatch{TransformationID: model.OptionalID{Set: true, Value: id}}
}

func TestAddSourceWithATransformation(t *testing.T) {
	env := newSourceEnv(t)
	env.withTransformations(transformA)

	input := source("Booking App")
	input.TransformationID = ptr(strings.ToUpper(transformA))
	added, err := env.svc.AddSource(env.ctx, env.org, input)
	require.NoError(t, err)
	require.NotNil(t, added.TransformationID)
	assert.Equal(t, transformA, *added.TransformationID, "stored in canonical form")

	stored, err := env.svc.GetSource(env.ctx, env.org, added.SourceID)
	require.NoError(t, err)
	assert.Equal(t, added.ToResponse(), stored.ToResponse())
}

func TestAddSourceWithoutATransformationStoresNull(t *testing.T) {
	env := newSourceEnv(t)
	added := env.add(t, env.org, "Mobile App")
	assert.Nil(t, added.TransformationID)

	stored, err := env.svc.GetSource(env.ctx, env.org, added.SourceID)
	require.NoError(t, err)
	assert.Nil(t, stored.TransformationID)
}

func TestAddSourceRejectsAnUnknownTransformationAndStoresNothing(t *testing.T) {
	env := newSourceEnv(t)
	env.withTransformations(transformA)

	input := source("Booking App")
	input.TransformationID = ptr(destinationA) // a UUID that is not a transformation of the org
	_, err := env.svc.AddSource(env.ctx, env.org, input)
	requireClientError(t, err, http.StatusBadRequest, errors2.SourceTransformationInvalid)

	sources, err := env.svc.GetSources(env.ctx, env.org)
	require.NoError(t, err)
	assert.Empty(t, sources)
}

func TestAddSourceRejectsAnotherOrgsTransformation(t *testing.T) {
	env := newSourceEnv(t)
	fake := env.withTransformations()
	other := env.newOrg(t)
	fake.add(other.ID, transformA, true)

	input := source("Booking App")
	input.TransformationID = ptr(transformA)
	_, err := env.svc.AddSource(env.ctx, env.org, input)
	requireClientError(t, err, http.StatusBadRequest, errors2.SourceTransformationInvalid)
}

func TestUpdateSourceLinksAndUnlinksATransformation(t *testing.T) {
	env := newSourceEnv(t)
	env.withTransformations(transformA)
	setClock(t, time.Date(2026, 11, 25, 6, 0, 0, 0, time.UTC))
	added := env.add(t, env.org, "Mobile App")

	linkedAt := time.Date(2026, 11, 26, 6, 0, 0, 0, time.UTC)
	setClock(t, linkedAt)
	linked, err := env.svc.UpdateSource(env.ctx, env.org, added.SourceID, linkTo(ptr(transformA)))
	require.NoError(t, err)
	require.NotNil(t, linked.TransformationID)
	assert.Equal(t, transformA, *linked.TransformationID)
	assert.Equal(t, added.Description, linked.Description, "a patch without description keeps it")
	assert.Equal(t, linkedAt, linked.UpdatedAt)

	// A description-only patch keeps the link.
	described, err := env.svc.UpdateSource(env.ctx, env.org, added.SourceID, describe("new"))
	require.NoError(t, err)
	require.NotNil(t, described.TransformationID)

	unlinked, err := env.svc.UpdateSource(env.ctx, env.org, added.SourceID, linkTo(nil))
	require.NoError(t, err)
	assert.Nil(t, unlinked.TransformationID)

	stored, err := env.svc.GetSource(env.ctx, env.org, added.SourceID)
	require.NoError(t, err)
	assert.Nil(t, stored.TransformationID)
	assert.Equal(t, "new", stored.Description)
}

func TestUpdateSourceBothFieldsAtOnce(t *testing.T) {
	env := newSourceEnv(t)
	env.withTransformations(transformA)
	added := env.add(t, env.org, "Mobile App")

	patch := linkTo(ptr(transformA))
	patch.Description = ptr("Linked")
	updated, err := env.svc.UpdateSource(env.ctx, env.org, added.SourceID, patch)
	require.NoError(t, err)
	assert.Equal(t, "Linked", updated.Description)
	require.NotNil(t, updated.TransformationID)
}

func TestUpdateSourceWithABadLinkChangesNothing(t *testing.T) {
	env := newSourceEnv(t)
	env.withTransformations(transformA)
	added := env.add(t, env.org, "Mobile App")

	patch := linkTo(ptr(destinationA))
	patch.Description = ptr("should not be stored")
	_, err := env.svc.UpdateSource(env.ctx, env.org, added.SourceID, patch)
	requireClientError(t, err, http.StatusBadRequest, errors2.SourceTransformationInvalid)

	stored, err := env.svc.GetSource(env.ctx, env.org, added.SourceID)
	require.NoError(t, err)
	assert.Equal(t, added.ToResponse(), stored.ToResponse())
}

func TestUpdateSourceRejectsAnEmptyPatch(t *testing.T) {
	env := newSourceEnv(t)
	added := env.add(t, env.org, "Mobile App")
	_, err := env.svc.UpdateSource(env.ctx, env.org, added.SourceID, model.SourcePatch{})
	requireClientError(t, err, http.StatusBadRequest, errors2.SourceEmptyUpdate)
}
