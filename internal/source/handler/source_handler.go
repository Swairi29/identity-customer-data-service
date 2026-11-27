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

// Package handler serves the source endpoints.
package handler

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	orgService "github.com/wso2/identity-customer-data-service/internal/organization/service"
	"github.com/wso2/identity-customer-data-service/internal/source/model"
	"github.com/wso2/identity-customer-data-service/internal/source/provider"
	"github.com/wso2/identity-customer-data-service/internal/system/audit"
	"github.com/wso2/identity-customer-data-service/internal/system/config"
	"github.com/wso2/identity-customer-data-service/internal/system/constants"
	errors2 "github.com/wso2/identity-customer-data-service/internal/system/errors"
	"github.com/wso2/identity-customer-data-service/internal/system/security"
	"github.com/wso2/identity-customer-data-service/internal/system/utils"
)

// SourceHandler handles the source endpoints.
type SourceHandler struct{}

// NewSourceHandler returns a new SourceHandler instance.
func NewSourceHandler() *SourceHandler {
	return &SourceHandler{}
}

// AddSource handles POST /sources.
func (h *SourceHandler) AddSource(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if err := security.AuthnAndAuthz(r, "source:create"); err != nil {
		utils.HandleError(w, err)
		return
	}

	var req model.SourceRequest
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		utils.HandleError(w, badRequest(errors2.AddSourceBadRequest, utils.HandleDecodeError(err, "source")))
		return
	}

	org, err := orgService.ResolveOrganization(ctx, utils.ExtractOrgHandleFromPath(r))
	if err != nil {
		utils.HandleError(w, err)
		return
	}
	source, err := provider.NewSourceProvider().GetSourceService().AddSource(ctx, org, req.ToSource(org.ID))
	if err != nil {
		utils.HandleError(w, err)
		return
	}

	audit.SetTargetID(ctx, source.SourceID)
	w.Header().Set("Location", sourceLocation(ctx, org.Handle, source.SourceID))
	writeJSON(w, http.StatusCreated, source.ToResponse())
}

// GetAllSources handles GET /sources.
func (h *SourceHandler) GetAllSources(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if err := security.AuthnAndAuthz(r, "source:view"); err != nil {
		utils.HandleError(w, err)
		return
	}
	org, err := orgService.ResolveOrganization(ctx, utils.ExtractOrgHandleFromPath(r))
	if err != nil {
		utils.HandleError(w, err)
		return
	}
	sources, err := provider.NewSourceProvider().GetSourceService().GetSources(ctx, org)
	if err != nil {
		utils.HandleError(w, err)
		return
	}
	responses := make([]model.SourceResponse, 0, len(sources))
	for _, source := range sources {
		responses = append(responses, source.ToResponse())
	}
	writeJSON(w, http.StatusOK, responses)
}

// GetSource handles GET /sources/{sourceId}.
func (h *SourceHandler) GetSource(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if err := security.AuthnAndAuthz(r, "source:view"); err != nil {
		utils.HandleError(w, err)
		return
	}
	org, err := orgService.ResolveOrganization(ctx, utils.ExtractOrgHandleFromPath(r))
	if err != nil {
		utils.HandleError(w, err)
		return
	}
	source, err := provider.NewSourceProvider().GetSourceService().GetSource(ctx, org, r.PathValue("sourceId"))
	if err != nil {
		utils.HandleError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, source.ToResponse())
}

// PatchSource handles PATCH /sources/{sourceId}. Only the description can change.
func (h *SourceHandler) PatchSource(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if err := security.AuthnAndAuthz(r, "source:update"); err != nil {
		utils.HandleError(w, err)
		return
	}

	var patch model.SourcePatch
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&patch); err != nil {
		if errors.Is(err, io.EOF) {
			utils.HandleError(w, emptyUpdate())
			return
		}
		description := utils.HandleDecodeError(err, "source")
		if strings.HasPrefix(err.Error(), "json: unknown field ") {
			field := strings.Trim(strings.TrimPrefix(err.Error(), "json: unknown field "), `"`)
			description = "Only 'description' can be updated; '" + field + "' is not allowed."
		}
		utils.HandleError(w, badRequest(errors2.UpdateSourceBadRequest, description))
		return
	}
	if patch.Description == nil {
		utils.HandleError(w, emptyUpdate())
		return
	}

	org, err := orgService.ResolveOrganization(ctx, utils.ExtractOrgHandleFromPath(r))
	if err != nil {
		utils.HandleError(w, err)
		return
	}
	source, err := provider.NewSourceProvider().GetSourceService().UpdateSourceDescription(ctx, org,
		r.PathValue("sourceId"), *patch.Description)
	if err != nil {
		utils.HandleError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, source.ToResponse())
}

// DeleteSource handles DELETE /sources/{sourceId}. Deleting a source that does not exist also gives a 204.
func (h *SourceHandler) DeleteSource(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if err := security.AuthnAndAuthz(r, "source:delete"); err != nil {
		utils.HandleError(w, err)
		return
	}
	org, err := orgService.ResolveOrganization(ctx, utils.ExtractOrgHandleFromPath(r))
	if err != nil {
		utils.HandleError(w, err)
		return
	}
	if err := provider.NewSourceProvider().GetSourceService().DeleteSource(ctx, org,
		r.PathValue("sourceId")); err != nil {
		utils.HandleError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// sourceLocation returns the URL of a source under the path of the organization of the request, as
// BuildProfileLocation does for a profile. The tenant dispatcher strips /t/{org_handle} from r.URL.Path,
// so the path of the request cannot be used.
func sourceLocation(ctx context.Context, orgHandle, sourceID string) string {
	basePath, ok := ctx.Value(constants.OrgBasePathContextKey).(string)
	if !ok || basePath == "" {
		basePath = "/t/" + orgHandle
	}
	serverURL := strings.TrimSuffix(config.GetCDSRuntime().Config.ServerURL, "/")
	return serverURL + basePath + constants.APIBasePath + "/v1/sources/" + sourceID
}

func writeJSON(w http.ResponseWriter, status int, body interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func badRequest(code errors2.ErrorMessage, description string) error {
	return errors2.NewClientError(errors2.ErrorMessage{
		Code:        code.Code,
		Message:     code.Message,
		Description: description,
	}, http.StatusBadRequest)
}

func emptyUpdate() error {
	return badRequest(errors2.SourceEmptyUpdate, "The request body must contain 'description'.")
}
