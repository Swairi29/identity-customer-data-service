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

	integrationHandler "github.com/wso2/identity-customer-data-service/internal/integration/handler"
	"github.com/wso2/identity-customer-data-service/internal/system/constants"
)

type IntegrationService struct {
	handler *integrationHandler.IntegrationHandler
	mux     *http.ServeMux
}

// NewIntegrationService registers the PoC V2 routes (sources, destinations,
// deliveries) on the shared tenant mux.
func NewIntegrationService(mux *http.ServeMux) *IntegrationService {
	s := &IntegrationService{
		handler: integrationHandler.NewIntegrationHandler(),
		mux:     mux,
	}

	const base = constants.ApiBasePath + "/v1"
	s.mux.HandleFunc("POST "+base+"/destinations", s.handler.CreateDestination)
	s.mux.HandleFunc("GET "+base+"/destinations", s.handler.ListDestinations)
	s.mux.HandleFunc("GET "+base+"/destinations/{id}", s.handler.GetDestination)
	s.mux.HandleFunc("DELETE "+base+"/destinations/{id}", s.handler.DeleteDestination)

	s.mux.HandleFunc("POST "+base+"/sources", s.handler.CreateSource)
	s.mux.HandleFunc("GET "+base+"/sources", s.handler.ListSources)
	s.mux.HandleFunc("GET "+base+"/sources/{id}", s.handler.GetSource)
	s.mux.HandleFunc("PUT "+base+"/sources/{id}", s.handler.UpdateSource)
	s.mux.HandleFunc("DELETE "+base+"/sources/{id}", s.handler.DeleteSource)

	s.mux.HandleFunc("GET "+base+"/deliveries", s.handler.ListDeliveries)

	return s
}
