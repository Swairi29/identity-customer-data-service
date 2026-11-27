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

package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	orgModel "github.com/wso2/identity-customer-data-service/internal/organization/model"
	sourceModel "github.com/wso2/identity-customer-data-service/internal/source/model"
	sourceService "github.com/wso2/identity-customer-data-service/internal/source/service"
	sourceStore "github.com/wso2/identity-customer-data-service/internal/source/store"
	errors2 "github.com/wso2/identity-customer-data-service/internal/system/errors"
	"github.com/wso2/identity-customer-data-service/test/setup"
)

// guestTransformations is a TransformationLookup for these tests. Each transformation renames the source's
// guest_city to traits.city, and back.
type guestTransformations struct {
	active map[string]bool
}

func (g guestTransformations) GetTransformation(_ context.Context, _, id string) (
	*sourceService.LinkedTransformation, error) {
	active, ok := g.active[id]
	if !ok {
		return nil, nil
	}
	return &sourceService.LinkedTransformation{ID: id, Active: active}, nil
}

func (guestTransformations) Transform(_ context.Context, _, _ string, payload map[string]any) (
	map[string]any, error) {
	return map[string]any{"traits": map[string]any{"city": payload["guest_city"]}}, nil
}

func (guestTransformations) ReverseTransform(_ context.Context, _, _ string, profile map[string]any) (
	map[string]any, error) {
	traits, _ := profile["traits"].(map[string]any)
	return map[string]any{"guest_city": traits["city"]}, nil
}

type noDestinations struct{}

func (noDestinations) MissingDestinations(_ context.Context, _ string, ids []string) ([]string, error) {
	return ids, nil
}

const (
	activeTransformation   = "7b1e0c2a-5d4f-4e8b-9a3c-2f6d8e1b4a70"
	inactiveTransformation = "0f1e2d3c-4b5a-4968-8776-655443322110"
)

// sourceEnv is a profile handler env whose source service knows an active and an inactive transformation.
type sourceEnv struct {
	*profileHandlerEnv
	org orgModel.Ref
	svc *sourceService.SourceService
}

func newSourceEnv(t *testing.T) *sourceEnv {
	t.Helper()
	env := &sourceEnv{profileHandlerEnv: newProfileHandlerEnv(t)}
	env.org = orgModel.Ref{ID: env.orgID, Handle: env.handle}
	env.svc = sourceService.NewSourceService(guestTransformations{active: map[string]bool{
		activeTransformation: true, inactiveTransformation: false}}, noDestinations{})
	previous := sources
	sources = func() sourceService.SourceServiceInterface { return env.svc }
	t.Cleanup(func() { sources = previous })
	return env
}

func (e *sourceEnv) addSource(t *testing.T, name string, transformationID *string) *sourceModel.Source {
	t.Helper()
	source, err := e.svc.AddSource(e.ctx, e.org, sourceModel.Source{Name: name,
		IconURL: "https://cdn.example.com/icon.png", TransformationID: transformationID})
	require.NoError(t, err)
	return source
}

// latest returns the newest history entry of an attribute of a profile, as "action by source".
func (e *sourceEnv) latest(t *testing.T, profileID, attribute string) string {
	t.Helper()
	changes, err := sourceStore.GetAttributeChanges(e.ctx, profileID, e.orgID, attribute)
	require.NoError(t, err)
	require.NotEmpty(t, changes, "no history for "+attribute)
	source := "no source"
	if changes[0].SourceName != nil {
		source = *changes[0].SourceName
	}
	return changes[0].Action + " by " + source
}

func (e *sourceEnv) historyLength(t *testing.T, profileID string) int {
	t.Helper()
	changes, err := sourceStore.GetAttributeChanges(e.ctx, profileID, e.orgID, "")
	require.NoError(t, err)
	return len(changes)
}

func (e *sourceEnv) profileCount(t *testing.T) int {
	t.Helper()
	var count int
	require.NoError(t, e.db.QueryRow(`SELECT COUNT(*) FROM profiles WHERE org_id = ?`, e.orgID).Scan(&count))
	return count
}

func requireErrorCode(t *testing.T, recorder interface {
	Result() *http.Response
}, body []byte, status int, code errors2.ErrorMessage) string {
	t.Helper()
	require.Equal(t, status, recorder.Result().StatusCode, string(body))
	var response struct {
		Code        string `json:"code"`
		Description string `json:"description"`
	}
	require.NoError(t, json.Unmarshal(body, &response), string(body))
	assert.Equal(t, code.Code, response.Code, string(body))
	return response.Description
}

func strPtr(s string) *string { return &s }

func TestCreateProfileWithAnUnregisteredSource(t *testing.T) {
	env := newSourceEnv(t)
	for _, id := range []string{uuid.New().String(), "abc123", ""} {
		recorder := env.call(t, env.h.InitProfile, http.MethodPost, env.auth.ValidToken, "?source="+id,
			`{"traits":{"city":"Colombo"}}`, nil)
		description := requireErrorCode(t, recorder, recorder.Body.Bytes(), http.StatusBadRequest,
			errors2.SourceNotRegistered)
		assert.Contains(t, description, "is not registered for organization '"+env.handle+"'")
	}

	// Another org's source is not registered in this one.
	otherHandle := env.handle + "-other"
	other := setup.TestOrg(t, otherHandle)
	theirs, err := env.svc.AddSource(env.ctx, other, sourceModel.Source{Name: "Their App",
		IconURL: "https://cdn.example.com/icon.png"})
	require.NoError(t, err)
	recorder := env.call(t, env.h.InitProfile, http.MethodPost, env.auth.ValidToken, "?source="+theirs.SourceID,
		`{"traits":{"city":"Colombo"}}`, nil)
	requireErrorCode(t, recorder, recorder.Body.Bytes(), http.StatusBadRequest, errors2.SourceNotRegistered)
	assert.Equal(t, 0, env.profileCount(t), "nothing is stored")
}

func TestSourceWithoutATransformationSendsCDSFormat(t *testing.T) {
	env := newSourceEnv(t)
	booking := env.addSource(t, "Booking App", nil)

	recorder := env.call(t, env.h.InitProfile, http.MethodPost, env.auth.ValidToken, "?source="+booking.SourceID,
		`{"traits":{"city":"Colombo"}}`, nil)
	require.Equal(t, http.StatusCreated, recorder.Code, recorder.Body.String())
	created := decodeProfile(t, recorder)
	assert.Equal(t, "Colombo", created.Traits["city"], "the body is stored as it is")
	assert.Equal(t, "SET by Booking App", env.latest(t, created.ProfileID, "traits.city"))

	recorder = env.call(t, env.h.InitProfile, http.MethodPost, env.auth.ValidToken, "",
		`{"traits":{"city":"Galle"}}`, nil)
	require.Equal(t, http.StatusCreated, recorder.Code, recorder.Body.String())
	assert.Equal(t, "SET by no source", env.latest(t, decodeProfile(t, recorder).ProfileID, "traits.city"))

	// The body is checked as strictly as without a source.
	recorder = env.call(t, env.h.InitProfile, http.MethodPost, env.auth.ValidToken, "?source="+booking.SourceID,
		`{"guest_city":"Colombo"}`, nil)
	requireErrorCode(t, recorder, recorder.Body.Bytes(), http.StatusBadRequest, errors2.AddProfile)

	// GET with the source returns CDS format. The caller is a system app, so consent does not filter traits.
	env.markConsoleSystemApp(t)
	recorder = env.call(t, env.h.GetProfile, http.MethodGet, env.auth.ValidToken, "/"+created.ProfileID+
		"?source="+booking.SourceID, "", profileIDOf(created.ProfileID))
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	assert.Equal(t, "Colombo", decodeProfile(t, recorder).Traits["city"])
}

func TestPatchProfileWithASource(t *testing.T) {
	env := newSourceEnv(t)
	booking := env.addSource(t, "Booking App", nil)
	created := env.createProfile(t, "Colombo")

	recorder := env.call(t, env.h.PatchProfile, http.MethodPatch, env.auth.ValidToken,
		"/"+created.ProfileID+"?source="+booking.SourceID, `{"traits":{"city":"Kandy"}}`,
		profileIDOf(created.ProfileID))
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	assert.Equal(t, "Kandy", decodeProfile(t, recorder).Traits["city"])
	assert.Equal(t, "SET by Booking App", env.latest(t, created.ProfileID, "traits.city"))
	length := env.historyLength(t, created.ProfileID)

	// A patch from an unregistered source changes nothing.
	recorder = env.call(t, env.h.PatchProfile, http.MethodPatch, env.auth.ValidToken,
		"/"+created.ProfileID+"?source="+uuid.New().String(), `{"traits":{"city":"Matara"}}`,
		profileIDOf(created.ProfileID))
	requireErrorCode(t, recorder, recorder.Body.Bytes(), http.StatusBadRequest, errors2.SourceNotRegistered)
	env.markConsoleSystemApp(t)
	recorder = env.call(t, env.h.GetProfile, http.MethodGet, env.auth.ValidToken, "/"+created.ProfileID, "",
		profileIDOf(created.ProfileID))
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	assert.Equal(t, "Kandy", decodeProfile(t, recorder).Traits["city"], "the refused patch was not applied")
	assert.Equal(t, length, env.historyLength(t, created.ProfileID), "and added no history")
}

func TestSourceWithATransformationSendsItsOwnFormat(t *testing.T) {
	env := newSourceEnv(t)
	guests := env.addSource(t, "Guest App", strPtr(activeTransformation))

	recorder := env.call(t, env.h.InitProfile, http.MethodPost, env.auth.ValidToken, "?source="+guests.SourceID,
		`{"guest_city":"Kandy"}`, nil)
	require.Equal(t, http.StatusCreated, recorder.Code, recorder.Body.String())
	created := decodeProfile(t, recorder)
	assert.Equal(t, "Kandy", created.Traits["city"], "the body went through the transformation")
	assert.Equal(t, "SET by Guest App", env.latest(t, created.ProfileID, "traits.city"),
		"the history uses the CDS name, not the source's guest_city")

	recorder = env.call(t, env.h.PatchProfile, http.MethodPatch, env.auth.ValidToken,
		"/"+created.ProfileID+"?source="+guests.SourceID, `{"guest_city":"Galle"}`, profileIDOf(created.ProfileID))
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	assert.Equal(t, "Galle", decodeProfile(t, recorder).Traits["city"])

	// GET with the source returns the source's own field names.
	env.markConsoleSystemApp(t)
	recorder = env.call(t, env.h.GetProfile, http.MethodGet, env.auth.ValidToken, "/"+created.ProfileID+
		"?source="+guests.SourceID, "", profileIDOf(created.ProfileID))
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	assert.JSONEq(t, `{"guest_city":"Galle"}`, recorder.Body.String())

	// A body that is not a JSON object cannot be transformed.
	recorder = env.call(t, env.h.InitProfile, http.MethodPost, env.auth.ValidToken, "?source="+guests.SourceID,
		`["Kandy"]`, nil)
	requireErrorCode(t, recorder, recorder.Body.Bytes(), http.StatusBadRequest, errors2.AddProfile)
}

func TestSourceWithAnInactiveTransformationIsRefused(t *testing.T) {
	env := newSourceEnv(t)
	paused := env.addSource(t, "Paused App", strPtr(inactiveTransformation))
	created := env.createProfile(t, "Colombo")

	recorder := env.call(t, env.h.InitProfile, http.MethodPost, env.auth.ValidToken, "?source="+paused.SourceID,
		`{"guest_city":"Kandy"}`, nil)
	description := requireErrorCode(t, recorder, recorder.Body.Bytes(), http.StatusBadRequest,
		errors2.SourceTransformationInactive)
	assert.Contains(t, description, "is not active")
	assert.Equal(t, 1, env.profileCount(t), "nothing new is stored")

	for _, call := range []struct {
		handler http.HandlerFunc
		method  string
		body    string
	}{{env.h.PatchProfile, http.MethodPatch, `{"guest_city":"Kandy"}`}, {env.h.GetProfile, http.MethodGet, ""}} {
		recorder = env.call(t, call.handler, call.method, env.auth.ValidToken,
			"/"+created.ProfileID+"?source="+paused.SourceID, call.body, profileIDOf(created.ProfileID))
		requireErrorCode(t, recorder, recorder.Body.Bytes(), http.StatusBadRequest,
			errors2.SourceTransformationInactive)
	}
}
