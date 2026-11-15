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

// Package handler serves the transformation PoC endpoints.
//
// PoC only: these endpoints skip authentication on purpose. Every other CDS
// handler calls security.AuthnAndAuthz first; add that call before any real use.
package handler

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/wso2/identity-customer-data-service/internal/system/utils"
	"github.com/wso2/identity-customer-data-service/internal/transformation/model"
	"github.com/wso2/identity-customer-data-service/internal/transformation/service"
)

const maxPayloadBytes = 1 << 20 // 1 MB

type TransformationHandler struct{}

func NewTransformationHandler() *TransformationHandler {
	return &TransformationHandler{}
}

// CreateTransformation handles POST /transformations.
func (h *TransformationHandler) CreateTransformation(w http.ResponseWriter, r *http.Request) {

	req, ok := decodeRequest(w, r)
	if !ok {
		return
	}
	t, err := service.Create(r.Context(), utils.ExtractOrgHandleFromPath(r), req)
	if err != nil {
		writeError(w, err)
		return
	}
	utils.RespondJSON(w, statusFor(t, http.StatusCreated), t, "transformation")
}

// ListTransformations handles GET /transformations.
func (h *TransformationHandler) ListTransformations(w http.ResponseWriter, r *http.Request) {

	list, err := service.List(r.Context(), utils.ExtractOrgHandleFromPath(r))
	if err != nil {
		writeError(w, err)
		return
	}
	utils.RespondJSON(w, http.StatusOK, list, "transformations")
}

// GetTransformation handles GET /transformations/{id}.
func (h *TransformationHandler) GetTransformation(w http.ResponseWriter, r *http.Request) {

	t, err := service.Get(r.Context(), utils.ExtractOrgHandleFromPath(r), r.PathValue("id"))
	if err != nil {
		writeError(w, err)
		return
	}
	utils.RespondJSON(w, http.StatusOK, t, "transformation")
}

// UpdateTransformation handles PUT /transformations/{id}: new mapping, new version.
func (h *TransformationHandler) UpdateTransformation(w http.ResponseWriter, r *http.Request) {

	req, ok := decodeRequest(w, r)
	if !ok {
		return
	}
	t, err := service.Update(r.Context(), utils.ExtractOrgHandleFromPath(r), r.PathValue("id"), req)
	if err != nil {
		writeError(w, err)
		return
	}
	utils.RespondJSON(w, statusFor(t, http.StatusOK), t, "transformation")
}

// Transform handles POST /transformations/{id}/transform: the body is the
// source payload (P1); the response is MI's transformed payload (TP1).
func (h *TransformationHandler) Transform(w http.ResponseWriter, r *http.Request) {

	payload, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxPayloadBytes))
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "could not read the request body: "+err.Error())
		return
	}
	status, body, err := service.Transform(r.Context(), utils.ExtractOrgHandleFromPath(r), r.PathValue("id"),
		payload)
	if err != nil {
		writeError(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(body)
}

func decodeRequest(w http.ResponseWriter, r *http.Request) (model.TransformationRequest, bool) {

	var req model.TransformationRequest
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxPayloadBytes))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return req, false
	}
	return req, true
}

// statusFor reports a failed MI deployment as 502 so the caller notices it.
func statusFor(t *model.Transformation, success int) int {
	if t.Status == model.StatusFailed {
		return http.StatusBadGateway
	}
	return success
}

func writeError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, service.ErrBadRequest):
		writeJSONError(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, service.ErrNotFound):
		writeJSONError(w, http.StatusNotFound, err.Error())
	case errors.Is(err, service.ErrConflict):
		writeJSONError(w, http.StatusConflict, err.Error())
	case errors.Is(err, service.ErrNotActive):
		writeJSONError(w, http.StatusConflict, err.Error())
	default:
		writeJSONError(w, http.StatusInternalServerError, err.Error())
	}
}

func writeJSONError(w http.ResponseWriter, status int, message string) {
	utils.RespondJSON(w, status, map[string]string{"error": message}, "error")
}
