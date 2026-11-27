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
	"net/http"

	orgService "github.com/wso2/identity-customer-data-service/internal/organization/service"
	"github.com/wso2/identity-customer-data-service/internal/profile/provider"
	profileService "github.com/wso2/identity-customer-data-service/internal/profile/service"
	"github.com/wso2/identity-customer-data-service/internal/system/constants"
	errors2 "github.com/wso2/identity-customer-data-service/internal/system/errors"
	"github.com/wso2/identity-customer-data-service/internal/system/security"
	"github.com/wso2/identity-customer-data-service/internal/system/utils"
)

// GetProfileSources handles GET /profiles/{profileId}/sources: the current state of each attribute of a
// profile, with the source of its latest change. As for GET /profiles/{profileId}, a caller that is not a
// system application only sees the attributes it may read by consent.
func (ph *ProfileHandler) GetProfileSources(w http.ResponseWriter, r *http.Request) {
	request, ok := historyRequest(w, r)
	if !ok {
		return
	}
	service := provider.NewProfilesProvider().GetProfilesService()
	sources, err := service.GetAttributeSources(request.ctx, request.profileID, request.orgID)
	if err == nil && !request.isSystemApp {
		sources, err = profileService.FilterAttributeSourcesByConsent(request.ctx, sources, request.profileID,
			request.orgID, request.consentIDs)
	}
	if err != nil {
		utils.HandleError(w, err)
		return
	}
	utils.RespondJSON(w, http.StatusOK, sources, constants.ProfileResource)
}

// GetProfileSourceHistory handles GET /profiles/{profileId}/sources/history: every change to the attributes
// of a profile, newest first. ?attribute= limits it to one attribute. Consent applies as for
// GetProfileSources.
func (ph *ProfileHandler) GetProfileSourceHistory(w http.ResponseWriter, r *http.Request) {
	request, ok := historyRequest(w, r)
	if !ok {
		return
	}
	service := provider.NewProfilesProvider().GetProfilesService()
	history, err := service.GetAttributeHistory(request.ctx, request.profileID, request.orgID,
		r.URL.Query().Get("attribute"))
	if err == nil && !request.isSystemApp {
		history, err = profileService.FilterAttributeHistoryByConsent(request.ctx, history, request.profileID,
			request.orgID, request.consentIDs)
	}
	if err != nil {
		utils.HandleError(w, err)
		return
	}
	utils.RespondJSON(w, http.StatusOK, history, constants.ProfileResource)
}

// lineageRequest is what both attribute source endpoints read from a request before their own work.
type lineageRequest struct {
	ctx         context.Context
	orgID       string
	profileID   string
	isSystemApp bool
	consentIDs  []string
}

// historyRequest checks the token and the organization as GET /profiles/{profileId} does, and reads the
// caller and the requested consent categories. On a failure it writes the error and returns false.
func historyRequest(w http.ResponseWriter, r *http.Request) (lineageRequest, bool) {
	ctx := r.Context()
	if err := security.AuthnAndAuthz(r, "profile:view"); err != nil {
		utils.HandleError(w, err)
		return lineageRequest{}, false
	}
	org, err := orgService.ResolveOrganization(ctx, utils.ExtractOrgHandleFromPath(r))
	if err != nil {
		utils.HandleError(w, err)
		return lineageRequest{}, false
	}
	if !isCDSEnabled(ctx, org.ID) {
		utils.HandleError(w, errors2.NewClientError(errors2.CDSNotEnabled, http.StatusBadRequest))
		return lineageRequest{}, false
	}
	_, isSystemApp, err := resolveCaller(ctx, r, org.ID)
	if err != nil {
		utils.HandleError(w, err)
		return lineageRequest{}, false
	}
	return lineageRequest{
		ctx:         ctx,
		orgID:       org.ID,
		profileID:   r.PathValue("profileId"),
		isSystemApp: isSystemApp,
		consentIDs:  parseCommaSeparatedOrRepeated(r.URL.Query()["consentCategoryId"]),
	}, true
}
