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
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	orgModel "github.com/wso2/identity-customer-data-service/internal/organization/model"
	"github.com/wso2/identity-customer-data-service/internal/source/model"
	"github.com/wso2/identity-customer-data-service/internal/system/constants"
	"github.com/wso2/identity-customer-data-service/internal/system/database"
	"github.com/wso2/identity-customer-data-service/internal/system/database/provider"
	errors2 "github.com/wso2/identity-customer-data-service/internal/system/errors"
	"github.com/wso2/identity-customer-data-service/internal/system/log"
	"github.com/wso2/identity-customer-data-service/test/setup"
)

const mobileApp = `{"name":"Mobile App","description":"Events from the iOS and Android apps",` +
	`"icon_url":"https://cdn.example.com/icons/mobile.png"}`

var sourceHandlerOrgCounter atomic.Int64

type sourceHandlerEnv struct {
	handle string
	org    orgModel.Ref
	db     *sql.DB
	auth   setup.AuthStub
	h      *SourceHandler
}

func newSourceHandlerEnv(t *testing.T) *sourceHandlerEnv {
	t.Helper()
	require.NoError(t, log.Init("ERROR"))
	setup.EnableOrganizationManagement()
	testDB, err := setup.SetupTestSQLite()
	require.NoError(t, err)
	provider.SetTestDB(testDB.DB, database.TypeSQLite)
	t.Cleanup(func() {
		provider.SetTestDB(nil, "")
		testDB.Terminate()
	})
	auth := setup.InstallAuthStub(t, "source:view", "source:create", "source:update", "source:delete",
		"profile:view")

	handle := fmt.Sprintf("acme-%d", sourceHandlerOrgCounter.Add(1))
	org := setup.TestOrg(t, handle)
	return &sourceHandlerEnv{handle: handle, org: org, db: testDB.DB, auth: auth, h: NewSourceHandler()}
}

func (e *sourceHandlerEnv) call(t *testing.T, handler http.HandlerFunc, method, token, body,
	sourceID string) *httptest.ResponseRecorder {
	t.Helper()
	path := "/t/" + e.handle + "/cds/api/v1/sources"
	if sourceID != "" {
		path += "/" + sourceID
	}
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	request = request.WithContext(context.WithValue(request.Context(), constants.TenantContextKey, e.handle))
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	request.Header.Set("Content-Type", "application/json")
	if sourceID != "" {
		request.SetPathValue("sourceId", sourceID)
	}
	recorder := httptest.NewRecorder()
	handler(recorder, request)
	return recorder
}

// addMobileApp creates the mobileApp source and returns it.
func (e *sourceHandlerEnv) addMobileApp(t *testing.T) model.SourceResponse {
	t.Helper()
	recorder := e.call(t, e.h.AddSource, http.MethodPost, e.auth.ValidToken, mobileApp, "")
	require.Equal(t, http.StatusCreated, recorder.Code, recorder.Body.String())
	var created model.SourceResponse
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &created))
	return created
}

func requireError(t *testing.T, recorder *httptest.ResponseRecorder, status int, code errors2.ErrorMessage) string {
	t.Helper()
	require.Equal(t, status, recorder.Code, recorder.Body.String())
	var body struct {
		Code        string `json:"code"`
		Message     string `json:"message"`
		Description string `json:"description"`
	}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &body))
	assert.Equal(t, code.Code, body.Code)
	assert.Equal(t, code.Message, body.Message)
	return body.Description
}

func TestAddSourceReturnsTheCreatedSource(t *testing.T) {
	env := newSourceHandlerEnv(t)

	recorder := env.call(t, env.h.AddSource, http.MethodPost, env.auth.ValidToken, mobileApp, "")

	require.Equal(t, http.StatusCreated, recorder.Code, recorder.Body.String())
	var body map[string]interface{}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &body))
	assert.ElementsMatch(t, []string{"source_id", "name", "description", "icon_url", "transformation_id",
		"destination_ids", "created_at", "updated_at"}, keys(body),
		"the body has the fields of the Source schema, and no org_id")
	assert.Equal(t, []interface{}{}, body["destination_ids"], "no destinations is [], not null")

	sourceID := body["source_id"].(string)
	_, err := uuid.Parse(sourceID)
	assert.NoError(t, err)
	assert.Equal(t, "Mobile App", body["name"])
	assert.Equal(t, "/t/"+env.handle+"/cds/api/v1/sources/"+sourceID, recorder.Header().Get("Location"))
	assert.Equal(t, "application/json", recorder.Header().Get("Content-Type"))
}

func TestAddSourceRejectsUnknownFields(t *testing.T) {
	env := newSourceHandlerEnv(t)

	for _, body := range []string{
		`{"source_id":"3f6c2a1e-8b4d-4c7a-9e2f-1d5b6a7c8e90","name":"A","icon_url":"https://a.example.com/a.png"}`,
		`{"name":"A","icon_url":"https://a.example.com/a.png","url":"https://a.example.com"}`,
	} {
		recorder := env.call(t, env.h.AddSource, http.MethodPost, env.auth.ValidToken, body, "")
		description := requireError(t, recorder, http.StatusBadRequest, errors2.AddSourceBadRequest)
		assert.Contains(t, description, "Unknown field")
	}

	recorder := env.call(t, env.h.AddSource, http.MethodPost, env.auth.ValidToken, `{"name":`, "")
	requireError(t, recorder, http.StatusBadRequest, errors2.AddSourceBadRequest)
}

func TestAddSourceValidationAndConflict(t *testing.T) {
	env := newSourceHandlerEnv(t)

	recorder := env.call(t, env.h.AddSource, http.MethodPost, env.auth.ValidToken,
		`{"icon_url":"https://a.example.com/a.png"}`, "")
	requireError(t, recorder, http.StatusBadRequest, errors2.SourceNameInvalid)

	recorder = env.call(t, env.h.AddSource, http.MethodPost, env.auth.ValidToken,
		`{"name":"A","icon_url":"http://a.example.com/a.png"}`, "")
	requireError(t, recorder, http.StatusBadRequest, errors2.SourceIconURLInvalid)

	env.addMobileApp(t)
	recorder = env.call(t, env.h.AddSource, http.MethodPost, env.auth.ValidToken, mobileApp, "")
	description := requireError(t, recorder, http.StatusConflict, errors2.SourceAlreadyExists)
	assert.Equal(t, "A source named 'Mobile App' already exists in this organization.", description)
}

func TestGetSources(t *testing.T) {
	env := newSourceHandlerEnv(t)

	recorder := env.call(t, env.h.GetAllSources, http.MethodGet, env.auth.ValidToken, "", "")
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	assert.JSONEq(t, `[]`, recorder.Body.String(), "an org without sources gets an empty array")

	created := env.addMobileApp(t)
	recorder = env.call(t, env.h.GetAllSources, http.MethodGet, env.auth.ValidToken, "", "")
	require.Equal(t, http.StatusOK, recorder.Code)
	var listed []model.SourceResponse
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &listed))
	require.Len(t, listed, 1)
	assert.Equal(t, created, listed[0])

	recorder = env.call(t, env.h.GetSource, http.MethodGet, env.auth.ValidToken, "", created.SourceID)
	require.Equal(t, http.StatusOK, recorder.Code)
	var fetched model.SourceResponse
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &fetched))
	assert.Equal(t, created, fetched)
}

func TestGetSourceErrors(t *testing.T) {
	env := newSourceHandlerEnv(t)

	recorder := env.call(t, env.h.GetSource, http.MethodGet, env.auth.ValidToken, "", "abc123")
	requireError(t, recorder, http.StatusBadRequest, errors2.SourceIDInvalid)

	recorder = env.call(t, env.h.GetSource, http.MethodGet, env.auth.ValidToken, "", uuid.New().String())
	requireError(t, recorder, http.StatusNotFound, errors2.SourceNotFound)
}

func TestPatchSource(t *testing.T) {
	env := newSourceHandlerEnv(t)
	created := env.addMobileApp(t)

	recorder := env.call(t, env.h.PatchSource, http.MethodPatch, env.auth.ValidToken,
		`{"description":"Events from mobile apps and the wearable"}`, created.SourceID)
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	var updated model.SourceResponse
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &updated))
	assert.Equal(t, "Events from mobile apps and the wearable", updated.Description)
	assert.Equal(t, created.Name, updated.Name)
	assert.Equal(t, created.CreatedAt, updated.CreatedAt)
	assert.False(t, updated.UpdatedAt.Before(created.UpdatedAt))
}

func TestPatchSourceErrors(t *testing.T) {
	env := newSourceHandlerEnv(t)
	created := env.addMobileApp(t)

	for _, field := range []string{"name", "icon_url", "source_id"} {
		body := fmt.Sprintf(`{"description":"x",%q:"y"}`, field)
		recorder := env.call(t, env.h.PatchSource, http.MethodPatch, env.auth.ValidToken, body, created.SourceID)
		description := requireError(t, recorder, http.StatusBadRequest, errors2.UpdateSourceBadRequest)
		assert.Equal(t, "Only 'description', 'transformation_id' and 'destination_ids' can be updated; '"+
			field+"' is not allowed.", description)
	}

	for _, body := range []string{"", "{}"} {
		recorder := env.call(t, env.h.PatchSource, http.MethodPatch, env.auth.ValidToken, body, created.SourceID)
		requireError(t, recorder, http.StatusBadRequest, errors2.SourceEmptyUpdate)
	}

	recorder := env.call(t, env.h.PatchSource, http.MethodPatch, env.auth.ValidToken,
		`{"description":"x"}`, "abc123")
	requireError(t, recorder, http.StatusBadRequest, errors2.SourceIDInvalid)

	recorder = env.call(t, env.h.PatchSource, http.MethodPatch, env.auth.ValidToken,
		`{"description":"x"}`, uuid.New().String())
	requireError(t, recorder, http.StatusNotFound, errors2.SourceNotFound)
}

func TestDeleteSource(t *testing.T) {
	env := newSourceHandlerEnv(t)
	created := env.addMobileApp(t)

	for range 2 {
		recorder := env.call(t, env.h.DeleteSource, http.MethodDelete, env.auth.ValidToken, "", created.SourceID)
		assert.Equal(t, http.StatusNoContent, recorder.Code, "a delete, and a repeated one, give a 204")
		assert.Empty(t, recorder.Body.String())
	}

	recorder := env.call(t, env.h.GetSource, http.MethodGet, env.auth.ValidToken, "", created.SourceID)
	requireError(t, recorder, http.StatusNotFound, errors2.SourceNotFound)

	recorder = env.call(t, env.h.DeleteSource, http.MethodDelete, env.auth.ValidToken, "", "abc123")
	requireError(t, recorder, http.StatusBadRequest, errors2.SourceIDInvalid)
}

func TestSourceEndpointsRequireTheScope(t *testing.T) {
	env := newSourceHandlerEnv(t)
	sourceID := uuid.New().String()

	endpoints := []struct {
		name    string
		handler http.HandlerFunc
		method  string
		body    string
		id      string
	}{
		{"add", env.h.AddSource, http.MethodPost, mobileApp, ""},
		{"list", env.h.GetAllSources, http.MethodGet, "", ""},
		{"get", env.h.GetSource, http.MethodGet, "", sourceID},
		{"patch", env.h.PatchSource, http.MethodPatch, `{"description":"x"}`, sourceID},
		{"delete", env.h.DeleteSource, http.MethodDelete, "", sourceID},
	}
	for _, ep := range endpoints {
		t.Run(ep.name, func(t *testing.T) {
			recorder := env.call(t, ep.handler, ep.method, "", ep.body, ep.id)
			assert.Equal(t, http.StatusUnauthorized, recorder.Code, "no token")

			recorder = env.call(t, ep.handler, ep.method, env.auth.OtherToken, ep.body, ep.id)
			assert.Equal(t, http.StatusForbidden, recorder.Code, "a token without the scope")
		})
	}
}

func keys(m map[string]interface{}) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

func TestSourceWithoutTransformation(t *testing.T) {
	env := newSourceHandlerEnv(t)

	// Omitted and null both give a source with no transformation, returned as null.
	created := env.addMobileApp(t)
	assert.Nil(t, created.TransformationID)
	recorder := env.call(t, env.h.GetSource, http.MethodGet, env.auth.ValidToken, "", created.SourceID)
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	assert.Contains(t, recorder.Body.String(), `"transformation_id":null`)

	recorder = env.call(t, env.h.AddSource, http.MethodPost, env.auth.ValidToken,
		`{"name":"Booking App","icon_url":"https://cdn.example.com/b.png","transformation_id":null}`, "")
	require.Equal(t, http.StatusCreated, recorder.Code, recorder.Body.String())
	assert.Contains(t, recorder.Body.String(), `"transformation_id":null`)

	// A patch with only transformation_id: null is not empty, and keeps the description.
	recorder = env.call(t, env.h.PatchSource, http.MethodPatch, env.auth.ValidToken,
		`{"transformation_id":null}`, created.SourceID)
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	var patched model.SourceResponse
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &patched))
	assert.Nil(t, patched.TransformationID)
	assert.Equal(t, created.Description, patched.Description)
}

func TestTransformationLinksAreRejectedUntilTransformationsExist(t *testing.T) {
	env := newSourceHandlerEnv(t)
	created := env.addMobileApp(t)
	id := uuid.New().String()

	recorder := env.call(t, env.h.AddSource, http.MethodPost, env.auth.ValidToken,
		`{"name":"Booking App","icon_url":"https://cdn.example.com/b.png","transformation_id":"`+id+`"}`, "")
	description := requireError(t, recorder, http.StatusBadRequest, errors2.SourceTransformationInvalid)
	assert.Equal(t, "Transformations are not available yet, so 'transformation_id' must be null or omitted.",
		description)

	recorder = env.call(t, env.h.PatchSource, http.MethodPatch, env.auth.ValidToken,
		`{"transformation_id":"`+id+`"}`, created.SourceID)
	requireError(t, recorder, http.StatusBadRequest, errors2.SourceTransformationInvalid)

	recorder = env.call(t, env.h.PatchSource, http.MethodPatch, env.auth.ValidToken,
		`{"transformation_id":"abc123"}`, created.SourceID)
	description = requireError(t, recorder, http.StatusBadRequest, errors2.SourceTransformationInvalid)
	assert.Equal(t, "'abc123' is not a valid UUID.", description)

	recorder = env.call(t, env.h.PatchSource, http.MethodPatch, env.auth.ValidToken,
		`{"transformation_id":42}`, created.SourceID)
	requireError(t, recorder, http.StatusBadRequest, errors2.UpdateSourceBadRequest)
}

func TestDestinationLinksAreRejectedUntilDestinationsExist(t *testing.T) {
	env := newSourceHandlerEnv(t)
	created := env.addMobileApp(t)
	id := uuid.New().String()

	recorder := env.call(t, env.h.AddSource, http.MethodPost, env.auth.ValidToken,
		`{"name":"Booking App","icon_url":"https://cdn.example.com/b.png","destination_ids":["`+id+`"]}`, "")
	description := requireError(t, recorder, http.StatusBadRequest, errors2.SourceDestinationInvalid)
	assert.Equal(t, "Destinations are not available yet, so 'destination_ids' must be empty or omitted.",
		description)

	recorder = env.call(t, env.h.PatchSource, http.MethodPatch, env.auth.ValidToken,
		`{"destination_ids":["`+id+`"]}`, created.SourceID)
	requireError(t, recorder, http.StatusBadRequest, errors2.SourceDestinationInvalid)
}

func TestDestinationIDsWithoutDestinations(t *testing.T) {
	env := newSourceHandlerEnv(t)

	recorder := env.call(t, env.h.AddSource, http.MethodPost, env.auth.ValidToken,
		`{"name":"Booking App","icon_url":"https://cdn.example.com/b.png","destination_ids":[]}`, "")
	require.Equal(t, http.StatusCreated, recorder.Code, recorder.Body.String())
	var created model.SourceResponse
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &created))
	assert.Equal(t, []string{}, created.DestinationIDs)

	// [] is a valid patch on its own, and GET returns [].
	recorder = env.call(t, env.h.PatchSource, http.MethodPatch, env.auth.ValidToken,
		`{"destination_ids":[]}`, created.SourceID)
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	recorder = env.call(t, env.h.GetAllSources, http.MethodGet, env.auth.ValidToken, "", "")
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	assert.Contains(t, recorder.Body.String(), `"destination_ids":[]`)

	// null is not a list, and a wrong type names the field.
	for _, body := range []string{`{"destination_ids":null}`, `{"destination_ids":"x"}`, `{"destination_ids":[1]}`} {
		recorder = env.call(t, env.h.PatchSource, http.MethodPatch, env.auth.ValidToken, body, created.SourceID)
		description := requireError(t, recorder, http.StatusBadRequest, errors2.UpdateSourceBadRequest)
		assert.Contains(t, description, "destination_ids", body)
	}
}
