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

	"github.com/google/uuid"

	orgModel "github.com/wso2/identity-customer-data-service/internal/organization/model"
	"github.com/wso2/identity-customer-data-service/internal/source/model"
	"github.com/wso2/identity-customer-data-service/internal/source/store"
	errors2 "github.com/wso2/identity-customer-data-service/internal/system/errors"
)

// ResolveRequestSource returns the source a profile request names with ?source=. The source must be
// registered in the org (else CDS-18010). A source without a transformation is valid; one with a
// transformation needs it to be active (else CDS-18012), so a request fails before anything is stored.
func (s *SourceService) ResolveRequestSource(ctx context.Context, org orgModel.Ref, sourceID string) (
	*model.Source, error) {
	parsed, err := uuid.Parse(sourceID)
	if err != nil || len(sourceID) != 36 {
		return nil, notRegistered(org, sourceID)
	}
	source, err := store.GetSourceByID(ctx, parsed.String(), org.ID)
	if err != nil {
		return nil, err
	}
	if source == nil {
		return nil, notRegistered(org, sourceID)
	}
	if source.TransformationID == nil {
		return source, nil
	}

	transformation, err := s.transformations.GetTransformation(ctx, org.ID, *source.TransformationID)
	switch {
	case errors.Is(err, ErrLinksNotConfigured):
		return nil, inactive(fmt.Sprintf("Source '%s' is linked to transformation '%s', but transformations "+
			"are not available.", source.Name, *source.TransformationID))
	case err != nil:
		return nil, err
	case transformation == nil:
		return nil, inactive(fmt.Sprintf("Transformation '%s' linked to source '%s' no longer exists.",
			*source.TransformationID, source.Name))
	case !transformation.Active:
		return nil, inactive(fmt.Sprintf("Transformation '%s' linked to source '%s' is not active.",
			*source.TransformationID, source.Name))
	}
	return source, nil
}

// ToCDSFormat turns a payload in the source's format into CDS format through the source's transformation.
// A source without a transformation sends CDS format already, so its payload is returned as it is.
func (s *SourceService) ToCDSFormat(ctx context.Context, org orgModel.Ref, source model.Source,
	payload map[string]any) (map[string]any, error) {
	if source.TransformationID == nil {
		return payload, nil
	}
	out, err := s.transformations.Transform(ctx, org.ID, *source.TransformationID, payload)
	if err != nil {
		return nil, transformError(err, source, "to CDS format")
	}
	return out, nil
}

// ToSourceFormat turns a profile in CDS format into the source's format, through the reverse of its
// transformation. A source without a transformation gets the profile as it is.
func (s *SourceService) ToSourceFormat(ctx context.Context, org orgModel.Ref, source model.Source,
	profile map[string]any) (map[string]any, error) {
	if source.TransformationID == nil {
		return profile, nil
	}
	out, err := s.transformations.ReverseTransform(ctx, org.ID, *source.TransformationID, profile)
	if err != nil {
		return nil, transformError(err, source, "to the source's format")
	}
	return out, nil
}

func notRegistered(org orgModel.Ref, sourceID string) error {
	return clientError(errors2.SourceNotRegistered, http.StatusBadRequest,
		fmt.Sprintf("Source '%s' is not registered for organization '%s'.", sourceID, org.Handle))
}

func inactive(description string) error {
	return clientError(errors2.SourceTransformationInactive, http.StatusBadRequest, description)
}

// transformError passes a client error from the transformation on, and makes anything else a server error.
func transformError(err error, source model.Source, direction string) error {
	var clientErr *errors2.ClientError
	if errors.As(err, &clientErr) {
		return err
	}
	return errors2.NewServerError(errors2.ErrorMessage{
		Code:    errors2.TransformSourcePayload.Code,
		Message: errors2.TransformSourcePayload.Message,
		Description: fmt.Sprintf("Failed to transform the payload of source '%s' %s.", source.SourceID,
			direction),
	}, err)
}
