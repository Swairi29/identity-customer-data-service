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

package model

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSourcePatchTellsAbsentFromNull(t *testing.T) {
	cases := []struct {
		body    string
		set     bool
		value   *string
		isEmpty bool
	}{
		{`{}`, false, nil, true},
		{`{"description":"x"}`, false, nil, false},
		{`{"transformation_id":null}`, true, nil, false},
		{`{"transformation_id":"7b1e0c2a-5d4f-4e8b-9a3c-2f6d8e1b4a70"}`, true,
			strPtr("7b1e0c2a-5d4f-4e8b-9a3c-2f6d8e1b4a70"), false},
	}
	for _, c := range cases {
		var patch SourcePatch
		require.NoError(t, json.Unmarshal([]byte(c.body), &patch), c.body)
		assert.Equal(t, c.set, patch.TransformationID.Set, c.body)
		assert.Equal(t, c.value, patch.TransformationID.Value, c.body)
		assert.Equal(t, c.isEmpty, patch.IsEmpty(), c.body)
	}

	var patch SourcePatch
	assert.Error(t, json.Unmarshal([]byte(`{"transformation_id":42}`), &patch), "a number is not an ID")
}

func strPtr(s string) *string { return &s }

func TestSourcePatchDestinationIDs(t *testing.T) {
	cases := []struct {
		body   string
		set    bool
		values []string
	}{
		{`{"description":"x"}`, false, nil},
		{`{"destination_ids":[]}`, true, []string{}},
		{`{"destination_ids":["a","b"]}`, true, []string{"a", "b"}},
	}
	for _, c := range cases {
		var patch SourcePatch
		require.NoError(t, json.Unmarshal([]byte(c.body), &patch), c.body)
		assert.Equal(t, c.set, patch.DestinationIDs.Set, c.body)
		assert.Equal(t, c.values, patch.DestinationIDs.Values, c.body)
		assert.False(t, patch.IsEmpty(), c.body)
	}

	for _, body := range []string{`{"destination_ids":null}`, `{"destination_ids":"a"}`, `{"destination_ids":[1]}`} {
		var patch SourcePatch
		err := json.Unmarshal([]byte(body), &patch)
		var typeErr *json.UnmarshalTypeError
		require.ErrorAs(t, err, &typeErr, body)
		assert.Equal(t, "destination_ids", typeErr.Field, body)
	}
}

func TestToResponseNeverReturnsNullDestinations(t *testing.T) {
	assert.Equal(t, []string{}, Source{}.ToResponse().DestinationIDs)
}
