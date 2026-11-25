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

// Package model holds the data shapes of the multi-source PoC (PoC V2):
// sources, destinations and the delivery log.
package model

// Destination types.
const (
	DestinationWebhook      = "WEBHOOK"
	DestinationMIIntegrator = "MI_INTEGRATOR"
)

// Destination auth types.
const (
	AuthNone   = "NONE"
	AuthAPIKey = "API_KEY"
	AuthBearer = "BEARER"
	AuthBasic  = "BASIC"
)

// Delivery statuses.
const (
	DeliverySuccess = "SUCCESS"
	DeliveryFailed  = "FAILED"
)

// MaskedSecret replaces a stored secret in every API response.
const MaskedSecret = "****"

// Auth says how CDS authenticates when it calls a destination.
type Auth struct {
	Type   string `json:"type"`
	Header string `json:"header,omitempty"`
	Value  string `json:"value,omitempty"`
}

// DestinationRequest is the body of POST /destinations.
type DestinationRequest struct {
	Name string `json:"name"`
	Type string `json:"type"`
	URL  string `json:"url"`
	Auth *Auth  `json:"auth,omitempty"`
}

// Destination is where CDS sends a profile after it is stored.
type Destination struct {
	Id        string `json:"id"`
	OrgId     string `json:"org_id"`
	Name      string `json:"name"`
	Type      string `json:"type"`
	URL       string `json:"url"`
	Auth      Auth   `json:"auth"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

// SourceRequest is the body of POST /sources and PUT /sources/{id}.
type SourceRequest struct {
	Name             string   `json:"name"`
	Description      string   `json:"description,omitempty"`
	TransformationId string   `json:"transformation_id,omitempty"`
	DestinationIds   []string `json:"destination_ids,omitempty"`
}

// Source is a customer app that sends data to CDS. Its Id is the source ID
// the app sends with ?source= on the profile API.
type Source struct {
	Id               string   `json:"id"`
	OrgId            string   `json:"org_id"`
	Name             string   `json:"name"`
	Description      string   `json:"description,omitempty"`
	TransformationId string   `json:"transformation_id,omitempty"`
	DestinationIds   []string `json:"destination_ids"`
	CreatedAt        string   `json:"created_at"`
	UpdatedAt        string   `json:"updated_at"`
}

// Delivery is one send of one profile to one destination.
type Delivery struct {
	Id              string `json:"id"`
	OrgId           string `json:"org_id"`
	SourceId        string `json:"source_id"`
	DestinationId   string `json:"destination_id"`
	DestinationName string `json:"destination_name"`
	DestinationType string `json:"destination_type"`
	ProfileId       string `json:"profile_id"`
	Status          string `json:"status"`
	HTTPStatus      int    `json:"http_status"`
	Error           string `json:"error"`
	CreatedAt       string `json:"created_at"`
}

// DeliveryEvent is the body every destination receives.
type DeliveryEvent struct {
	Event      string                 `json:"event"`
	Org        string                 `json:"org"`
	SourceId   string                 `json:"source_id"`
	SourceName string                 `json:"source_name"`
	ProfileId  string                 `json:"profile_id"`
	OccurredAt string                 `json:"occurred_at"`
	Traits     map[string]interface{} `json:"traits"`
}
