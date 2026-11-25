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

// Package service holds the logic of the transformation PoC: it turns mapping
// rules into an MI template, deploys it to MI through its Management API (mi_deployer.go), and forwards
// payloads to the deployed MI endpoint.
package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/wso2/identity-customer-data-service/internal/system/log"
	"github.com/wso2/identity-customer-data-service/internal/transformation/model"
	"github.com/wso2/identity-customer-data-service/internal/transformation/store"
)

// Errors the handler turns into HTTP status codes.
var (
	ErrBadRequest = errors.New("bad request")
	ErrNotFound   = errors.New("transformation not found")
	ErrConflict   = errors.New("transformation already exists")
	ErrNotActive  = errors.New("transformation is not active")
)

var (
	namePattern = regexp.MustCompile(`^[A-Za-z0-9-]{1,64}$`)
	pathPattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*(\.[A-Za-z_][A-Za-z0-9_]*)*$`)
	nonAlnum    = regexp.MustCompile(`[^A-Za-z0-9-]`)
	miClient    = &http.Client{Timeout: 30 * time.Second}
)

// Create stores a new transformation for the organization and deploys it to MI.
func Create(ctx context.Context, orgId string, req model.TransformationRequest) (*model.Transformation, error) {

	if !namePattern.MatchString(req.Name) {
		return nil, fmt.Errorf("%w: name must be 1-64 letters, digits or hyphens", ErrBadRequest)
	}
	template, err := BuildTemplate(req.Mappings)
	if err != nil {
		return nil, err
	}
	existing, err := store.GetByName(ctx, orgId, req.Name)
	if err != nil {
		return nil, err
	}
	if existing != nil {
		return nil, fmt.Errorf("%w: '%s' (id %s); use PUT to change it", ErrConflict, req.Name, existing.Id)
	}

	now := time.Now().UTC().Format(time.RFC3339)
	t := model.Transformation{
		Id:        uuid.NewString(),
		OrgId:     orgId,
		Name:      req.Name,
		Mappings:  req.Mappings,
		Template:  template,
		Version:   "1.0.0",
		Status:    model.StatusPending,
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := store.Insert(ctx, t); err != nil {
		return nil, err
	}

	deploy(ctx, &t, "")
	if err := store.Update(ctx, t); err != nil {
		return nil, err
	}
	return &t, nil
}

// Update replaces the mapping of a transformation and deploys it as a new version.
func Update(ctx context.Context, orgId, id string, req model.TransformationRequest) (*model.Transformation, error) {

	t, err := store.Get(ctx, orgId, id)
	if err != nil {
		return nil, err
	}
	if t == nil {
		return nil, ErrNotFound
	}
	template, err := BuildTemplate(req.Mappings)
	if err != nil {
		return nil, err
	}

	oldVersion := t.Version
	t.Mappings = req.Mappings
	t.Template = template
	t.Version = nextVersion(oldVersion)
	t.UpdatedAt = time.Now().UTC().Format(time.RFC3339)

	deploy(ctx, t, oldVersion)
	if err := store.Update(ctx, *t); err != nil {
		return nil, err
	}
	return t, nil
}

// Get returns one transformation of the organization.
func Get(ctx context.Context, orgId, id string) (*model.Transformation, error) {

	t, err := store.Get(ctx, orgId, id)
	if err != nil {
		return nil, err
	}
	if t == nil {
		return nil, ErrNotFound
	}
	return t, nil
}

// List returns every transformation of the organization.
func List(ctx context.Context, orgId string) ([]model.Transformation, error) {
	return store.List(ctx, orgId)
}

// Transform sends the payload to the transformation's MI endpoint and returns
// MI's status code and body.
func Transform(ctx context.Context, orgId, id string, payload []byte) (int, []byte, error) {

	t, err := Get(ctx, orgId, id)
	if err != nil {
		return 0, nil, err
	}
	if t.Status != model.StatusActive || t.MIUrl == "" {
		return 0, nil, fmt.Errorf("%w: status is %s", ErrNotActive, t.Status)
	}
	return callMI(ctx, t.MIUrl, payload)
}

// ApplyForProfile is called by profile creation. name is the transformation to
// apply: from ?transformation=<name>, or the one linked to ?source=<id> (PoC V2).
// It returns the payload converted by MI (applied = true). With an empty name
// it returns the payload unchanged (applied = false), so callers that already
// send the CDS format keep working exactly as before.
func ApplyForProfile(ctx context.Context, orgId, name string, payload []byte) ([]byte, bool, error) {

	if name == "" {
		return payload, false, nil
	}
	t, err := store.GetByName(ctx, orgId, name)
	if err != nil {
		return nil, false, err
	}
	if t == nil {
		return nil, false, fmt.Errorf("%w: no transformation named '%s'", ErrNotFound, name)
	}
	if t.Status != model.StatusActive || t.MIUrl == "" {
		return nil, false, fmt.Errorf("%w: '%s' status is %s", ErrNotActive, t.Name, t.Status)
	}

	status, body, err := callMI(ctx, t.MIUrl, payload)
	if err != nil {
		return nil, false, err
	}
	if status < 200 || status > 299 {
		return nil, false, fmt.Errorf("MI returned HTTP %d: %s", status, tail(string(body), 300))
	}
	return body, true, nil
}

// ApplyReverse is called by profile retrieval. It converts a profile in the CDS
// format back to the source system's own format, using the reverse endpoint
// deployed with the transformation named name.
func ApplyReverse(ctx context.Context, orgId, name string, profile []byte) ([]byte, error) {

	t, err := store.GetByName(ctx, orgId, name)
	if err != nil {
		return nil, err
	}
	if t == nil {
		return nil, fmt.Errorf("%w: no transformation named '%s'", ErrNotFound, name)
	}
	if t.Status != model.StatusActive || t.MIUrl == "" {
		return nil, fmt.Errorf("%w: '%s' status is %s", ErrNotActive, t.Name, t.Status)
	}
	status, body, err := callMI(ctx, t.MIUrl+reverseSuffix, profile)
	if err != nil {
		return nil, err
	}
	if status < 200 || status > 299 {
		return nil, fmt.Errorf("MI returned HTTP %d: %s (transformations created before the reverse "+
			"mapping existed need a PUT to deploy it)", status, tail(string(body), 300))
	}
	return body, nil
}

// callMI posts a payload to an MI endpoint and returns its status code and body.
func callMI(ctx context.Context, url string, payload []byte) (int, []byte, error) {

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := miClient.Do(req)
	if err != nil {
		return 0, nil, fmt.Errorf("calling MI at %s: %w", url, err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, nil, err
	}
	return resp.StatusCode, body, nil
}

// BuildTemplate turns mapping rules into the MI PayloadFactory template. Each
// value is copied as the source sent it: only attribute names change.
//
// Rule {source: "fullName", target: "traits.full_name"} becomes
// {"traits":{"full_name":"${payload.fullName}"}}.
func BuildTemplate(rules []model.MappingRule) (string, error) {

	if len(rules) == 0 {
		return "", fmt.Errorf("%w: at least one mapping is required", ErrBadRequest)
	}
	root := map[string]interface{}{}
	for _, rule := range rules {
		if !pathPattern.MatchString(rule.Source) || !pathPattern.MatchString(rule.Target) {
			return "", fmt.Errorf("%w: invalid mapping %q -> %q; use dot paths of letters, digits and _",
				ErrBadRequest, rule.Source, rule.Target)
		}
		parts := strings.Split(rule.Target, ".")
		node := root
		for _, part := range parts[:len(parts)-1] {
			child, exists := node[part]
			if !exists {
				next := map[string]interface{}{}
				node[part] = next
				node = next
				continue
			}
			next, isObject := child.(map[string]interface{})
			if !isObject {
				return "", fmt.Errorf("%w: target %q conflicts with another target", ErrBadRequest, rule.Target)
			}
			node = next
		}
		leaf := parts[len(parts)-1]
		if _, exists := node[leaf]; exists {
			return "", fmt.Errorf("%w: target %q is mapped twice", ErrBadRequest, rule.Target)
		}
		node[leaf] = "${payload." + rule.Source + "}"
	}

	var buf bytes.Buffer
	encoder := json.NewEncoder(&buf)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(root); err != nil {
		return "", err
	}
	return strings.TrimSpace(buf.String()), nil
}

// reverseSuffix names the reverse MI endpoint: /transform/acme/crm-out.
const reverseSuffix = "-out"

// deploy deploys both directions of a transformation to MI: the inbound mapping
// (source format -> CDS) and its reverse (CDS -> source format), which is built
// automatically by swapping each rule's source and target. The outcome is
// recorded on t (status, URL, error); it never fails the request.
func deploy(ctx context.Context, t *model.Transformation, oldVersion string) {

	url, err := runDeploy(ctx, t.OrgId, t.Name, t.Version, t.Template, oldVersion)
	if err != nil {
		markFailed(t, err.Error())
		return
	}
	reverseTemplate, err := BuildTemplate(reverseRules(t.Mappings))
	if err != nil {
		markFailed(t, "building the reverse template: "+err.Error())
		return
	}
	if _, err := runDeploy(ctx, t.OrgId, t.Name+reverseSuffix, t.Version, reverseTemplate, oldVersion); err != nil {
		markFailed(t, "deploying the reverse mapping: "+err.Error())
		return
	}
	t.MIUrl = url
	t.Status = model.StatusActive
	t.Error = ""
}

// reverseRules swaps source and target, so a rename-only mapping runs backwards:
// {fullName -> traits.full_name} becomes {traits.full_name -> fullName}.
func reverseRules(rules []model.MappingRule) []model.MappingRule {
	reversed := make([]model.MappingRule, 0, len(rules))
	for _, rule := range rules {
		reversed = append(reversed, model.MappingRule{Source: rule.Target, Target: rule.Source})
	}
	return reversed
}

// runDeploy deploys one MI endpoint and returns the URL MI reports. Part M:
// it calls MI's Management API from Go (mi_deployer.go) instead of running
// ~/transformer-poc/deploy.sh, so CDS needs nothing outside the repository.
func runDeploy(ctx context.Context, orgId, name, version, template, oldVersion string) (string, error) {

	logger := log.GetLogger()
	deployCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()

	url, err := deployToMI(deployCtx, miConfigFromEnv(), miOrg(orgId), name, version, template, oldVersion)
	if err != nil {
		logger.Error(fmt.Sprintf("Deploying '%s' version %s to MI failed: %s", name, version, err.Error()))
		return "", errors.New(tail(err.Error(), 500))
	}
	logger.Info(fmt.Sprintf("Deployed '%s' version %s to MI: %s", name, version, url))
	return url, nil
}

func markFailed(t *model.Transformation, reason string) {
	t.Status = model.StatusFailed
	t.Error = reason
}

// miOrg makes an organization handle safe for MI names and paths (carbon.super -> carbon-super).
func miOrg(orgId string) string {
	return nonAlnum.ReplaceAllString(orgId, "-")
}

// nextVersion bumps the last part of a version: 1.0.3 -> 1.0.4.
func nextVersion(version string) string {
	parts := strings.Split(version, ".")
	last, err := strconv.Atoi(parts[len(parts)-1])
	if err != nil {
		return version + ".1"
	}
	parts[len(parts)-1] = strconv.Itoa(last + 1)
	return strings.Join(parts, ".")
}

func tail(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return "..." + s[len(s)-max:]
}
