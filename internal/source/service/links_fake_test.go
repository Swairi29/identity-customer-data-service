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

import "context"

// fakeTransformations is a TransformationLookup for tests: the transformations each org has, by ID, and
// whether each is active. err, when set, is returned by every call.
type fakeTransformations struct {
	byOrg map[string]map[string]bool
	err   error
}

func (f *fakeTransformations) add(orgID, transformationID string, active bool) {
	if f.byOrg == nil {
		f.byOrg = map[string]map[string]bool{}
	}
	if f.byOrg[orgID] == nil {
		f.byOrg[orgID] = map[string]bool{}
	}
	f.byOrg[orgID][transformationID] = active
}

func (f *fakeTransformations) GetTransformation(_ context.Context, orgID, transformationID string) (
	*LinkedTransformation, error) {
	if f.err != nil {
		return nil, f.err
	}
	active, ok := f.byOrg[orgID][transformationID]
	if !ok {
		return nil, nil
	}
	return &LinkedTransformation{ID: transformationID, Active: active}, nil
}

// Transform copies the payload and marks it, so a test can tell it went through the transformation.
func (f *fakeTransformations) Transform(_ context.Context, _, transformationID string,
	payload map[string]any) (map[string]any, error) {
	if f.err != nil {
		return nil, f.err
	}
	out := make(map[string]any, len(payload)+1)
	for k, v := range payload {
		out[k] = v
	}
	out["transformed_by"] = transformationID
	return out, nil
}

// fakeDestinations is a DestinationLookup for tests: the destination IDs each org has. err, when set, is
// returned by every call.
type fakeDestinations struct {
	byOrg map[string]map[string]bool
	err   error
}

func (f *fakeDestinations) add(orgID string, destinationIDs ...string) {
	if f.byOrg == nil {
		f.byOrg = map[string]map[string]bool{}
	}
	if f.byOrg[orgID] == nil {
		f.byOrg[orgID] = map[string]bool{}
	}
	for _, id := range destinationIDs {
		f.byOrg[orgID][id] = true
	}
}

func (f *fakeDestinations) MissingDestinations(_ context.Context, orgID string, destinationIDs []string) (
	[]string, error) {
	if f.err != nil {
		return nil, f.err
	}
	var missing []string
	for _, id := range destinationIDs {
		if !f.byOrg[orgID][id] {
			missing = append(missing, id)
		}
	}
	return missing, nil
}

// ReverseTransform copies the profile and marks it, so a test can tell it went back through the
// transformation.
func (f *fakeTransformations) ReverseTransform(_ context.Context, _, transformationID string,
	profile map[string]any) (map[string]any, error) {
	if f.err != nil {
		return nil, f.err
	}
	out := make(map[string]any, len(profile)+1)
	for k, v := range profile {
		out[k] = v
	}
	out["reversed_by"] = transformationID
	return out, nil
}
