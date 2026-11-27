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
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	consentModel "github.com/wso2/identity-customer-data-service/internal/consent/model"
	consentStore "github.com/wso2/identity-customer-data-service/internal/consent/store"
	"github.com/wso2/identity-customer-data-service/internal/profile/model"
	profileStore "github.com/wso2/identity-customer-data-service/internal/profile/store"
	"github.com/wso2/identity-customer-data-service/internal/system/constants"
	errors2 "github.com/wso2/identity-customer-data-service/internal/system/errors"
)

type historyEntry struct {
	Attribute  string    `json:"attribute"`
	Action     string    `json:"action"`
	SourceID   *string   `json:"source_id"`
	SourceName *string   `json:"source_name"`
	ChangedAt  time.Time `json:"changed_at"`
}

func (e *profileHandlerEnv) getHistory(t *testing.T, profileID, query string) *httptest.ResponseRecorder {
	t.Helper()
	return e.call(t, e.h.GetProfileSourceHistory, http.MethodGet, e.auth.ValidToken,
		"/"+profileID+"/sources/history"+query, "", profileIDOf(profileID))
}

func (e *profileHandlerEnv) getSources(t *testing.T, profileID, query string) []sourceEntry {
	t.Helper()
	recorder := e.call(t, e.h.GetProfileSources, http.MethodGet, e.auth.ValidToken, "/"+profileID+"/sources"+query,
		"", profileIDOf(profileID))
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	var raw []map[string]interface{}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &raw), recorder.Body.String())
	for _, item := range raw {
		assert.ElementsMatch(t, []string{"attribute", "source_id", "source_name", "created_at", "updated_at"},
			keysOf(item), "the fields of the AttributeSource schema")
	}
	var sources []sourceEntry
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &sources))
	return sources
}

type sourceEntry struct {
	Attribute  string    `json:"attribute"`
	SourceName *string   `json:"source_name"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

func decodeHistory(t *testing.T, recorder *httptest.ResponseRecorder) []historyEntry {
	t.Helper()
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	var history []historyEntry
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &history), recorder.Body.String())
	return history
}

func attributesOf(history []historyEntry) []string {
	out := make([]string, 0, len(history))
	for _, h := range history {
		out = append(out, h.Action+" "+h.Attribute)
	}
	return out
}

// patch changes the traits of a profile through the service, as a call without ?source= does.
func (e *profileHandlerEnv) patch(t *testing.T, profileID string, traits map[string]interface{}) {
	t.Helper()
	_, err := e.svc.PatchProfile(e.ctx, profileID, e.orgID, map[string]interface{}{"traits": traits})
	require.NoError(t, err)
}

func TestGetProfileSourceHistoryReturnsEveryChange(t *testing.T) {
	env := newProfileHandlerEnv(t)
	env.markConsoleSystemApp(t)
	created := env.createProfile(t, "Colombo")
	time.Sleep(2 * time.Millisecond)
	env.patch(t, created.ProfileID, map[string]interface{}{"city": "Kandy", "age": 30})

	recorder := env.getHistory(t, created.ProfileID, "")
	var raw []map[string]interface{}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &raw), recorder.Body.String())
	require.NotEmpty(t, raw)
	assert.ElementsMatch(t, []string{"attribute", "action", "source_id", "source_name", "changed_at"},
		keysOf(raw[0]), "the fields of the AttributeChange schema")
	assert.Nil(t, raw[0]["source_id"], "a write without ?source= names no source")

	history := decodeHistory(t, recorder)
	assert.Equal(t, []string{"SET traits.age", "SET traits.city", "SET traits.city"}, attributesOf(history),
		"newest first; the patch set age and changed city")
	assert.True(t, history[0].ChangedAt.After(history[2].ChangedAt))

	only := decodeHistory(t, env.getHistory(t, created.ProfileID, "?attribute=traits.city"))
	assert.Equal(t, []string{"SET traits.city", "SET traits.city"}, attributesOf(only))
}

func TestGetProfileSourceHistoryOfAMergedProfileIsItsMasters(t *testing.T) {
	env := newProfileHandlerEnv(t)
	env.markConsoleSystemApp(t)
	master := env.createProfile(t, "Colombo")
	childID := uuid.New().String()
	now := time.Now().UTC()
	require.NoError(t, profileStore.InsertProfile(env.ctx, env.orgID, model.Profile{ProfileID: childID,
		OrgID: env.orgID, CreatedAt: now, UpdatedAt: now,
		ProfileStatus: &model.ProfileStatus{ReferenceProfileID: master.ProfileID, ReferenceReason: "email"}}))

	assert.Equal(t, attributesOf(decodeHistory(t, env.getHistory(t, master.ProfileID, ""))),
		attributesOf(decodeHistory(t, env.getHistory(t, childID, ""))))
}

func TestGetProfileSourceHistoryAppliesConsentForOtherCallers(t *testing.T) {
	env := newProfileHandlerEnv(t)
	created := env.createProfile(t, "Colombo")
	env.patch(t, created.ProfileID, map[string]interface{}{"age": 30})
	category := "category-city"
	require.NoError(t, consentStore.AddConsentCategory(env.ctx, env.orgID, consentModel.ConsentCategory{
		CategoryName:       "Analytics",
		CategoryIdentifier: category,
		OrgID:              env.orgID,
		Purpose:            "profiling",
		Attributes: []consentModel.ConsentAttribute{{
			Scope:         constants.Traits,
			AttributeName: constants.Traits + ".city",
			AttributeID:   "attr-" + constants.Traits + ".city",
		}},
	}))
	require.NoError(t, env.svc.UpdateProfileConsents(env.ctx, created.ProfileID, env.orgID,
		[]model.ConsentRecord{{CategoryIdentifier: category, IsConsented: true}}))

	assert.Empty(t, decodeHistory(t, env.getHistory(t, created.ProfileID, "")),
		"no category requested shares no attribute")
	assert.Equal(t, []string{"SET traits.city"},
		attributesOf(decodeHistory(t, env.getHistory(t, created.ProfileID, "?consentCategoryId="+category))),
		"only the consented attribute; age is left out")

	env.markConsoleSystemApp(t)
	assert.Len(t, decodeHistory(t, env.getHistory(t, created.ProfileID, "")), 2, "a system app sees everything")
}

func TestGetProfileSourceHistoryErrors(t *testing.T) {
	env := newProfileHandlerEnv(t)
	created := env.createProfile(t, "Colombo")

	recorder := env.getHistory(t, uuid.New().String(), "")
	require.Equal(t, http.StatusNotFound, recorder.Code, recorder.Body.String())

	for _, attribute := range []string{"city", "application_data.theme", "traits.", "traits.address.city"} {
		recorder = env.getHistory(t, created.ProfileID, "?attribute="+attribute)
		require.Equal(t, http.StatusBadRequest, recorder.Code, attribute)
		var body struct {
			Code string `json:"code"`
		}
		require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &body))
		assert.Equal(t, errors2.AttributeInvalid.Code, body.Code, attribute)
	}
}

func keysOf(m map[string]interface{}) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

func TestGetProfileSourcesReturnsTheCurrentStateOfEachAttribute(t *testing.T) {
	env := newProfileHandlerEnv(t)
	env.markConsoleSystemApp(t)
	created := env.create(t, model.ProfileRequest{Traits: map[string]interface{}{"city": "Colombo", "age": 30}})
	time.Sleep(2 * time.Millisecond)
	env.patch(t, created.ProfileID, map[string]interface{}{"city": "Kandy"})
	_, err := env.svc.UpdateProfile(env.ctx, created.ProfileID, env.orgID,
		model.ProfileRequest{Traits: map[string]interface{}{"city": "Kandy"}}) // removes age
	require.NoError(t, err)

	sources := env.getSources(t, created.ProfileID, "")
	require.Len(t, sources, 1, "age was removed, so only city is listed")
	assert.Equal(t, "traits.city", sources[0].Attribute)
	assert.Nil(t, sources[0].SourceName, "the latest change had no source")
	assert.True(t, sources[0].CreatedAt.Equal(created.Meta.CreatedAt), "created_at is the first SET")
	assert.True(t, sources[0].UpdatedAt.After(sources[0].CreatedAt), "updated_at is the latest change")

	// The history still has every row, including the removal.
	assert.Equal(t, []string{"REMOVED traits.age", "SET traits.city", "SET traits.age", "SET traits.city"},
		attributesOf(decodeHistory(t, env.getHistory(t, created.ProfileID, ""))))
}

func TestGetProfileSourcesAppliesConsentAndFollowsTheMaster(t *testing.T) {
	env := newProfileHandlerEnv(t)
	master := env.create(t, model.ProfileRequest{Traits: map[string]interface{}{"city": "Colombo", "age": 30}})
	category := "category-city"
	require.NoError(t, consentStore.AddConsentCategory(env.ctx, env.orgID, consentModel.ConsentCategory{
		CategoryName: "Analytics", CategoryIdentifier: category, OrgID: env.orgID, Purpose: "profiling",
		Attributes: []consentModel.ConsentAttribute{{Scope: constants.Traits,
			AttributeName: constants.Traits + ".city", AttributeID: "attr-" + constants.Traits + ".city"}},
	}))
	require.NoError(t, env.svc.UpdateProfileConsents(env.ctx, master.ProfileID, env.orgID,
		[]model.ConsentRecord{{CategoryIdentifier: category, IsConsented: true}}))

	assert.Empty(t, env.getSources(t, master.ProfileID, ""), "no category requested shares no attribute")
	consented := env.getSources(t, master.ProfileID, "?consentCategoryId="+category)
	require.Len(t, consented, 1)
	assert.Equal(t, "traits.city", consented[0].Attribute, "age is left out")

	env.markConsoleSystemApp(t)
	childID := uuid.New().String()
	now := time.Now().UTC()
	require.NoError(t, profileStore.InsertProfile(env.ctx, env.orgID, model.Profile{ProfileID: childID,
		OrgID: env.orgID, CreatedAt: now, UpdatedAt: now,
		ProfileStatus: &model.ProfileStatus{ReferenceProfileID: master.ProfileID, ReferenceReason: "email"}}))
	assert.Len(t, env.getSources(t, childID, ""), 2, "a merged profile reads its master's attributes")

	recorder := env.call(t, env.h.GetProfileSources, http.MethodGet, env.auth.ValidToken, "", "",
		profileIDOf(uuid.New().String()))
	assert.Equal(t, http.StatusNotFound, recorder.Code)
}

// create stores a profile through the service, as a call without ?source= does.
func (e *profileHandlerEnv) create(t *testing.T, request model.ProfileRequest) *model.ProfileResponse {
	t.Helper()
	created, err := e.svc.CreateProfile(e.ctx, request, e.orgID)
	require.NoError(t, err)
	return created
}
