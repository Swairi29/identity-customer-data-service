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
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wso2/identity-customer-data-service/internal/source/model"
	errors2 "github.com/wso2/identity-customer-data-service/internal/system/errors"
)

func TestResolveRequestSource(t *testing.T) {
	env := newSourceEnv(t)
	fake := env.withTransformations(transformA)
	plain := env.add(t, env.org, "Plain App")
	input := source("Linked App")
	input.TransformationID = ptr(transformA)
	linked, err := env.svc.AddSource(env.ctx, env.org, input)
	require.NoError(t, err)

	got, err := env.svc.ResolveRequestSource(env.ctx, env.org, strings.ToUpper(plain.SourceID))
	require.NoError(t, err)
	assert.Equal(t, plain.SourceID, got.SourceID, "a source without a transformation is valid")

	got, err = env.svc.ResolveRequestSource(env.ctx, env.org, linked.SourceID)
	require.NoError(t, err)
	assert.Equal(t, transformA, *got.TransformationID)

	for _, id := range []string{"abc123", uuid.New().String()} {
		_, err = env.svc.ResolveRequestSource(env.ctx, env.org, id)
		requireClientError(t, err, http.StatusBadRequest, errors2.SourceNotRegistered)
	}

	// The linked transformation is paused, gone, or transformations are switched off.
	fake.add(env.org.ID, transformA, false)
	_, err = env.svc.ResolveRequestSource(env.ctx, env.org, linked.SourceID)
	assert.Contains(t, asClientError(t, err, http.StatusBadRequest, errors2.SourceTransformationInactive).Description,
		"is not active")

	delete(fake.byOrg[env.org.ID], transformA)
	_, err = env.svc.ResolveRequestSource(env.ctx, env.org, linked.SourceID)
	assert.Contains(t, asClientError(t, err, http.StatusBadRequest, errors2.SourceTransformationInactive).Description,
		"no longer exists")

	unavailable := GetSourceService()
	_, err = unavailable.ResolveRequestSource(env.ctx, env.org, linked.SourceID)
	assert.Contains(t, asClientError(t, err, http.StatusBadRequest, errors2.SourceTransformationInactive).Description,
		"transformations are not available")
}

func TestPayloadsGoThroughTheTransformationOnlyWhenLinked(t *testing.T) {
	env := newSourceEnv(t)
	fake := env.withTransformations(transformA)
	payload := map[string]any{"mail": "a@b.c"}

	plain := model.Source{SourceID: uuid.New().String()}
	out, err := env.svc.ToCDSFormat(env.ctx, env.org, plain, payload)
	require.NoError(t, err)
	assert.Equal(t, payload, out)
	out, err = env.svc.ToSourceFormat(env.ctx, env.org, plain, payload)
	require.NoError(t, err)
	assert.Equal(t, payload, out)

	linked := model.Source{SourceID: uuid.New().String(), TransformationID: ptr(transformA)}
	out, err = env.svc.ToCDSFormat(env.ctx, env.org, linked, payload)
	require.NoError(t, err)
	assert.Equal(t, transformA, out["transformed_by"])
	out, err = env.svc.ToSourceFormat(env.ctx, env.org, linked, payload)
	require.NoError(t, err)
	assert.Equal(t, transformA, out["reversed_by"])

	// A failing transformation is a server error; a client error from it is passed on.
	fake.err = errors.New("mapping engine down")
	_, err = env.svc.ToCDSFormat(env.ctx, env.org, linked, payload)
	var serverErr *errors2.ServerError
	require.ErrorAs(t, err, &serverErr)
	assert.Equal(t, errors2.TransformSourcePayload.Code, serverErr.Code)

	fake.err = errors2.NewClientError(errors2.BadRequest, http.StatusBadRequest)
	_, err = env.svc.ToCDSFormat(env.ctx, env.org, linked, payload)
	requireClientError(t, err, http.StatusBadRequest, errors2.BadRequest)
}
