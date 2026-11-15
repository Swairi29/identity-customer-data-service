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

// Package model holds the data shapes of the transformation PoC.
package model

// MappingRule copies one field of the source payload to one CDS attribute.
// Source is a dot path in the source payload (contact.email_address);
// Target is a dot path in the CDS payload (traits.primary_email).
type MappingRule struct {
	Source string `json:"source"`
	Target string `json:"target"`
}

// TransformationRequest is the body of the create and update calls.
type TransformationRequest struct {
	Name     string        `json:"name"`
	Mappings []MappingRule `json:"mappings"`
}

// Transformation is one organization's mapping and the MI endpoint that runs it.
type Transformation struct {
	Id        string        `json:"id"`
	OrgId     string        `json:"org_id"`
	Name      string        `json:"name"`
	Mappings  []MappingRule `json:"mappings"`
	Template  string        `json:"template"`
	Version   string        `json:"version"`
	MIUrl     string        `json:"mi_url"`
	Status    string        `json:"status"`
	Error     string        `json:"error,omitempty"`
	CreatedAt string        `json:"created_at"`
	UpdatedAt string        `json:"updated_at"`
}

// Status values of a transformation.
const (
	StatusPending = "PENDING"
	StatusActive  = "ACTIVE"
	StatusFailed  = "FAILED"
)
