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

// Package handler serves the PoC V2 endpoints: /sources, /destinations and
// /deliveries.
//
// PoC only: like the transformation endpoints, these skip authentication on
// purpose. Every other CDS handler calls security.AuthnAndAuthz first; add that
// call before any real use.
package handler

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/wso2/identity-customer-data-service/internal/integration/model"
	"github.com/wso2/identity-customer-data-service/internal/integration/service"
	"github.com/wso2/identity-customer-data-service/internal/system/utils"
)

const maxBodyBytes = 1 << 20 // 1 MB

type IntegrationHandler struct{}

func NewIntegrationHandler() *IntegrationHandler {
	return &IntegrationHandler{}
}

// ---------- destinations ----------

// CreateDestination handles POST /destinations.
func (h *IntegrationHandler) CreateDestination(w http.ResponseWriter, r *http.Request) {
	var req model.DestinationRequest
	if !decode(w, r, &req) {
		return
	}
	d, err := service.CreateDestination(r.Context(), utils.ExtractOrgHandleFromPath(r), req)
	if err != nil {
		writeError(w, err)
		return
	}
	utils.RespondJSON(w, http.StatusCreated, d, "destination")
}

// ListDestinations handles GET /destinations.
func (h *IntegrationHandler) ListDestinations(w http.ResponseWriter, r *http.Request) {
	list, err := service.ListDestinations(r.Context(), utils.ExtractOrgHandleFromPath(r))
	if err != nil {
		writeError(w, err)
		return
	}
	utils.RespondJSON(w, http.StatusOK, list, "destinations")
}

// GetDestination handles GET /destinations/{id}.
func (h *IntegrationHandler) GetDestination(w http.ResponseWriter, r *http.Request) {
	d, err := service.GetDestination(r.Context(), utils.ExtractOrgHandleFromPath(r), r.PathValue("id"))
	if err != nil {
		writeError(w, err)
		return
	}
	utils.RespondJSON(w, http.StatusOK, d, "destination")
}

// DeleteDestination handles DELETE /destinations/{id}.
func (h *IntegrationHandler) DeleteDestination(w http.ResponseWriter, r *http.Request) {
	if err := service.DeleteDestination(r.Context(), utils.ExtractOrgHandleFromPath(r),
		r.PathValue("id")); err != nil {
		writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ---------- sources ----------

// CreateSource handles POST /sources.
func (h *IntegrationHandler) CreateSource(w http.ResponseWriter, r *http.Request) {
	var req model.SourceRequest
	if !decode(w, r, &req) {
		return
	}
	s, err := service.CreateSource(r.Context(), utils.ExtractOrgHandleFromPath(r), req)
	if err != nil {
		writeError(w, err)
		return
	}
	utils.RespondJSON(w, http.StatusCreated, s, "source")
}

// ListSources handles GET /sources.
func (h *IntegrationHandler) ListSources(w http.ResponseWriter, r *http.Request) {
	list, err := service.ListSources(r.Context(), utils.ExtractOrgHandleFromPath(r))
	if err != nil {
		writeError(w, err)
		return
	}
	utils.RespondJSON(w, http.StatusOK, list, "sources")
}

// GetSource handles GET /sources/{id}.
func (h *IntegrationHandler) GetSource(w http.ResponseWriter, r *http.Request) {
	s, err := service.GetSource(r.Context(), utils.ExtractOrgHandleFromPath(r), r.PathValue("id"))
	if err != nil {
		writeError(w, err)
		return
	}
	utils.RespondJSON(w, http.StatusOK, s, "source")
}

// UpdateSource handles PUT /sources/{id}.
func (h *IntegrationHandler) UpdateSource(w http.ResponseWriter, r *http.Request) {
	var req model.SourceRequest
	if !decode(w, r, &req) {
		return
	}
	s, err := service.UpdateSource(r.Context(), utils.ExtractOrgHandleFromPath(r), r.PathValue("id"), req)
	if err != nil {
		writeError(w, err)
		return
	}
	utils.RespondJSON(w, http.StatusOK, s, "source")
}

// DeleteSource handles DELETE /sources/{id}.
func (h *IntegrationHandler) DeleteSource(w http.ResponseWriter, r *http.Request) {
	if err := service.DeleteSource(r.Context(), utils.ExtractOrgHandleFromPath(r), r.PathValue("id")); err != nil {
		writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ---------- deliveries ----------

// ListDeliveries handles GET /deliveries?source_id=&profile_id=.
func (h *IntegrationHandler) ListDeliveries(w http.ResponseWriter, r *http.Request) {
	list, err := service.ListDeliveries(r.Context(), utils.ExtractOrgHandleFromPath(r),
		r.URL.Query().Get("source_id"), r.URL.Query().Get("profile_id"))
	if err != nil {
		writeError(w, err)
		return
	}
	utils.RespondJSON(w, http.StatusOK, list, "deliveries")
}

// ---------- helpers ----------

func decode(w http.ResponseWriter, r *http.Request, target interface{}) bool {
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBodyBytes))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return false
	}
	return true
}

func writeError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, service.ErrBadRequest):
		writeJSONError(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, service.ErrNotFound):
		writeJSONError(w, http.StatusNotFound, err.Error())
	case errors.Is(err, service.ErrConflict):
		writeJSONError(w, http.StatusConflict, err.Error())
	default:
		writeJSONError(w, http.StatusInternalServerError, err.Error())
	}
}

func writeJSONError(w http.ResponseWriter, status int, message string) {
	utils.RespondJSON(w, status, map[string]string{"error": message}, "error")
}
