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

package model

import "time"

// The actions of a change to an attribute, as stored in the action column.
const (
	// ActionSet is an attribute that was added or got a new value.
	ActionSet = "SET"
	// ActionRemoved is an attribute that was taken away.
	ActionRemoved = "REMOVED"
	// ActionMerged is an attribute whose value a merge of profiles kept, naming the source of that value.
	ActionMerged = "MERGED"
)

// AttributeChange is one change to one attribute of a profile: the attribute history is a list of them,
// appended and never updated. SourceID and SourceName are nil for a change made without ?source=;
// SourceName is the name the source had then, so the history still reads well after it is deleted.
type AttributeChange struct {
	ChangeID   string    `json:"-"`
	ProfileID  string    `json:"-"`
	Attribute  string    `json:"attribute"`
	Action     string    `json:"action"`
	SourceID   *string   `json:"source_id"`
	SourceName *string   `json:"source_name"`
	ChangedAt  time.Time `json:"changed_at"`
}

// AttributeSource is the current state of one attribute of a profile, worked out from its history: the
// source of the latest change (nil when that change had no source), when the attribute got the value it
// has (created_at) and when it last changed (updated_at).
type AttributeSource struct {
	Attribute  string    `json:"attribute"`
	SourceID   *string   `json:"source_id"`
	SourceName *string   `json:"source_name"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}
