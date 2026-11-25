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
	"context"
	"errors"
	"net/http"
	"strings"

	integrationModel "github.com/wso2/identity-customer-data-service/internal/integration/model"
	integrationService "github.com/wso2/identity-customer-data-service/internal/integration/service"
	errors2 "github.com/wso2/identity-customer-data-service/internal/system/errors"
)

// PoC V2 error codes for ?source= on the profile API.
var (
	sourceNotRegistered = errors2.ErrorMessage{
		Code:    "CDS-90001",
		Message: "Invalid source.",
	}
	sourceWithoutTransformation = errors2.ErrorMessage{
		Code:    "CDS-90002",
		Message: "Source has no transformation.",
	}
	sourceTransformationInactive = errors2.ErrorMessage{
		Code:    "CDS-90003",
		Message: "Transformation not active.",
	}
)

// resolveSource looks up ?source=<id> for the profile API (PoC V2). It returns
// the source and the name of its linked transformation, or a client error
// that says why the source can't be used.
func resolveSource(ctx context.Context, orgHandle, sourceId string) (*integrationModel.Source, string, error) {

	source, transformation, err := integrationService.ResolveSource(ctx, orgHandle, sourceId)
	if err == nil {
		return source, transformation.Name, nil
	}
	var message errors2.ErrorMessage
	var cause error
	switch {
	case errors.Is(err, integrationService.ErrUnknownSource):
		message, cause = sourceNotRegistered, integrationService.ErrUnknownSource
	case errors.Is(err, integrationService.ErrNoTransformation):
		message, cause = sourceWithoutTransformation, integrationService.ErrNoTransformation
	case errors.Is(err, integrationService.ErrTransformationInactive):
		message, cause = sourceTransformationInactive, integrationService.ErrTransformationInactive
	default:
		return nil, "", err
	}
	// The description is the detail only: "source 'x' is not registered ...".
	message.Description = strings.TrimPrefix(err.Error(), cause.Error()+": ")
	return nil, "", errors2.NewClientError(message, http.StatusBadRequest)
}
