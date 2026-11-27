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

// Package store persists the sources of an organization. Every statement filters by org_id, so one
// organization can never read or change the sources of another.
package store

import (
	"context"
	"fmt"
	"sort"

	"github.com/wso2/identity-customer-data-service/internal/source/model"
	dbModel "github.com/wso2/identity-customer-data-service/internal/system/database/model"
	"github.com/wso2/identity-customer-data-service/internal/system/database/provider"
	"github.com/wso2/identity-customer-data-service/internal/system/database/rows"
	"github.com/wso2/identity-customer-data-service/internal/system/database/scripts"
	errors2 "github.com/wso2/identity-customer-data-service/internal/system/errors"
	"github.com/wso2/identity-customer-data-service/internal/system/log"
)

// AddSource inserts a new source and its destination links, in one transaction.
func AddSource(ctx context.Context, source model.Source) error {
	err := inTx(ctx, errors2.AddSource, fmt.Sprintf("inserting source %s", source.SourceID),
		func(tx *dbModel.Tx) error {
			if _, err := tx.ExecContext(ctx, scripts.InsertSource, source.SourceID, source.OrgID, source.Name,
				source.Description, source.IconURL, source.TransformationID, source.CreatedAt,
				source.UpdatedAt); err != nil {
				return err
			}
			return insertDestinations(ctx, tx, source)
		})
	if err == nil {
		log.GetLogger().Info("Successfully inserted source: " + source.SourceID)
	}
	return err
}

// GetSources returns every source of the org, newest first.
func GetSources(ctx context.Context, orgID string) ([]model.Source, error) {
	results, err := execute(ctx, errors2.FetchSources, "fetching sources", scripts.GetSourcesByOrg, orgID)
	if err != nil {
		return nil, err
	}
	links, err := execute(ctx, errors2.FetchSources, "fetching source destinations",
		scripts.GetSourceDestinationsByOrg, orgID)
	if err != nil {
		return nil, err
	}
	destinations := map[string][]string{}
	for _, row := range links {
		sourceID := rows.String(row, "source_id")
		destinations[sourceID] = append(destinations[sourceID], rows.String(row, "destination_id"))
	}

	sources := make([]model.Source, 0, len(results))
	for _, row := range results {
		source := scanSource(row)
		source.DestinationIDs = sorted(destinations[source.SourceID])
		sources = append(sources, source)
	}
	return sources, nil
}

// GetSourceByID returns the source of the org with the given ID, or nil when there is none.
func GetSourceByID(ctx context.Context, sourceID, orgID string) (*model.Source, error) {
	return getOne(ctx, fmt.Sprintf("fetching source %s", sourceID), scripts.GetSourceByID, sourceID, orgID)
}

// GetSourceByName returns the source of the org with the given name, or nil when there is none.
func GetSourceByName(ctx context.Context, name, orgID string) (*model.Source, error) {
	return getOne(ctx, "fetching source by name", scripts.GetSourceByName, name, orgID)
}

// UpdateSource writes the description, transformation and destination links of a source and sets its
// updated_at, in one transaction. A nil transformation ID stores NULL. The destination links are replaced
// by source.DestinationIDs.
func UpdateSource(ctx context.Context, source model.Source) error {
	return inTx(ctx, errors2.UpdateSource, fmt.Sprintf("updating source %s", source.SourceID),
		func(tx *dbModel.Tx) error {
			if _, err := tx.ExecContext(ctx, scripts.UpdateSource, source.Description, source.TransformationID,
				source.UpdatedAt, source.SourceID, source.OrgID); err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, scripts.DeleteSourceDestinations, source.SourceID,
				source.OrgID); err != nil {
				return err
			}
			return insertDestinations(ctx, tx, source)
		})
}

// DeleteSource removes the source of the org with the given ID; ON DELETE CASCADE removes its destination
// links. A missing source is not an error.
func DeleteSource(ctx context.Context, sourceID, orgID string) error {
	_, err := execute(ctx, errors2.DeleteSource, fmt.Sprintf("deleting source %s", sourceID),
		scripts.DeleteSource, sourceID, orgID)
	if err == nil {
		log.GetLogger().Info("Successfully deleted source: " + sourceID)
	}
	return err
}

func getOne(ctx context.Context, action string, query dbModel.DBQuery, args ...interface{}) (*model.Source, error) {
	results, err := execute(ctx, errors2.FetchSources, action, query, args...)
	if err != nil {
		return nil, err
	}
	if len(results) == 0 {
		return nil, nil
	}
	source := scanSource(results[0])
	links, err := execute(ctx, errors2.FetchSources, action+" destinations", scripts.GetSourceDestinations,
		source.SourceID, source.OrgID)
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(links))
	for _, row := range links {
		ids = append(ids, rows.String(row, "destination_id"))
	}
	source.DestinationIDs = sorted(ids)
	return &source, nil
}

// insertDestinations links the source to each of its destinations.
func insertDestinations(ctx context.Context, tx *dbModel.Tx, source model.Source) error {
	for _, destinationID := range source.DestinationIDs {
		if _, err := tx.ExecContext(ctx, scripts.InsertSourceDestination, source.SourceID, destinationID,
			source.OrgID); err != nil {
			return err
		}
	}
	return nil
}

// inTx runs fn in a transaction, and rolls it back and returns a server error with the given code when
// any step fails.
func inTx(ctx context.Context, code errors2.ErrorMessage, action string, fn func(tx *dbModel.Tx) error) error {
	dbClient, err := provider.NewDBProvider().GetDBClient()
	if err != nil {
		return serverError(code, "Failed to get db client for "+action, err)
	}
	defer func() { _ = dbClient.Close() }()

	tx, err := dbClient.BeginTxContext(ctx)
	if err != nil {
		return serverError(code, "Failed to begin transaction for "+action, err)
	}
	if err := fn(tx); err != nil {
		if rollbackErr := tx.Rollback(); rollbackErr != nil {
			log.GetLogger().Debug("Failed to roll back "+action, log.Error(rollbackErr))
		}
		return serverError(code, "Failed to execute query for "+action, err)
	}
	if err := tx.Commit(); err != nil {
		return serverError(code, "Failed to commit "+action, err)
	}
	return nil
}

// sorted returns the IDs in ascending order, never nil. The order is set here rather than by ORDER BY,
// because each datasource's collation can sort the same IDs differently.
func sorted(ids []string) []string {
	out := append([]string{}, ids...)
	sort.Strings(out)
	return out
}

// execute runs one statement and turns a failure into a server error with the given code.
func execute(ctx context.Context, code errors2.ErrorMessage, action string, query dbModel.DBQuery,
	args ...interface{}) ([]map[string]interface{}, error) {
	logger := log.GetLogger()
	dbClient, err := provider.NewDBProvider().GetDBClient()
	if err != nil {
		return nil, serverError(code, "Failed to get db client for "+action, err)
	}
	defer func() { _ = dbClient.Close() }()

	results, err := dbClient.ExecuteQueryContext(ctx, query, args...)
	if err != nil {
		logger.Debug("Failed to execute query for "+action, log.Error(err))
		return nil, serverError(code, "Failed to execute query for "+action, err)
	}
	return results, nil
}

func serverError(code errors2.ErrorMessage, description string, err error) error {
	log.GetLogger().Debug(description, log.Error(err))
	return errors2.NewServerError(errors2.ErrorMessage{
		Code:        code.Code,
		Message:     code.Message,
		Description: description,
	}, err)
}

// scanSource reads one row without asserting column types, because each datasource returns its own types,
// and Oracle returns an empty description as NULL.
func scanSource(row map[string]interface{}) model.Source {
	return model.Source{
		SourceID:         rows.String(row, "source_id"),
		OrgID:            rows.String(row, "org_id"),
		Name:             rows.String(row, "name"),
		Description:      rows.String(row, "description"),
		IconURL:          rows.String(row, "icon_url"),
		TransformationID: nullableString(row, "transformation_id"),
		CreatedAt:        rows.Time(row, "created_at"),
		UpdatedAt:        rows.Time(row, "updated_at"),
	}
}

// nullableString returns the column as a string, or nil when it is NULL or empty.
func nullableString(row map[string]interface{}, column string) *string {
	value := rows.String(row, column)
	if value == "" {
		return nil
	}
	return &value
}
