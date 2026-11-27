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
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	orgModel "github.com/wso2/identity-customer-data-service/internal/organization/model"
	"github.com/wso2/identity-customer-data-service/internal/source/model"
	"github.com/wso2/identity-customer-data-service/internal/system/database"
	"github.com/wso2/identity-customer-data-service/internal/system/database/provider"
	errors2 "github.com/wso2/identity-customer-data-service/internal/system/errors"
	"github.com/wso2/identity-customer-data-service/internal/system/log"
	"github.com/wso2/identity-customer-data-service/test/setup"
)

var sourceOrgCounter atomic.Int64

type sourceEnv struct {
	ctx context.Context
	db  *sql.DB
	org orgModel.Ref
	svc *SourceService
}

func newSourceEnv(t *testing.T) *sourceEnv {
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

	env := &sourceEnv{ctx: context.Background(), db: testDB.DB, svc: &SourceService{}}
	env.org = env.newOrg(t)
	return env
}

func (e *sourceEnv) newOrg(t *testing.T) orgModel.Ref {
	t.Helper()
	handle := fmt.Sprintf("acme-%d", sourceOrgCounter.Add(1))
	org := orgModel.Ref{ID: setup.TestOrgID(handle), Handle: handle}
	require.NoError(t, setup.InsertTestOrganization(e.db, org.ID, handle))
	return org
}

// setClock makes the service stamp every write with the given time.
func setClock(t *testing.T, at time.Time) {
	t.Helper()
	previous := now
	now = func() time.Time { return at }
	t.Cleanup(func() { now = previous })
}

func source(name string) model.Source {
	return model.Source{
		Name:        name,
		Description: "Events from the iOS and Android apps",
		IconURL:     "https://cdn.example.com/icons/mobile.png",
	}
}

func (e *sourceEnv) add(t *testing.T, org orgModel.Ref, name string) *model.Source {
	t.Helper()
	added, err := e.svc.AddSource(e.ctx, org, source(name))
	require.NoError(t, err)
	return added
}

// requireClientError checks that err is a client error with the given status and code.
func requireClientError(t *testing.T, err error, status int, code errors2.ErrorMessage) {
	t.Helper()
	var clientErr *errors2.ClientError
	require.True(t, errors.As(err, &clientErr), "expected a client error, got %v", err)
	assert.Equal(t, status, clientErr.StatusCode)
	assert.Equal(t, code.Code, clientErr.Code)
}

// asClientError checks err as requireClientError does and returns it, for a test that also checks the description.
func asClientError(t *testing.T, err error, status int, code errors2.ErrorMessage) *errors2.ClientError {
	t.Helper()
	requireClientError(t, err, status, code)
	var clientErr *errors2.ClientError
	errors.As(err, &clientErr)
	return clientErr
}

func TestAddSourceGeneratesTheIDAndTimestamps(t *testing.T) {
	env := newSourceEnv(t)
	at := time.Date(2026, 11, 25, 6, 0, 0, 123456000, time.UTC)
	setClock(t, at)

	added, err := env.svc.AddSource(env.ctx, env.org, source("  Mobile App  "))
	require.NoError(t, err)

	_, err = uuid.Parse(added.SourceID)
	assert.NoError(t, err)
	assert.Equal(t, "Mobile App", added.Name, "the name is trimmed")
	assert.Equal(t, env.org.ID, added.OrgID)
	assert.Equal(t, at, added.CreatedAt)
	assert.Equal(t, at, added.UpdatedAt)

	stored, err := env.svc.GetSource(env.ctx, env.org, added.SourceID)
	require.NoError(t, err)
	assert.Equal(t, added.ToResponse(), stored.ToResponse(), "a read returns what the write returned")
}

func TestAddSourceWithoutDescriptionStoresAnEmptyOne(t *testing.T) {
	env := newSourceEnv(t)
	input := source("Web")
	input.Description = ""

	added, err := env.svc.AddSource(env.ctx, env.org, input)
	require.NoError(t, err)

	stored, err := env.svc.GetSource(env.ctx, env.org, added.SourceID)
	require.NoError(t, err)
	assert.Equal(t, "", stored.Description)
}

func TestAddSourceValidatesTheFields(t *testing.T) {
	env := newSourceEnv(t)

	cases := []struct {
		name   string
		modify func(*model.Source)
		code   errors2.ErrorMessage
	}{
		{"empty name", func(s *model.Source) { s.Name = "" }, errors2.SourceNameInvalid},
		{"blank name", func(s *model.Source) { s.Name = "   " }, errors2.SourceNameInvalid},
		{"long name", func(s *model.Source) { s.Name = strings.Repeat("n", model.MaxNameLength+1) },
			errors2.SourceNameInvalid},
		{"missing icon URL", func(s *model.Source) { s.IconURL = "" }, errors2.SourceIconURLInvalid},
		{"http icon URL", func(s *model.Source) { s.IconURL = "http://cdn.example.com/a.png" },
			errors2.SourceIconURLInvalid},
		{"relative icon URL", func(s *model.Source) { s.IconURL = "/icons/a.png" }, errors2.SourceIconURLInvalid},
		{"icon URL without host", func(s *model.Source) { s.IconURL = "https:///a.png" },
			errors2.SourceIconURLInvalid},
		{"long icon URL", func(s *model.Source) {
			s.IconURL = "https://cdn.example.com/" + strings.Repeat("a", model.MaxIconURLLength)
		}, errors2.SourceIconURLInvalid},
		{"long description", func(s *model.Source) {
			s.Description = strings.Repeat("d", model.MaxDescriptionLength+1)
		}, errors2.SourceDescriptionTooLong},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			input := source("Mobile App")
			tc.modify(&input)
			_, err := env.svc.AddSource(env.ctx, env.org, input)
			requireClientError(t, err, http.StatusBadRequest, tc.code)
		})
	}

	sources, err := env.svc.GetSources(env.ctx, env.org)
	require.NoError(t, err)
	assert.Empty(t, sources, "no invalid source is stored")
}

func TestAddSourceAcceptsTheLimits(t *testing.T) {
	env := newSourceEnv(t)
	const iconPrefix = "https://cdn.example.com/"
	input := model.Source{
		Name:        strings.Repeat("n", model.MaxNameLength),
		Description: strings.Repeat("d", model.MaxDescriptionLength),
		IconURL:     iconPrefix + strings.Repeat("a", model.MaxIconURLLength-len(iconPrefix)),
	}

	_, err := env.svc.AddSource(env.ctx, env.org, input)
	assert.NoError(t, err)
}

func TestAddSourceRejectsADuplicateName(t *testing.T) {
	env := newSourceEnv(t)
	env.add(t, env.org, "Mobile App")

	_, err := env.svc.AddSource(env.ctx, env.org, source("Mobile App"))
	clientErr := asClientError(t, err, http.StatusConflict, errors2.SourceAlreadyExists)
	assert.Contains(t, clientErr.Description, "Mobile App")

	// Another organization can use the same name.
	_, err = env.svc.AddSource(env.ctx, env.newOrg(t), source("Mobile App"))
	assert.NoError(t, err)
}

func TestGetSourcesReturnsNewestFirst(t *testing.T) {
	env := newSourceEnv(t)

	sources, err := env.svc.GetSources(env.ctx, env.org)
	require.NoError(t, err)
	assert.NotNil(t, sources, "an org without sources gets an empty list, not nil")
	assert.Empty(t, sources)

	base := time.Date(2026, 11, 25, 6, 0, 0, 0, time.UTC)
	for i, name := range []string{"First", "Second", "Third"} {
		setClock(t, base.Add(time.Duration(i)*time.Minute))
		env.add(t, env.org, name)
	}
	env.add(t, env.newOrg(t), "Other org")

	sources, err = env.svc.GetSources(env.ctx, env.org)
	require.NoError(t, err)
	names := make([]string, 0, len(sources))
	for _, s := range sources {
		names = append(names, s.Name)
	}
	assert.Equal(t, []string{"Third", "Second", "First"}, names)
}

func TestGetSourceChecksTheID(t *testing.T) {
	env := newSourceEnv(t)

	_, err := env.svc.GetSource(env.ctx, env.org, "abc123")
	clientErr := asClientError(t, err, http.StatusBadRequest, errors2.SourceIDInvalid)
	assert.Equal(t, "'abc123' is not a valid UUID.", clientErr.Description)

	missing := uuid.New().String()
	_, err = env.svc.GetSource(env.ctx, env.org, missing)
	clientErr = asClientError(t, err, http.StatusNotFound, errors2.SourceNotFound)
	assert.Equal(t, fmt.Sprintf("No source with ID '%s' exists in organization '%s'.", missing, env.org.Handle),
		clientErr.Description)
}

func TestUpdateSourceDescription(t *testing.T) {
	env := newSourceEnv(t)
	created := time.Date(2026, 11, 25, 6, 0, 0, 0, time.UTC)
	setClock(t, created)
	added := env.add(t, env.org, "Mobile App")

	updatedAt := created.Add(time.Hour)
	setClock(t, updatedAt)
	updated, err := env.svc.UpdateSourceDescription(env.ctx, env.org, added.SourceID, "Mobile apps and the wearable")
	require.NoError(t, err)
	assert.Equal(t, "Mobile apps and the wearable", updated.Description)
	assert.Equal(t, created, updated.CreatedAt)
	assert.Equal(t, updatedAt, updated.UpdatedAt)

	stored, err := env.svc.GetSource(env.ctx, env.org, added.SourceID)
	require.NoError(t, err)
	assert.Equal(t, updated.ToResponse(), stored.ToResponse())
	assert.Equal(t, "Mobile App", stored.Name, "only the description changes")
	assert.Equal(t, added.IconURL, stored.IconURL, "only the description changes")

	// The description can be cleared.
	cleared, err := env.svc.UpdateSourceDescription(env.ctx, env.org, added.SourceID, "")
	require.NoError(t, err)
	assert.Equal(t, "", cleared.Description)
}

func TestUpdateSourceDescriptionErrors(t *testing.T) {
	env := newSourceEnv(t)
	added := env.add(t, env.org, "Mobile App")

	_, err := env.svc.UpdateSourceDescription(env.ctx, env.org, added.SourceID,
		strings.Repeat("d", model.MaxDescriptionLength+1))
	requireClientError(t, err, http.StatusBadRequest, errors2.SourceDescriptionTooLong)

	_, err = env.svc.UpdateSourceDescription(env.ctx, env.org, "not-a-uuid", "x")
	requireClientError(t, err, http.StatusBadRequest, errors2.SourceIDInvalid)

	_, err = env.svc.UpdateSourceDescription(env.ctx, env.org, uuid.New().String(), "x")
	requireClientError(t, err, http.StatusNotFound, errors2.SourceNotFound)
}

func TestDeleteSourceIsIdempotent(t *testing.T) {
	env := newSourceEnv(t)
	added := env.add(t, env.org, "Mobile App")

	require.NoError(t, env.svc.DeleteSource(env.ctx, env.org, added.SourceID))
	_, err := env.svc.GetSource(env.ctx, env.org, added.SourceID)
	requireClientError(t, err, http.StatusNotFound, errors2.SourceNotFound)

	assert.NoError(t, env.svc.DeleteSource(env.ctx, env.org, added.SourceID), "a second delete succeeds")

	err = env.svc.DeleteSource(env.ctx, env.org, "abc123")
	requireClientError(t, err, http.StatusBadRequest, errors2.SourceIDInvalid)
}

func TestAnOrgCannotReachTheSourcesOfAnother(t *testing.T) {
	env := newSourceEnv(t)
	added := env.add(t, env.org, "Mobile App")
	other := env.newOrg(t)

	_, err := env.svc.GetSource(env.ctx, other, added.SourceID)
	requireClientError(t, err, http.StatusNotFound, errors2.SourceNotFound)

	_, err = env.svc.UpdateSourceDescription(env.ctx, other, added.SourceID, "changed")
	requireClientError(t, err, http.StatusNotFound, errors2.SourceNotFound)

	require.NoError(t, env.svc.DeleteSource(env.ctx, other, added.SourceID))
	stored, err := env.svc.GetSource(env.ctx, env.org, added.SourceID)
	require.NoError(t, err, "the delete of another org leaves the source in place")
	assert.Equal(t, added.Description, stored.Description)
}

func TestAChildOrganizationCanOnlyReadSources(t *testing.T) {
	env := newSourceEnv(t)
	handle := fmt.Sprintf("child-%d", sourceOrgCounter.Add(1))
	child := orgModel.Ref{ID: setup.TestOrgID(handle), Handle: handle}
	_, err := env.db.Exec(`INSERT INTO organizations (org_id, org_handle, parent_org_id, primary_org_id, status)
		VALUES ($1, $2, $3, $3, 'ACTIVE')`, child.ID, handle, env.org.ID)
	require.NoError(t, err)

	_, err = env.svc.AddSource(env.ctx, child, source("Mobile App"))
	requireClientError(t, err, http.StatusForbidden, errors2.SubOrgWriteNotAllowed)

	_, err = env.svc.UpdateSourceDescription(env.ctx, child, uuid.New().String(), "x")
	requireClientError(t, err, http.StatusForbidden, errors2.SubOrgWriteNotAllowed)

	err = env.svc.DeleteSource(env.ctx, child, uuid.New().String())
	requireClientError(t, err, http.StatusForbidden, errors2.SubOrgWriteNotAllowed)

	sources, err := env.svc.GetSources(env.ctx, child)
	require.NoError(t, err)
	assert.Empty(t, sources)
}
