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

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wso2/identity-customer-data-service/internal/source/model"
	errors2 "github.com/wso2/identity-customer-data-service/internal/system/errors"
)

const destinationC = "0f1e2d3c-4b5a-4968-8776-655443322110"

// withDestinations makes the service check links against fakes: the env's org has transformA and the given
// destinations.
func (e *sourceEnv) withDestinations(ids ...string) *fakeDestinations {
	transformations := &fakeTransformations{}
	transformations.add(e.org.ID, transformA, true)
	destinations := &fakeDestinations{}
	destinations.add(e.org.ID, ids...)
	e.svc = NewSourceService(transformations, destinations)
	return destinations
}

func linkDestinations(ids ...string) model.SourcePatch {
	return model.SourcePatch{DestinationIDs: model.OptionalIDList{Set: true, Values: append([]string{}, ids...)}}
}

// countLinks returns the number of source_destinations rows of a source, read from the table directly.
func (e *sourceEnv) countLinks(t *testing.T, sourceID string) int {
	t.Helper()
	var count int
	require.NoError(t, e.db.QueryRow(`SELECT COUNT(*) FROM source_destinations WHERE source_id = ?`, sourceID).
		Scan(&count))
	return count
}

func TestAddSourceWithDestinations(t *testing.T) {
	env := newSourceEnv(t)
	env.withDestinations(destinationA, destinationB)

	input := source("Booking App")
	input.DestinationIDs = []string{strings.ToUpper(destinationA), destinationB}
	added, err := env.svc.AddSource(env.ctx, env.org, input)
	require.NoError(t, err)
	// Sorted by ID and in canonical form: destinationB (9a8b...) sorts before destinationA (c2d9...).
	assert.Equal(t, []string{destinationB, destinationA}, added.DestinationIDs)
	assert.Equal(t, 2, env.countLinks(t, added.SourceID))

	stored, err := env.svc.GetSource(env.ctx, env.org, added.SourceID)
	require.NoError(t, err)
	assert.Equal(t, added.ToResponse(), stored.ToResponse())
}

func TestAddSourceWithoutDestinationsStoresNoLinks(t *testing.T) {
	env := newSourceEnv(t)
	added := env.add(t, env.org, "Mobile App")
	assert.Equal(t, []string{}, added.ToResponse().DestinationIDs)
	assert.Equal(t, 0, env.countLinks(t, added.SourceID))
}

func TestAddSourceWithABadDestinationStoresNothing(t *testing.T) {
	env := newSourceEnv(t)
	env.withDestinations(destinationA)

	cases := map[string][]string{
		"unknown":  {destinationA, destinationB},
		"repeated": {destinationA, destinationA},
		"not uuid": {"abc123"},
	}
	for name, ids := range cases {
		input := source("Booking App " + name)
		input.TransformationID = ptr(transformA)
		input.DestinationIDs = ids
		_, err := env.svc.AddSource(env.ctx, env.org, input)
		requireClientError(t, err, http.StatusBadRequest, errors2.SourceDestinationInvalid)
	}
	sources, err := env.svc.GetSources(env.ctx, env.org)
	require.NoError(t, err)
	assert.Empty(t, sources)
}

func TestAddSourceRejectsAnotherOrgsDestination(t *testing.T) {
	env := newSourceEnv(t)
	fake := env.withDestinations()
	other := env.newOrg(t)
	fake.add(other.ID, destinationA)

	input := source("Booking App")
	input.DestinationIDs = []string{destinationA}
	_, err := env.svc.AddSource(env.ctx, env.org, input)
	requireClientError(t, err, http.StatusBadRequest, errors2.SourceDestinationInvalid)
}

func TestGetSourcesReturnsEachSourcesOwnDestinations(t *testing.T) {
	env := newSourceEnv(t)
	env.withDestinations(destinationA, destinationB, destinationC)

	first := source("First")
	first.DestinationIDs = []string{destinationA, destinationC}
	addedFirst, err := env.svc.AddSource(env.ctx, env.org, first)
	require.NoError(t, err)
	second := source("Second")
	second.DestinationIDs = []string{destinationB}
	addedSecond, err := env.svc.AddSource(env.ctx, env.org, second)
	require.NoError(t, err)
	env.add(t, env.org, "Third")

	sources, err := env.svc.GetSources(env.ctx, env.org)
	require.NoError(t, err)
	byID := map[string][]string{}
	for _, s := range sources {
		byID[s.SourceID] = s.ToResponse().DestinationIDs
	}
	assert.Equal(t, []string{destinationC, destinationA}, byID[addedFirst.SourceID])
	assert.Equal(t, []string{destinationB}, byID[addedSecond.SourceID])
	assert.Len(t, byID, 3)
	for _, s := range sources {
		if s.Name == "Third" {
			assert.Equal(t, []string{}, s.DestinationIDs)
		}
	}
}

func TestUpdateSourceReplacesTheDestinations(t *testing.T) {
	env := newSourceEnv(t)
	env.withDestinations(destinationA, destinationB, destinationC)
	input := source("Booking App")
	input.DestinationIDs = []string{destinationA, destinationB}
	added, err := env.svc.AddSource(env.ctx, env.org, input)
	require.NoError(t, err)

	replaced, err := env.svc.UpdateSource(env.ctx, env.org, added.SourceID, linkDestinations(destinationC))
	require.NoError(t, err)
	assert.Equal(t, []string{destinationC}, replaced.DestinationIDs)
	assert.Equal(t, added.Description, replaced.Description)

	// A patch without destination_ids keeps them.
	described, err := env.svc.UpdateSource(env.ctx, env.org, added.SourceID, describe("new"))
	require.NoError(t, err)
	assert.Equal(t, []string{destinationC}, described.DestinationIDs)

	stored, err := env.svc.GetSource(env.ctx, env.org, added.SourceID)
	require.NoError(t, err)
	assert.Equal(t, described.ToResponse(), stored.ToResponse())

	// [] removes every link.
	cleared, err := env.svc.UpdateSource(env.ctx, env.org, added.SourceID, linkDestinations())
	require.NoError(t, err)
	assert.Equal(t, []string{}, cleared.DestinationIDs)
	assert.Equal(t, 0, env.countLinks(t, added.SourceID))
}

func TestUpdateSourceWithABadDestinationChangesNothing(t *testing.T) {
	env := newSourceEnv(t)
	env.withDestinations(destinationA)
	input := source("Booking App")
	input.DestinationIDs = []string{destinationA}
	added, err := env.svc.AddSource(env.ctx, env.org, input)
	require.NoError(t, err)

	patch := linkDestinations(destinationA, destinationB)
	patch.Description = ptr("should not be stored")
	patch.TransformationID = model.OptionalID{Set: true, Value: ptr(transformA)}
	_, err = env.svc.UpdateSource(env.ctx, env.org, added.SourceID, patch)
	requireClientError(t, err, http.StatusBadRequest, errors2.SourceDestinationInvalid)

	stored, err := env.svc.GetSource(env.ctx, env.org, added.SourceID)
	require.NoError(t, err)
	assert.Equal(t, added.ToResponse(), stored.ToResponse())
}

func TestDeleteSourceRemovesItsDestinationLinks(t *testing.T) {
	env := newSourceEnv(t)
	env.withDestinations(destinationA, destinationB)
	input := source("Booking App")
	input.DestinationIDs = []string{destinationA, destinationB}
	added, err := env.svc.AddSource(env.ctx, env.org, input)
	require.NoError(t, err)
	kept := source("Kept")
	kept.DestinationIDs = []string{destinationA}
	addedKept, err := env.svc.AddSource(env.ctx, env.org, kept)
	require.NoError(t, err)

	require.NoError(t, env.svc.DeleteSource(env.ctx, env.org, added.SourceID))
	assert.Equal(t, 0, env.countLinks(t, added.SourceID), "the links go with the source")
	assert.Equal(t, 1, env.countLinks(t, addedKept.SourceID), "other sources keep theirs")
}
