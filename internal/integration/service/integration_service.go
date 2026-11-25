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

// Package service holds the logic of PoC V2: registering sources and
// destinations, resolving a source to its transformation at runtime, and
// delivering stored profiles to the source's destinations.
package service

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/wso2/identity-customer-data-service/internal/integration/model"
	"github.com/wso2/identity-customer-data-service/internal/integration/store"
	"github.com/wso2/identity-customer-data-service/internal/system/log"
	transformationModel "github.com/wso2/identity-customer-data-service/internal/transformation/model"
	transformationService "github.com/wso2/identity-customer-data-service/internal/transformation/service"
)

// Errors the handlers turn into HTTP status codes.
var (
	ErrBadRequest = errors.New("bad request")
	ErrNotFound   = errors.New("not found")
	ErrConflict   = errors.New("already exists")

	// Runtime errors of the profile API (?source=).
	ErrUnknownSource          = errors.New("unknown source")
	ErrNoTransformation       = errors.New("source has no transformation")
	ErrTransformationInactive = errors.New("transformation is not active")
)

var (
	namePattern    = regexp.MustCompile(`^[A-Za-z0-9-]{1,64}$`)
	deliveryClient = &http.Client{Timeout: 5 * time.Second}
)

// ---------- destinations ----------

// CreateDestination validates and stores a destination.
func CreateDestination(ctx context.Context, orgId string, req model.DestinationRequest) (*model.Destination,
	error) {

	if !namePattern.MatchString(req.Name) {
		return nil, fmt.Errorf("%w: name must be 1-64 letters, digits or hyphens", ErrBadRequest)
	}
	if req.Type != model.DestinationWebhook && req.Type != model.DestinationMIIntegrator {
		return nil, fmt.Errorf("%w: type must be %s or %s", ErrBadRequest, model.DestinationWebhook,
			model.DestinationMIIntegrator)
	}
	if !strings.HasPrefix(req.URL, "http://") && !strings.HasPrefix(req.URL, "https://") {
		return nil, fmt.Errorf("%w: url must start with http:// or https://", ErrBadRequest)
	}
	auth := model.Auth{Type: model.AuthNone}
	if req.Auth != nil && req.Auth.Type != "" {
		auth = *req.Auth
	}
	switch auth.Type {
	case model.AuthNone:
		auth.Header, auth.Value = "", ""
	case model.AuthAPIKey:
		if auth.Header == "" || auth.Value == "" {
			return nil, fmt.Errorf("%w: API_KEY auth needs header and value", ErrBadRequest)
		}
	case model.AuthBearer, model.AuthBasic:
		if auth.Value == "" {
			return nil, fmt.Errorf("%w: %s auth needs value", ErrBadRequest, auth.Type)
		}
		auth.Header = ""
	default:
		return nil, fmt.Errorf("%w: auth.type must be NONE, API_KEY, BEARER or BASIC", ErrBadRequest)
	}

	existing, err := store.GetDestinationByName(ctx, orgId, req.Name)
	if err != nil {
		return nil, err
	}
	if existing != nil {
		return nil, fmt.Errorf("%w: destination '%s' (id %s)", ErrConflict, req.Name, existing.Id)
	}

	now := now()
	d := model.Destination{
		Id:        uuid.NewString(),
		OrgId:     orgId,
		Name:      req.Name,
		Type:      req.Type,
		URL:       req.URL,
		Auth:      auth,
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := store.InsertDestination(ctx, d); err != nil {
		return nil, err
	}
	return masked(d), nil
}

// GetDestination returns one destination with its secret masked.
func GetDestination(ctx context.Context, orgId, id string) (*model.Destination, error) {
	d, err := store.GetDestination(ctx, orgId, id)
	if err != nil {
		return nil, err
	}
	if d == nil {
		return nil, fmt.Errorf("%w: destination '%s'", ErrNotFound, id)
	}
	return masked(*d), nil
}

// ListDestinations returns every destination with secrets masked.
func ListDestinations(ctx context.Context, orgId string) ([]model.Destination, error) {
	list, err := store.ListDestinations(ctx, orgId)
	if err != nil {
		return nil, err
	}
	for i := range list {
		list[i] = *masked(list[i])
	}
	return list, nil
}

// DeleteDestination removes a destination and its links to sources.
func DeleteDestination(ctx context.Context, orgId, id string) error {
	if _, err := GetDestination(ctx, orgId, id); err != nil {
		return err
	}
	return store.DeleteDestination(ctx, orgId, id)
}

func masked(d model.Destination) *model.Destination {
	if d.Auth.Value != "" {
		d.Auth.Value = model.MaskedSecret
	}
	return &d
}

// ---------- sources ----------

// CreateSource validates and stores a source. CDS generates its ID.
func CreateSource(ctx context.Context, orgId string, req model.SourceRequest) (*model.Source, error) {

	if err := validateSource(ctx, orgId, req); err != nil {
		return nil, err
	}
	existing, err := store.GetSourceByName(ctx, orgId, req.Name)
	if err != nil {
		return nil, err
	}
	if existing != nil {
		return nil, fmt.Errorf("%w: source '%s' (id %s); use PUT to change it", ErrConflict, req.Name,
			existing.Id)
	}

	now := now()
	s := model.Source{
		Id:               uuid.NewString(),
		OrgId:            orgId,
		Name:             req.Name,
		Description:      req.Description,
		TransformationId: req.TransformationId,
		DestinationIds:   unique(req.DestinationIds),
		CreatedAt:        now,
		UpdatedAt:        now,
	}
	if err := store.InsertSource(ctx, s); err != nil {
		return nil, err
	}
	return &s, nil
}

// UpdateSource replaces a source's name, description, transformation and destinations.
func UpdateSource(ctx context.Context, orgId, id string, req model.SourceRequest) (*model.Source, error) {

	s, err := GetSource(ctx, orgId, id)
	if err != nil {
		return nil, err
	}
	if err := validateSource(ctx, orgId, req); err != nil {
		return nil, err
	}
	if req.Name != s.Name {
		other, err := store.GetSourceByName(ctx, orgId, req.Name)
		if err != nil {
			return nil, err
		}
		if other != nil {
			return nil, fmt.Errorf("%w: source '%s' (id %s)", ErrConflict, req.Name, other.Id)
		}
	}
	s.Name = req.Name
	s.Description = req.Description
	s.TransformationId = req.TransformationId
	s.DestinationIds = unique(req.DestinationIds)
	s.UpdatedAt = now()
	if err := store.UpdateSource(ctx, *s); err != nil {
		return nil, err
	}
	return s, nil
}

// GetSource returns one source of the organization.
func GetSource(ctx context.Context, orgId, id string) (*model.Source, error) {
	s, err := store.GetSource(ctx, orgId, id)
	if err != nil {
		return nil, err
	}
	if s == nil {
		return nil, fmt.Errorf("%w: source '%s'", ErrNotFound, id)
	}
	return s, nil
}

// ListSources returns every source of the organization.
func ListSources(ctx context.Context, orgId string) ([]model.Source, error) {
	return store.ListSources(ctx, orgId)
}

// DeleteSource removes a source and its destination links.
func DeleteSource(ctx context.Context, orgId, id string) error {
	if _, err := GetSource(ctx, orgId, id); err != nil {
		return err
	}
	return store.DeleteSource(ctx, orgId, id)
}

// validateSource checks the name and that every linked record belongs to the organization.
func validateSource(ctx context.Context, orgId string, req model.SourceRequest) error {

	if !namePattern.MatchString(req.Name) {
		return fmt.Errorf("%w: name must be 1-64 letters, digits or hyphens", ErrBadRequest)
	}
	if req.TransformationId != "" {
		if _, err := transformationService.Get(ctx, orgId, req.TransformationId); err != nil {
			if errors.Is(err, transformationService.ErrNotFound) {
				return fmt.Errorf("%w: transformation '%s' does not exist in organization '%s'", ErrBadRequest,
					req.TransformationId, orgId)
			}
			return err
		}
	}
	for _, destinationId := range req.DestinationIds {
		d, err := store.GetDestination(ctx, orgId, destinationId)
		if err != nil {
			return err
		}
		if d == nil {
			return fmt.Errorf("%w: destination '%s' does not exist in organization '%s'", ErrBadRequest,
				destinationId, orgId)
		}
	}
	return nil
}

// ---------- runtime ----------

// ResolveSource is called by the profile API for ?source=<id>. It returns the
// source and the transformation linked to it, or an error saying why the
// source can't be used: not registered, nothing linked, or not active.
func ResolveSource(ctx context.Context, orgId, sourceId string) (*model.Source,
	*transformationModel.Transformation, error) {

	s, err := store.GetSource(ctx, orgId, sourceId)
	if err != nil {
		return nil, nil, err
	}
	if s == nil {
		return nil, nil, fmt.Errorf("%w: source '%s' is not registered for organization '%s'", ErrUnknownSource,
			sourceId, orgId)
	}
	if s.TransformationId == "" {
		return nil, nil, fmt.Errorf("%w: source '%s' has no transformation linked", ErrNoTransformation, s.Name)
	}
	t, err := transformationService.Get(ctx, orgId, s.TransformationId)
	if err != nil {
		if errors.Is(err, transformationService.ErrNotFound) {
			return nil, nil, fmt.Errorf("%w: source '%s' links transformation '%s', which no longer exists",
				ErrNoTransformation, s.Name, s.TransformationId)
		}
		return nil, nil, err
	}
	if t.Status != transformationModel.StatusActive {
		return nil, nil, fmt.Errorf("%w: transformation '%s' is %s", ErrTransformationInactive, t.Name, t.Status)
	}
	return s, t, nil
}

// Deliver sends a stored profile to every destination linked to the source,
// one after another, and records each send. A failed send never fails the
// request: the profile is already stored. It returns how many sends succeeded
// and failed.
func Deliver(ctx context.Context, s *model.Source, profileId string, traits map[string]interface{}) (int, int) {

	logger := log.GetLogger()
	event := model.DeliveryEvent{
		Event:      "profile.created",
		Org:        s.OrgId,
		SourceId:   s.Id,
		SourceName: s.Name,
		ProfileId:  profileId,
		OccurredAt: time.Now().UTC().Format(time.RFC3339),
		Traits:     traits,
	}
	body, err := json.Marshal(event)
	if err != nil {
		logger.Error("Could not encode the delivery event: " + err.Error())
		return 0, len(s.DestinationIds)
	}

	sent, failed := 0, 0
	for _, destinationId := range s.DestinationIds {
		d, err := store.GetDestination(ctx, s.OrgId, destinationId)
		if err != nil || d == nil {
			failed++
			logger.Error(fmt.Sprintf("Delivery skipped: destination '%s' not found", destinationId))
			continue
		}
		httpStatus, sendErr := send(ctx, d, body)
		record := model.Delivery{
			Id:              uuid.NewString(),
			OrgId:           s.OrgId,
			SourceId:        s.Id,
			DestinationId:   d.Id,
			DestinationName: d.Name,
			DestinationType: d.Type,
			ProfileId:       profileId,
			Status:          model.DeliverySuccess,
			HTTPStatus:      httpStatus,
			CreatedAt:       now(),
		}
		if sendErr != nil {
			record.Status = model.DeliveryFailed
			record.Error = sendErr.Error()
			failed++
		} else {
			sent++
		}
		logger.Info(fmt.Sprintf("Delivery of profile %s to %s '%s': %s (HTTP %d) %s", profileId, d.Type, d.Name,
			record.Status, httpStatus, record.Error))
		if err := store.InsertDelivery(ctx, record); err != nil {
			logger.Error("Could not record the delivery: " + err.Error())
		}
	}
	return sent, failed
}

// ListDeliveries returns the delivery log, newest first.
func ListDeliveries(ctx context.Context, orgId, sourceId, profileId string) ([]model.Delivery, error) {
	return store.ListDeliveries(ctx, orgId, sourceId, profileId)
}

// send POSTs the event to one destination with its auth and returns the HTTP
// status. Any 2xx is a success.
func send(ctx context.Context, d *model.Destination, body []byte) (int, error) {

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, d.URL, bytes.NewReader(body))
	if err != nil {
		return 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	switch d.Auth.Type {
	case model.AuthAPIKey:
		req.Header.Set(d.Auth.Header, d.Auth.Value)
	case model.AuthBearer:
		req.Header.Set("Authorization", "Bearer "+d.Auth.Value)
	case model.AuthBasic:
		req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(d.Auth.Value)))
	}

	resp, err := deliveryClient.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	reply, _ := io.ReadAll(io.LimitReader(resp.Body, 300))
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return resp.StatusCode, fmt.Errorf("destination answered HTTP %d: %s", resp.StatusCode,
			strings.TrimSpace(string(reply)))
	}
	return resp.StatusCode, nil
}

// now is the timestamp format of every record; nanoseconds keep the order of
// records created in the same second.
func now() string {
	return time.Now().UTC().Format(time.RFC3339Nano)
}

func unique(ids []string) []string {
	seen := map[string]bool{}
	result := make([]string, 0, len(ids))
	for _, id := range ids {
		if id != "" && !seen[id] {
			seen[id] = true
			result = append(result, id)
		}
	}
	return result
}
