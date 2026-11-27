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
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	errors2 "github.com/wso2/identity-customer-data-service/internal/system/errors"
)

const (
	linkOrg      = "org-a"
	otherLinkOrg = "org-b"
	transformA   = "7b1e0c2a-5d4f-4e8b-9a3c-2f6d8e1b4a70"
	destinationA = "c2d9a4e1-6b3f-4a7e-8d5c-1e9f0b2a3c4d"
	destinationB = "9a8b7c6d-5e4f-4a3b-8c2d-1e0f9a8b7c6d"
)

func ptr(s string) *string { return &s }

// requireLinkError checks that err is a 400 client error with the given code whose description contains want.
func requireLinkError(t *testing.T, err error, code errors2.ErrorMessage, want string) {
	t.Helper()
	var clientErr *errors2.ClientError
	require.True(t, errors.As(err, &clientErr), "want a client error, got %v", err)
	assert.Equal(t, http.StatusBadRequest, clientErr.StatusCode)
	assert.Equal(t, code.Code, clientErr.ErrorMessage.Code)
	assert.Contains(t, clientErr.ErrorMessage.Description, want)
}

func TestGetSourceService_UsesNotConfiguredLinks(t *testing.T) {
	svc := GetSourceService().(*SourceService)
	assert.IsType(t, notConfiguredTransformations{}, svc.transformations)
	assert.IsType(t, notConfiguredDestinations{}, svc.destinations)
}

func TestValidateTransformationLink(t *testing.T) {
	ctx := context.Background()
	fake := &fakeTransformations{}
	fake.add(linkOrg, transformA, true)

	t.Run("no transformation is always valid", func(t *testing.T) {
		for _, lookup := range []TransformationLookup{fake, notConfiguredTransformations{}} {
			id, err := validateTransformationLink(ctx, lookup, linkOrg, nil)
			require.NoError(t, err)
			assert.Nil(t, id)
		}
	})
	t.Run("a transformation of the org is returned in canonical form", func(t *testing.T) {
		id, err := validateTransformationLink(ctx, fake, linkOrg, ptr(strings.ToUpper(transformA)))
		require.NoError(t, err)
		assert.Equal(t, transformA, *id)
	})
	t.Run("an inactive transformation can still be linked", func(t *testing.T) {
		inactive := &fakeTransformations{}
		inactive.add(linkOrg, transformA, false)
		_, err := validateTransformationLink(ctx, inactive, linkOrg, ptr(transformA))
		require.NoError(t, err)
	})
	t.Run("not a UUID", func(t *testing.T) {
		_, err := validateTransformationLink(ctx, fake, linkOrg, ptr("abc123"))
		requireLinkError(t, err, errors2.SourceTransformationInvalid, "'abc123' is not a valid UUID.")
	})
	t.Run("another org's transformation", func(t *testing.T) {
		_, err := validateTransformationLink(ctx, fake, otherLinkOrg, ptr(transformA))
		requireLinkError(t, err, errors2.SourceTransformationInvalid, "No transformation with ID")
	})
	t.Run("transformations not configured", func(t *testing.T) {
		_, err := validateTransformationLink(ctx, notConfiguredTransformations{}, linkOrg, ptr(transformA))
		requireLinkError(t, err, errors2.SourceTransformationInvalid, "Transformations are not available yet")
	})
	t.Run("a lookup failure is passed on as a server error", func(t *testing.T) {
		boom := errors.New("db down")
		_, err := validateTransformationLink(ctx, &fakeTransformations{err: boom}, linkOrg, ptr(transformA))
		assert.ErrorIs(t, err, boom)
	})
}

func TestValidateDestinationLinks(t *testing.T) {
	ctx := context.Background()
	fake := &fakeDestinations{}
	fake.add(linkOrg, destinationA, destinationB)

	t.Run("no destinations is always valid", func(t *testing.T) {
		for _, lookup := range []DestinationLookup{fake, notConfiguredDestinations{}} {
			for _, ids := range [][]string{nil, {}} {
				got, err := validateDestinationLinks(ctx, lookup, linkOrg, ids)
				require.NoError(t, err)
				assert.Equal(t, []string{}, got)
			}
		}
	})
	t.Run("destinations of the org are returned in order and canonical form", func(t *testing.T) {
		got, err := validateDestinationLinks(ctx, fake, linkOrg, []string{destinationB, strings.ToUpper(destinationA)})
		require.NoError(t, err)
		assert.Equal(t, []string{destinationB, destinationA}, got)
	})
	t.Run("not a UUID", func(t *testing.T) {
		_, err := validateDestinationLinks(ctx, fake, linkOrg, []string{destinationA, "abc123"})
		requireLinkError(t, err, errors2.SourceDestinationInvalid, "'abc123' in 'destination_ids' is not a valid UUID.")
	})
	t.Run("repeated, also in another case", func(t *testing.T) {
		_, err := validateDestinationLinks(ctx, fake, linkOrg, []string{destinationA, strings.ToUpper(destinationA)})
		requireLinkError(t, err, errors2.SourceDestinationInvalid, "appears more than once")
	})
	t.Run("another org's destinations are named", func(t *testing.T) {
		_, err := validateDestinationLinks(ctx, fake, otherLinkOrg, []string{destinationA, destinationB})
		requireLinkError(t, err, errors2.SourceDestinationInvalid,
			"No destination with ID '"+destinationA+"', '"+destinationB+"' exists")
	})
	t.Run("destinations not configured", func(t *testing.T) {
		_, err := validateDestinationLinks(ctx, notConfiguredDestinations{}, linkOrg, []string{destinationA})
		requireLinkError(t, err, errors2.SourceDestinationInvalid, "Destinations are not available yet")
	})
	t.Run("a lookup failure is passed on as a server error", func(t *testing.T) {
		boom := errors.New("db down")
		_, err := validateDestinationLinks(ctx, &fakeDestinations{err: boom}, linkOrg, []string{destinationA})
		assert.ErrorIs(t, err, boom)
	})
}

func TestFakeTransformations_Transform(t *testing.T) {
	fake := &fakeTransformations{}
	out, err := fake.Transform(context.Background(), linkOrg, transformA, map[string]any{"mail": "a@b.c"})
	require.NoError(t, err)
	assert.Equal(t, map[string]any{"mail": "a@b.c", "transformed_by": transformA}, out)

	_, err = notConfiguredTransformations{}.Transform(context.Background(), linkOrg, transformA, nil)
	assert.ErrorIs(t, err, ErrLinksNotConfigured)
}
