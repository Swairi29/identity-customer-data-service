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
	"fmt"
	"net/http"
	"strings"

	profileStore "github.com/wso2/identity-customer-data-service/internal/profile/store"
	"github.com/wso2/identity-customer-data-service/internal/source/lineage"
	sourceModel "github.com/wso2/identity-customer-data-service/internal/source/model"
	sourceStore "github.com/wso2/identity-customer-data-service/internal/source/store"
	errors2 "github.com/wso2/identity-customer-data-service/internal/system/errors"
)

// GetAttributeHistory returns the raw attribute history of a profile, newest first; a non-empty attribute
// limits it to that attribute, which must be a tracked one such as traits.email. A profile merged into
// another reads the history of its master, which holds its data. An unknown profile gives a 404.
func (ps *ProfilesService) GetAttributeHistory(ctx context.Context, profileID, orgID, attribute string) (
	[]sourceModel.AttributeChange, error) {
	if attribute != "" && !isTrackedAttribute(attribute) {
		return nil, errors2.NewClientError(errors2.ErrorMessage{
			Code:    errors2.AttributeInvalid.Code,
			Message: errors2.AttributeInvalid.Message,
			Description: fmt.Sprintf("'%s' is not an attribute with a history: use %s.<name> or %s.<name>.",
				attribute, lineage.IdentityAttributes, lineage.Traits),
		}, http.StatusBadRequest)
	}

	owner, err := historyOwner(ctx, profileID, orgID)
	if err != nil {
		return nil, err
	}
	return sourceStore.GetAttributeChanges(ctx, owner, orgID, attribute)
}

// GetAttributeSources returns the current state of each attribute of a profile, sorted by attribute: the
// source of its latest change, when it got its value and when it last changed. Attributes that were removed
// are left out. A profile merged into another reads its master's, and an unknown profile gives a 404.
func (ps *ProfilesService) GetAttributeSources(ctx context.Context, profileID, orgID string) (
	[]sourceModel.AttributeSource, error) {
	owner, err := historyOwner(ctx, profileID, orgID)
	if err != nil {
		return nil, err
	}
	history, err := sourceStore.GetAttributeChanges(ctx, owner, orgID, "")
	if err != nil {
		return nil, err
	}
	return lineage.CurrentSources(history), nil
}

// historyOwner returns the ID of the profile that holds the history of a profile: the profile itself, or
// its master for a profile merged into another. An unknown profile gives a 404.
func historyOwner(ctx context.Context, profileID, orgID string) (string, error) {
	profile, err := profileStore.GetProfile(ctx, profileID, orgID)
	if err != nil {
		return "", err
	}
	if profile == nil {
		return "", profileNotFoundError()
	}
	if status := profile.ProfileStatus; status != nil && !status.IsReferenceProfile && status.ReferenceProfileID != "" {
		return status.ReferenceProfileID, nil
	}
	return profile.ProfileID, nil
}

// FilterAttributeHistoryByConsent keeps the history entries of the attributes the caller may read on the
// profile, by the same consent rule as FilterProfileByConsent.
func FilterAttributeHistoryByConsent(ctx context.Context, changes []sourceModel.AttributeChange,
	profileID, orgID string, consentIDs []string) ([]sourceModel.AttributeChange, error) {
	return filterByConsent(ctx, changes, func(c sourceModel.AttributeChange) string { return c.Attribute },
		profileID, orgID, consentIDs)
}

// FilterAttributeSourcesByConsent keeps the attributes the caller may read on the profile, by the same
// consent rule as FilterProfileByConsent.
func FilterAttributeSourcesByConsent(ctx context.Context, sources []sourceModel.AttributeSource,
	profileID, orgID string, consentIDs []string) ([]sourceModel.AttributeSource, error) {
	return filterByConsent(ctx, sources, func(s sourceModel.AttributeSource) string { return s.Attribute },
		profileID, orgID, consentIDs)
}

// filterByConsent keeps the items whose attribute the caller may read on the profile: the attributes
// FilterProfileByConsent would return.
func filterByConsent[T any](ctx context.Context, items []T, attributeOf func(T) string,
	profileID, orgID string, consentIDs []string) ([]T, error) {
	if len(items) == 0 {
		return items, nil
	}
	filter, err := newConsentFilter(ctx, orgID, consentIDs, []string{profileID})
	if err != nil {
		return nil, err
	}
	allowed := filter.allowed(profileID)
	kept := make([]T, 0, len(items))
	for _, item := range items {
		group, name, _ := strings.Cut(attributeOf(item), ".")
		if allowed[fmt.Sprintf("%s::%s", group, name)] {
			kept = append(kept, item)
		}
	}
	return kept, nil
}

// isTrackedAttribute reports whether the name is an attribute the history covers: identity_attributes.x or
// traits.x, where x is a top-level attribute.
func isTrackedAttribute(attribute string) bool {
	group, name, ok := strings.Cut(attribute, ".")
	if !ok || name == "" || strings.Contains(name, ".") {
		return false
	}
	return group == lineage.IdentityAttributes || group == lineage.Traits
}
