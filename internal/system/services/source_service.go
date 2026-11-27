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

package services

import (
	"net/http"
	"strings"

	"github.com/wso2/identity-customer-data-service/internal/source/handler"
	"github.com/wso2/identity-customer-data-service/internal/system/audit"
	"github.com/wso2/identity-customer-data-service/internal/system/constants"
)

// SourceService registers the source endpoints.
type SourceService struct {
	handler *handler.SourceHandler
	mux     *http.ServeMux
}

// NewSourceService registers the source routes on mux and returns the service.
func NewSourceService(mux *http.ServeMux) *SourceService {
	s := &SourceService{
		handler: handler.NewSourceHandler(),
		mux:     mux,
	}

	const base = constants.APIBasePath + "/v1"
	s.mux.HandleFunc("GET "+base+"/sources", s.handler.GetAllSources)
	s.mux.HandleFunc("POST "+base+"/sources", audit.Wrap(audit.SourceCreate, s.handler.AddSource))
	s.mux.HandleFunc("GET "+base+"/sources/{sourceId}", s.handler.GetSource)
	s.mux.HandleFunc("PATCH "+base+"/sources/{sourceId}", audit.Wrap(audit.SourceUpdate, s.handler.PatchSource))
	s.mux.HandleFunc("DELETE "+base+"/sources/{sourceId}", audit.Wrap(audit.SourceDelete, s.handler.DeleteSource))

	return s
}

// Route handles tenant-aware routing for sources.
func (s *SourceService) Route(w http.ResponseWriter, r *http.Request) {
	// Normalize trailing slashes for consistent matching
	if trimmed := strings.TrimSuffix(r.URL.Path, "/"); trimmed != "" {
		r.URL.Path = trimmed
	}
	s.mux.ServeHTTP(w, r)
}
