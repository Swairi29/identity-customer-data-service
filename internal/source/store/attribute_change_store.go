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

package store

import (
	"context"
	"fmt"
	"sort"

	"github.com/google/uuid"

	"github.com/wso2/identity-customer-data-service/internal/source/model"
	dbModel "github.com/wso2/identity-customer-data-service/internal/system/database/model"
	"github.com/wso2/identity-customer-data-service/internal/system/database/rows"
	"github.com/wso2/identity-customer-data-service/internal/system/database/scripts"
	errors2 "github.com/wso2/identity-customer-data-service/internal/system/errors"
)

// InsertAttributeChangesTx appends changes to the attribute history of profiles of the org inside tx, so a
// profile write and its history are committed or rolled back together. A change without a ChangeID gets a
// new one. The caller handles the error by rolling tx back.
func InsertAttributeChangesTx(ctx context.Context, tx *dbModel.Tx, orgID string,
	changes []model.AttributeChange) error {
	for _, change := range changes {
		changeID := change.ChangeID
		if changeID == "" {
			changeID = uuid.New().String()
		}
		if _, err := tx.ExecContext(ctx, scripts.InsertAttributeChange, changeID, change.ProfileID,
			change.Attribute, change.Action, change.SourceID, change.SourceName, orgID,
			change.ChangedAt.UTC()); err != nil {
			return fmt.Errorf("insert the %s change of %s of profile %s: %w", change.Action, change.Attribute,
				change.ProfileID, err)
		}
	}
	return nil
}

// InsertAttributeChanges appends changes in a transaction of their own, for a write that is not part of a
// profile write.
func InsertAttributeChanges(ctx context.Context, orgID string, changes []model.AttributeChange) error {
	if len(changes) == 0 {
		return nil
	}
	return inTx(ctx, errors2.RecordAttributeHistory, "recording the attribute history",
		func(tx *dbModel.Tx) error { return InsertAttributeChangesTx(ctx, tx, orgID, changes) })
}

// GetAttributeChanges returns the attribute history of a profile of the org, newest first. A non-empty
// attribute limits it to that attribute.
func GetAttributeChanges(ctx context.Context, profileID, orgID, attribute string) ([]model.AttributeChange,
	error) {
	action := fmt.Sprintf("fetching the attribute history of profile %s", profileID)
	query, args := scripts.GetAttributeChanges, []interface{}{profileID, orgID}
	if attribute != "" {
		query, args = scripts.GetAttributeChangesOfAttribute, append(args, attribute)
	}
	results, err := execute(ctx, errors2.FetchAttributeHistory, action, query, args...)
	if err != nil {
		return nil, err
	}
	changes := make([]model.AttributeChange, 0, len(results))
	for _, row := range results {
		changes = append(changes, model.AttributeChange{
			ChangeID:   rows.String(row, "change_id"),
			ProfileID:  rows.String(row, "profile_id"),
			Attribute:  rows.String(row, "attribute_path"),
			Action:     rows.String(row, "action"),
			SourceID:   nullableString(row, "source_id"),
			SourceName: nullableString(row, "source_name"),
			ChangedAt:  rows.Time(row, "changed_at").UTC(),
		})
	}
	SortNewestFirst(changes)
	return changes, nil
}

// SortNewestFirst orders changes by changed_at, newest first. Changes made at the same time (a profile
// created with several attributes) are ordered by attribute. The order is set here rather than by ORDER BY,
// so every datasource returns the same one.
func SortNewestFirst(changes []model.AttributeChange) {
	sort.SliceStable(changes, func(i, j int) bool {
		if !changes[i].ChangedAt.Equal(changes[j].ChangedAt) {
			return changes[i].ChangedAt.After(changes[j].ChangedAt)
		}
		return changes[i].Attribute < changes[j].Attribute
	})
}
