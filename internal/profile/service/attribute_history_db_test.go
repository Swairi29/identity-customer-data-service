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
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	profileModel "github.com/wso2/identity-customer-data-service/internal/profile/model"
	"github.com/wso2/identity-customer-data-service/internal/source/lineage"
	sourceModel "github.com/wso2/identity-customer-data-service/internal/source/model"
	sourceStore "github.com/wso2/identity-customer-data-service/internal/source/store"
	errors2 "github.com/wso2/identity-customer-data-service/internal/system/errors"
)

// entry is a history row without its ID and time, for comparing.
type entry struct {
	attribute, action, source string
}

func (e *profileEnv) history(t *testing.T, profileID string) []sourceModel.AttributeChange {
	t.Helper()
	changes, err := sourceStore.GetAttributeChanges(e.ctx, profileID, e.orgID, "")
	require.NoError(t, err)
	return changes
}

func (e *profileEnv) entries(t *testing.T, profileID string) []entry {
	t.Helper()
	var out []entry
	for _, c := range e.history(t, profileID) {
		source := ""
		if c.SourceName != nil {
			source = *c.SourceName
		}
		out = append(out, entry{c.Attribute, c.Action, source})
	}
	return out
}

func fromSource(name string) context.Context {
	return lineage.WithSource(context.Background(), &sourceModel.Source{
		SourceID: "3f6c2a1e-8b4d-4c7a-9e2f-1d5b6a7c8e90", Name: name})
}

func TestCreatingAProfileSetsEachAttribute(t *testing.T) {
	env := newProfileEnv(t)
	created, err := env.svc.CreateProfile(fromSource("CRM"), profileModel.ProfileRequest{
		IdentityAttributes: map[string]interface{}{"email": "kasun@example.com"},
		Traits:             map[string]interface{}{"city": "Colombo", "age": 30},
	}, env.orgID)
	require.NoError(t, err)

	history := env.history(t, created.ProfileID)
	assert.Equal(t, []entry{{"identity_attributes.email", "SET", "CRM"}, {"traits.age", "SET", "CRM"},
		{"traits.city", "SET", "CRM"}}, env.entries(t, created.ProfileID))
	for _, c := range history {
		assert.Equal(t, "3f6c2a1e-8b4d-4c7a-9e2f-1d5b6a7c8e90", *c.SourceID)
		assert.True(t, c.ChangedAt.Equal(created.Meta.CreatedAt), "the time of the write")
	}
}

func TestAWriteWithoutASourceRecordsNoSource(t *testing.T) {
	env := newProfileEnv(t)
	created := env.createWithCity(t, "Colombo")
	history := env.history(t, created.ProfileID)
	require.Len(t, history, 1)
	assert.Nil(t, history[0].SourceID)
	assert.Nil(t, history[0].SourceName)
}

func TestAPatchRecordsOnlyActualChanges(t *testing.T) {
	env := newProfileEnv(t)
	created := env.create(t, profileModel.ProfileRequest{Traits: map[string]interface{}{"city": "Colombo",
		"age": 30}})

	_, err := env.svc.PatchProfile(fromSource("Booking App"), created.ProfileID, env.orgID,
		map[string]interface{}{"traits": map[string]interface{}{"city": "Kandy", "age": 30}})
	require.NoError(t, err)
	assert.Equal(t, []entry{{"traits.city", "SET", "Booking App"}, {"traits.age", "SET", ""},
		{"traits.city", "SET", ""}}, env.entries(t, created.ProfileID), "age was sent with the same value")

	// The same values again add nothing.
	_, err = env.svc.PatchProfile(fromSource("Booking App"), created.ProfileID, env.orgID,
		map[string]interface{}{"traits": map[string]interface{}{"city": "Kandy"}})
	require.NoError(t, err)
	assert.Len(t, env.history(t, created.ProfileID), 3)
}

func TestAnUpdateRecordsRemovedAttributes(t *testing.T) {
	env := newProfileEnv(t)
	created := env.create(t, profileModel.ProfileRequest{Traits: map[string]interface{}{"city": "Colombo",
		"age": 30}})

	_, err := env.svc.UpdateProfile(fromSource("CRM"), created.ProfileID, env.orgID,
		profileModel.ProfileRequest{Traits: map[string]interface{}{"city": "Colombo"}})
	require.NoError(t, err)
	assert.Equal(t, entry{"traits.age", "REMOVED", "CRM"}, env.entries(t, created.ProfileID)[0])
	assert.Len(t, env.history(t, created.ProfileID), 3, "city did not change")
}

func TestAPatchOfAMergedProfileIsRecordedOnItsMaster(t *testing.T) {
	env := newProfileEnv(t)
	master := env.createWithCity(t, "Colombo")
	env.addChild(t, master.ProfileID, "child-1")

	_, err := env.svc.PatchProfile(fromSource("CRM"), "child-1", env.orgID,
		map[string]interface{}{"traits": map[string]interface{}{"city": "Galle"}})
	require.NoError(t, err)
	assert.Equal(t, entry{"traits.city", "SET", "CRM"}, env.entries(t, master.ProfileID)[0],
		"the master holds the data that changed")
	assert.Empty(t, env.history(t, "child-1"))
}

func TestAFailedHistoryWriteStoresNoProfile(t *testing.T) {
	env := newProfileEnv(t)
	created := env.createWithCity(t, "Colombo")
	_, err := env.db.Exec(`DROP TABLE profile_attribute_sources`)
	require.NoError(t, err)

	_, err = env.svc.CreateProfile(env.ctx, profileModel.ProfileRequest{
		Traits: map[string]interface{}{"city": "Kandy"}}, env.orgID)
	var serverErr *errors2.ServerError
	require.ErrorAs(t, err, &serverErr)
	assert.Equal(t, errors2.RecordAttributeHistory.Code, serverErr.Code)
	var count int
	require.NoError(t, env.db.QueryRow(`SELECT COUNT(*) FROM profiles WHERE org_id = ?`, env.orgID).Scan(&count))
	assert.Equal(t, 1, count, "the new profile was rolled back")

	_, err = env.svc.PatchProfile(env.ctx, created.ProfileID, env.orgID,
		map[string]interface{}{"traits": map[string]interface{}{"city": "Galle"}})
	require.ErrorAs(t, err, &serverErr)
	stored, err := env.svc.GetProfile(env.ctx, created.ProfileID, env.orgID)
	require.NoError(t, err)
	assert.Equal(t, "Colombo", stored.Traits["city"], "the update was rolled back")
}
