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

package store

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wso2/identity-customer-data-service/internal/source/model"
	"github.com/wso2/identity-customer-data-service/internal/system/database"
	"github.com/wso2/identity-customer-data-service/internal/system/database/provider"
	"github.com/wso2/identity-customer-data-service/internal/system/log"
	"github.com/wso2/identity-customer-data-service/test/setup"
)

const historyOrg = "org-history"

var (
	at1 = time.Date(2026, 11, 20, 10, 0, 0, 0, time.UTC)
	at2 = time.Date(2026, 11, 25, 9, 0, 0, 0, time.UTC)
	at3 = time.Date(2026, 11, 27, 9, 15, 0, 0, time.UTC)
)

func newHistoryDB(t *testing.T) *sql.DB {
	t.Helper()
	require.NoError(t, log.Init("ERROR"))
	testDB, err := setup.SetupTestSQLite()
	require.NoError(t, err)
	provider.SetTestDB(testDB.DB, database.TypeSQLite)
	t.Cleanup(func() {
		provider.SetTestDB(nil, "")
		testDB.Terminate()
	})
	return testDB.DB
}

// addProfile inserts a bare profile row; the history table needs one for its foreign key.
func addProfile(t *testing.T, db *sql.DB) string {
	t.Helper()
	id := uuid.New().String()
	_, err := db.Exec(`INSERT INTO profiles (profile_id, org_id) VALUES (?, ?)`, id, historyOrg)
	require.NoError(t, err)
	return id
}

func strp(s string) *string { return &s }

func change(profileID, attribute, kind string, sourceID, sourceName *string, at time.Time) model.AttributeChange {
	return model.AttributeChange{ProfileID: profileID, Attribute: attribute, Action: kind, SourceID: sourceID,
		SourceName: sourceName, ChangedAt: at}
}

func withoutIDs(changes []model.AttributeChange) []model.AttributeChange {
	out := make([]model.AttributeChange, len(changes))
	for i, c := range changes {
		c.ChangeID = ""
		out[i] = c
	}
	return out
}

func TestAttributeHistoryIsReadNewestFirst(t *testing.T) {
	db := newHistoryDB(t)
	ctx := context.Background()
	profileID := addProfile(t, db)
	crm, booking := strp(uuid.New().String()), strp(uuid.New().String())

	email1 := change(profileID, "traits.email", model.ActionSet, crm, strp("CRM"), at1)
	city := change(profileID, "traits.city", model.ActionSet, crm, strp("CRM"), at1)
	email2 := change(profileID, "traits.email", model.ActionSet, booking, strp("Booking App"), at2)
	phone := change(profileID, "traits.phone", model.ActionRemoved, nil, nil, at3)
	require.NoError(t, InsertAttributeChanges(ctx, historyOrg, []model.AttributeChange{email1, city, email2, phone}))

	history, err := GetAttributeChanges(ctx, profileID, historyOrg, "")
	require.NoError(t, err)
	for _, c := range history {
		_, err := uuid.Parse(c.ChangeID)
		assert.NoError(t, err, "every change gets an ID")
	}
	// Newest first; the two changes at at1 are ordered by attribute. A change without a source keeps nil.
	assert.Equal(t, []model.AttributeChange{phone, email2, city, email1}, withoutIDs(history))

	only, err := GetAttributeChanges(ctx, profileID, historyOrg, "traits.email")
	require.NoError(t, err)
	assert.Equal(t, []model.AttributeChange{email2, email1}, withoutIDs(only))

	none, err := GetAttributeChanges(ctx, profileID, "another-org", "")
	require.NoError(t, err)
	assert.Empty(t, none, "another org reads nothing")
	assert.NotNil(t, none)
}

func TestAGivenChangeIDIsKept(t *testing.T) {
	db := newHistoryDB(t)
	ctx := context.Background()
	profileID := addProfile(t, db)
	copied := change(profileID, "traits.email", model.ActionSet, nil, nil, at1)
	copied.ChangeID = uuid.New().String()
	require.NoError(t, InsertAttributeChanges(ctx, historyOrg, []model.AttributeChange{copied}))

	history, err := GetAttributeChanges(ctx, profileID, historyOrg, "")
	require.NoError(t, err)
	assert.Equal(t, []model.AttributeChange{copied}, history)
}

func TestAttributeHistoryIsWrittenWithTheTransaction(t *testing.T) {
	db := newHistoryDB(t)
	ctx := context.Background()
	profileID := addProfile(t, db)
	client, err := provider.NewDBProvider().GetDBClient()
	require.NoError(t, err)
	defer func() { _ = client.Close() }()

	tx, err := client.BeginTxContext(ctx)
	require.NoError(t, err)
	require.NoError(t, InsertAttributeChangesTx(ctx, tx, historyOrg, []model.AttributeChange{
		change(profileID, "traits.email", model.ActionSet, nil, nil, at1)}))
	require.NoError(t, tx.Rollback())

	history, err := GetAttributeChanges(ctx, profileID, historyOrg, "")
	require.NoError(t, err)
	assert.Empty(t, history, "a rolled back write leaves no history")
}

func TestABadChangeFailsTheWholeWrite(t *testing.T) {
	db := newHistoryDB(t)
	ctx := context.Background()
	profileID := addProfile(t, db)

	err := InsertAttributeChanges(ctx, historyOrg, []model.AttributeChange{
		change(profileID, "traits.email", model.ActionSet, nil, nil, at1),
		change(profileID, "traits.city", "renamed", nil, nil, at1), // not SET, REMOVED or MERGED
	})
	assert.Error(t, err)
	err = InsertAttributeChanges(ctx, historyOrg, []model.AttributeChange{
		change(uuid.New().String(), "traits.email", model.ActionSet, nil, nil, at1), // no such profile
	})
	assert.Error(t, err)

	history, err := GetAttributeChanges(ctx, profileID, historyOrg, "")
	require.NoError(t, err)
	assert.Empty(t, history, "the first change was rolled back with the second")
}

func TestAttributeHistoryGoesWithTheProfile(t *testing.T) {
	db := newHistoryDB(t)
	ctx := context.Background()
	profileID := addProfile(t, db)
	require.NoError(t, InsertAttributeChanges(ctx, historyOrg, []model.AttributeChange{
		change(profileID, "traits.email", model.ActionSet, nil, nil, at1)}))

	_, err := db.Exec(`DELETE FROM profiles WHERE profile_id = ?`, profileID)
	require.NoError(t, err)
	var count int
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM profile_attribute_sources WHERE profile_id = ?`,
		profileID).Scan(&count))
	assert.Equal(t, 0, count)
}

func TestNothingToInsertIsNotAWrite(t *testing.T) {
	newHistoryDB(t)
	assert.NoError(t, InsertAttributeChanges(context.Background(), historyOrg, nil))
}

func TestTheActionColumnTakesOnlyTheThreeActions(t *testing.T) {
	db := newHistoryDB(t)
	ctx := context.Background()
	profileID := addProfile(t, db)

	for _, action := range []string{model.ActionSet, model.ActionRemoved, model.ActionMerged} {
		assert.NoError(t, InsertAttributeChanges(ctx, historyOrg, []model.AttributeChange{
			change(profileID, "traits.email", action, nil, nil, at1)}), action)
	}
	for _, action := range []string{"set", "removed", "merged", "UPDATED", ""} {
		assert.Error(t, InsertAttributeChanges(ctx, historyOrg, []model.AttributeChange{
			change(profileID, "traits.email", action, nil, nil, at1)}), "%q is not an action", action)
	}

	history, err := GetAttributeChanges(ctx, profileID, historyOrg, "")
	require.NoError(t, err)
	actions := map[string]bool{}
	for _, c := range history {
		actions[c.Action] = true
	}
	assert.Equal(t, map[string]bool{"SET": true, "REMOVED": true, "MERGED": true}, actions,
		"the action is read back from the action column")
}
