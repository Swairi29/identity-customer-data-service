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
	"fmt"
	"net/http"
	"strings"

	"github.com/google/uuid"

	errors2 "github.com/wso2/identity-customer-data-service/internal/system/errors"
)

// A source links to a transformation and to destinations, which other modules own. The source service
// reaches them only through the two interfaces below, so it does not depend on those modules. Until a
// module exists, its "not configured" default is used: it accepts no links, so no source can point to
// something that does not exist.

// ErrLinksNotConfigured is returned by a lookup whose module is not available in this deployment.
var ErrLinksNotConfigured = errors.New("module is not configured")

// LinkedTransformation is what the source service needs to know about a transformation.
type LinkedTransformation struct {
	ID     string
	Active bool
}

// TransformationLookup is what the source service needs from the transformations module.
type TransformationLookup interface {
	// GetTransformation returns the transformation of the org with this ID, or nil when there is none.
	GetTransformation(ctx context.Context, orgID, transformationID string) (*LinkedTransformation, error)
	// Transform runs a payload in the source's format through the transformation and returns it in
	// CDS format.
	Transform(ctx context.Context, orgID, transformationID string, payload map[string]any) (map[string]any, error)
	// ReverseTransform turns a profile in CDS format back into the source's format.
	ReverseTransform(ctx context.Context, orgID, transformationID string, profile map[string]any) (map[string]any,
		error)
}

// DestinationLookup is what the source service needs from the destinations module.
type DestinationLookup interface {
	// MissingDestinations returns the IDs that are not destinations of the org, in the order given.
	MissingDestinations(ctx context.Context, orgID string, destinationIDs []string) ([]string, error)
}

// notConfiguredTransformations is the TransformationLookup used until the transformations module exists.
type notConfiguredTransformations struct{}

func (notConfiguredTransformations) GetTransformation(context.Context, string, string) (*LinkedTransformation,
	error) {
	return nil, ErrLinksNotConfigured
}

func (notConfiguredTransformations) Transform(context.Context, string, string, map[string]any) (map[string]any,
	error) {
	return nil, ErrLinksNotConfigured
}

func (notConfiguredTransformations) ReverseTransform(context.Context, string, string, map[string]any) (
	map[string]any, error) {
	return nil, ErrLinksNotConfigured
}

// notConfiguredDestinations is the DestinationLookup used until the destinations module exists.
type notConfiguredDestinations struct{}

func (notConfiguredDestinations) MissingDestinations(context.Context, string, []string) ([]string, error) {
	return nil, ErrLinksNotConfigured
}

// validateTransformationLink checks that the ID, when given, is a transformation of the org, and returns it
// in canonical form. nil means no transformation is linked and is always valid.
func validateTransformationLink(ctx context.Context, lookup TransformationLookup, orgID string,
	transformationID *string) (*string, error) {
	if transformationID == nil {
		return nil, nil
	}
	parsed, err := uuid.Parse(*transformationID)
	if err != nil || len(*transformationID) != 36 {
		return nil, clientError(errors2.SourceTransformationInvalid, http.StatusBadRequest,
			fmt.Sprintf("'%s' is not a valid UUID.", *transformationID))
	}
	id := parsed.String()

	transformation, err := lookup.GetTransformation(ctx, orgID, id)
	if errors.Is(err, ErrLinksNotConfigured) {
		return nil, clientError(errors2.SourceTransformationInvalid, http.StatusBadRequest,
			"Transformations are not available yet, so 'transformation_id' must be null or omitted.")
	}
	if err != nil {
		return nil, err
	}
	if transformation == nil {
		return nil, clientError(errors2.SourceTransformationInvalid, http.StatusBadRequest,
			fmt.Sprintf("No transformation with ID '%s' exists in this organization.", id))
	}
	return &id, nil
}

// validateDestinationLinks checks that every ID is a destination of the org and none is repeated, and
// returns them in canonical form. An empty list links no destinations and is always valid.
func validateDestinationLinks(ctx context.Context, lookup DestinationLookup, orgID string,
	destinationIDs []string) ([]string, error) {
	if len(destinationIDs) == 0 {
		return []string{}, nil
	}
	ids := make([]string, 0, len(destinationIDs))
	seen := make(map[string]bool, len(destinationIDs))
	for _, raw := range destinationIDs {
		parsed, err := uuid.Parse(raw)
		if err != nil || len(raw) != 36 {
			return nil, clientError(errors2.SourceDestinationInvalid, http.StatusBadRequest,
				fmt.Sprintf("'%s' in 'destination_ids' is not a valid UUID.", raw))
		}
		id := parsed.String()
		if seen[id] {
			return nil, clientError(errors2.SourceDestinationInvalid, http.StatusBadRequest,
				fmt.Sprintf("'%s' appears more than once in 'destination_ids'.", id))
		}
		seen[id] = true
		ids = append(ids, id)
	}

	missing, err := lookup.MissingDestinations(ctx, orgID, ids)
	if errors.Is(err, ErrLinksNotConfigured) {
		return nil, clientError(errors2.SourceDestinationInvalid, http.StatusBadRequest,
			"Destinations are not available yet, so 'destination_ids' must be empty or omitted.")
	}
	if err != nil {
		return nil, err
	}
	if len(missing) > 0 {
		return nil, clientError(errors2.SourceDestinationInvalid, http.StatusBadRequest,
			fmt.Sprintf("No destination with ID '%s' exists in this organization.",
				strings.Join(missing, "', '")))
	}
	return ids, nil
}
