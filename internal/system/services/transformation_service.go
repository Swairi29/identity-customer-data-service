/*
 * Copyright (c) 2026, WSO2 LLC. (http://www.wso2.com).
 *
 * WSO2 LLC. licenses this file to you under the Apache License,
 * Version 2.0 (the "License"); you may not use this file except
 * in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing,
 * software distributed under the License is distributed on an
 * "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY
 * KIND, either express or implied.  See the License for the
 * specific language governing permissions and limitations
 * under the License.
 */

package services

import (
	"net/http"

	"github.com/wso2/identity-customer-data-service/internal/system/constants"
	transformationHandler "github.com/wso2/identity-customer-data-service/internal/transformation/handler"
)

type TransformationService struct {
	handler *transformationHandler.TransformationHandler
	mux     *http.ServeMux
}

// NewTransformationService registers the transformation PoC routes on the shared tenant mux.
func NewTransformationService(mux *http.ServeMux) *TransformationService {
	s := &TransformationService{
		handler: transformationHandler.NewTransformationHandler(),
		mux:     mux,
	}

	const base = constants.ApiBasePath + "/v1"
	s.mux.HandleFunc("POST "+base+"/transformations", s.handler.CreateTransformation)
	s.mux.HandleFunc("GET "+base+"/transformations", s.handler.ListTransformations)
	s.mux.HandleFunc("GET "+base+"/transformations/{id}", s.handler.GetTransformation)
	s.mux.HandleFunc("PUT "+base+"/transformations/{id}", s.handler.UpdateTransformation)
	s.mux.HandleFunc("POST "+base+"/transformations/{id}/transform", s.handler.Transform)

	return s
}
