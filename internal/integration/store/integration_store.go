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

// Package store reads and writes the sources, destinations, source_destinations
// and deliveries tables.
package store

import (
	"context"
	"fmt"
	"strconv"

	"github.com/wso2/identity-customer-data-service/internal/integration/model"
	dbmodel "github.com/wso2/identity-customer-data-service/internal/system/database/model"
	"github.com/wso2/identity-customer-data-service/internal/system/database/provider"
)

// ---------- destinations ----------

const destinationColumns = `destination_id, org_id, name, type, url, auth_type, auth_header, auth_value,
	created_at, updated_at`

var insertDestination = dbmodel.DBQuery{ID: "CDS-INT-01",
	Query: `INSERT INTO destinations (` + destinationColumns + `)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)`}

var getDestination = dbmodel.DBQuery{ID: "CDS-INT-02",
	Query: `SELECT ` + destinationColumns + ` FROM destinations WHERE destination_id = $1 AND org_id = $2`}

var getDestinationByName = dbmodel.DBQuery{ID: "CDS-INT-03",
	Query: `SELECT ` + destinationColumns + ` FROM destinations WHERE org_id = $1 AND name = $2`}

var listDestinations = dbmodel.DBQuery{ID: "CDS-INT-04",
	Query: `SELECT ` + destinationColumns + ` FROM destinations WHERE org_id = $1 ORDER BY created_at`}

var deleteDestination = dbmodel.DBQuery{ID: "CDS-INT-05",
	Query: `DELETE FROM destinations WHERE destination_id = $1 AND org_id = $2`}

var deleteLinksOfDestination = dbmodel.DBQuery{ID: "CDS-INT-06",
	Query: `DELETE FROM source_destinations WHERE destination_id = $1`}

// InsertDestination stores a new destination.
func InsertDestination(ctx context.Context, d model.Destination) error {
	return exec(ctx, insertDestination, d.Id, d.OrgId, d.Name, d.Type, d.URL, d.Auth.Type, d.Auth.Header,
		d.Auth.Value, d.CreatedAt, d.UpdatedAt)
}

// GetDestination returns one destination of an organization, or nil.
func GetDestination(ctx context.Context, orgId, id string) (*model.Destination, error) {
	rows, err := query(ctx, getDestination, id, orgId)
	if err != nil || len(rows) == 0 {
		return nil, err
	}
	d := destinationFromRow(rows[0])
	return &d, nil
}

// GetDestinationByName returns the destination with this name, or nil.
func GetDestinationByName(ctx context.Context, orgId, name string) (*model.Destination, error) {
	rows, err := query(ctx, getDestinationByName, orgId, name)
	if err != nil || len(rows) == 0 {
		return nil, err
	}
	d := destinationFromRow(rows[0])
	return &d, nil
}

// ListDestinations returns every destination of an organization.
func ListDestinations(ctx context.Context, orgId string) ([]model.Destination, error) {
	rows, err := query(ctx, listDestinations, orgId)
	if err != nil {
		return nil, err
	}
	result := make([]model.Destination, 0, len(rows))
	for _, row := range rows {
		result = append(result, destinationFromRow(row))
	}
	return result, nil
}

// DeleteDestination removes a destination and every link to it.
func DeleteDestination(ctx context.Context, orgId, id string) error {
	if err := exec(ctx, deleteLinksOfDestination, id); err != nil {
		return err
	}
	return exec(ctx, deleteDestination, id, orgId)
}

func destinationFromRow(row map[string]interface{}) model.Destination {
	return model.Destination{
		Id:    text(row["destination_id"]),
		OrgId: text(row["org_id"]),
		Name:  text(row["name"]),
		Type:  text(row["type"]),
		URL:   text(row["url"]),
		Auth: model.Auth{
			Type:   text(row["auth_type"]),
			Header: text(row["auth_header"]),
			Value:  text(row["auth_value"]),
		},
		CreatedAt: text(row["created_at"]),
		UpdatedAt: text(row["updated_at"]),
	}
}

// ---------- sources ----------

const sourceColumns = `source_id, org_id, name, description, transformation_id, created_at, updated_at`

var insertSource = dbmodel.DBQuery{ID: "CDS-INT-11",
	Query: `INSERT INTO sources (` + sourceColumns + `) VALUES ($1, $2, $3, $4, $5, $6, $7)`}

var updateSource = dbmodel.DBQuery{ID: "CDS-INT-12",
	Query: `UPDATE sources SET name = $1, description = $2, transformation_id = $3, updated_at = $4
		WHERE source_id = $5 AND org_id = $6`}

var getSource = dbmodel.DBQuery{ID: "CDS-INT-13",
	Query: `SELECT ` + sourceColumns + ` FROM sources WHERE source_id = $1 AND org_id = $2`}

var getSourceByName = dbmodel.DBQuery{ID: "CDS-INT-14",
	Query: `SELECT ` + sourceColumns + ` FROM sources WHERE org_id = $1 AND name = $2`}

var listSources = dbmodel.DBQuery{ID: "CDS-INT-15",
	Query: `SELECT ` + sourceColumns + ` FROM sources WHERE org_id = $1 ORDER BY created_at`}

var deleteSource = dbmodel.DBQuery{ID: "CDS-INT-16",
	Query: `DELETE FROM sources WHERE source_id = $1 AND org_id = $2`}

var insertLink = dbmodel.DBQuery{ID: "CDS-INT-17",
	Query: `INSERT INTO source_destinations (source_id, destination_id) VALUES ($1, $2)`}

var deleteLinksOfSource = dbmodel.DBQuery{ID: "CDS-INT-18",
	Query: `DELETE FROM source_destinations WHERE source_id = $1`}

var listLinksOfSource = dbmodel.DBQuery{ID: "CDS-INT-19",
	Query: `SELECT destination_id FROM source_destinations WHERE source_id = $1 ORDER BY destination_id`}

// InsertSource stores a new source and its destination links.
func InsertSource(ctx context.Context, s model.Source) error {
	if err := exec(ctx, insertSource, s.Id, s.OrgId, s.Name, s.Description, s.TransformationId, s.CreatedAt,
		s.UpdatedAt); err != nil {
		return err
	}
	return replaceLinks(ctx, s.Id, s.DestinationIds)
}

// UpdateSource stores a source's new fields and replaces its destination links.
func UpdateSource(ctx context.Context, s model.Source) error {
	if err := exec(ctx, updateSource, s.Name, s.Description, s.TransformationId, s.UpdatedAt, s.Id,
		s.OrgId); err != nil {
		return err
	}
	return replaceLinks(ctx, s.Id, s.DestinationIds)
}

// GetSource returns one source of an organization with its destination IDs, or nil.
func GetSource(ctx context.Context, orgId, id string) (*model.Source, error) {
	rows, err := query(ctx, getSource, id, orgId)
	if err != nil || len(rows) == 0 {
		return nil, err
	}
	return withLinks(ctx, sourceFromRow(rows[0]))
}

// GetSourceByName returns the source with this name, or nil.
func GetSourceByName(ctx context.Context, orgId, name string) (*model.Source, error) {
	rows, err := query(ctx, getSourceByName, orgId, name)
	if err != nil || len(rows) == 0 {
		return nil, err
	}
	return withLinks(ctx, sourceFromRow(rows[0]))
}

// ListSources returns every source of an organization with its destination IDs.
func ListSources(ctx context.Context, orgId string) ([]model.Source, error) {
	rows, err := query(ctx, listSources, orgId)
	if err != nil {
		return nil, err
	}
	result := make([]model.Source, 0, len(rows))
	for _, row := range rows {
		s, err := withLinks(ctx, sourceFromRow(row))
		if err != nil {
			return nil, err
		}
		result = append(result, *s)
	}
	return result, nil
}

// DeleteSource removes a source and its destination links.
func DeleteSource(ctx context.Context, orgId, id string) error {
	if err := exec(ctx, deleteLinksOfSource, id); err != nil {
		return err
	}
	return exec(ctx, deleteSource, id, orgId)
}

func replaceLinks(ctx context.Context, sourceId string, destinationIds []string) error {
	if err := exec(ctx, deleteLinksOfSource, sourceId); err != nil {
		return err
	}
	for _, destinationId := range destinationIds {
		if err := exec(ctx, insertLink, sourceId, destinationId); err != nil {
			return err
		}
	}
	return nil
}

func withLinks(ctx context.Context, s model.Source) (*model.Source, error) {
	rows, err := query(ctx, listLinksOfSource, s.Id)
	if err != nil {
		return nil, err
	}
	s.DestinationIds = make([]string, 0, len(rows))
	for _, row := range rows {
		s.DestinationIds = append(s.DestinationIds, text(row["destination_id"]))
	}
	return &s, nil
}

func sourceFromRow(row map[string]interface{}) model.Source {
	return model.Source{
		Id:               text(row["source_id"]),
		OrgId:            text(row["org_id"]),
		Name:             text(row["name"]),
		Description:      text(row["description"]),
		TransformationId: text(row["transformation_id"]),
		CreatedAt:        text(row["created_at"]),
		UpdatedAt:        text(row["updated_at"]),
	}
}

// ---------- deliveries ----------

const deliveryColumns = `delivery_id, org_id, source_id, destination_id, destination_name, destination_type,
	profile_id, status, http_status, error, created_at`

var insertDelivery = dbmodel.DBQuery{ID: "CDS-INT-21",
	Query: `INSERT INTO deliveries (` + deliveryColumns + `)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)`}

var listDeliveries = dbmodel.DBQuery{ID: "CDS-INT-22",
	Query: `SELECT ` + deliveryColumns + ` FROM deliveries WHERE org_id = $1
		ORDER BY created_at DESC, delivery_id`}

// InsertDelivery records one send.
func InsertDelivery(ctx context.Context, d model.Delivery) error {
	return exec(ctx, insertDelivery, d.Id, d.OrgId, d.SourceId, d.DestinationId, d.DestinationName,
		d.DestinationType, d.ProfileId, d.Status, d.HTTPStatus, d.Error, d.CreatedAt)
}

// ListDeliveries returns the delivery log of an organization, newest first.
// An empty sourceId or profileId means no filter on that column.
func ListDeliveries(ctx context.Context, orgId, sourceId, profileId string) ([]model.Delivery, error) {
	rows, err := query(ctx, listDeliveries, orgId)
	if err != nil {
		return nil, err
	}
	result := make([]model.Delivery, 0, len(rows))
	for _, row := range rows {
		if sourceId != "" && text(row["source_id"]) != sourceId {
			continue
		}
		if profileId != "" && text(row["profile_id"]) != profileId {
			continue
		}
		status, _ := strconv.Atoi(text(row["http_status"]))
		result = append(result, model.Delivery{
			Id:              text(row["delivery_id"]),
			OrgId:           text(row["org_id"]),
			SourceId:        text(row["source_id"]),
			DestinationId:   text(row["destination_id"]),
			DestinationName: text(row["destination_name"]),
			DestinationType: text(row["destination_type"]),
			ProfileId:       text(row["profile_id"]),
			Status:          text(row["status"]),
			HTTPStatus:      status,
			Error:           text(row["error"]),
			CreatedAt:       text(row["created_at"]),
		})
	}
	return result, nil
}

// ---------- helpers ----------

func exec(ctx context.Context, q dbmodel.DBQuery, args ...interface{}) error {
	_, err := query(ctx, q, args...)
	return err
}

func query(ctx context.Context, q dbmodel.DBQuery, args ...interface{}) ([]map[string]interface{}, error) {
	dbClient, err := provider.NewDBProvider().GetDBClient()
	if err != nil {
		return nil, fmt.Errorf("failed to get database client: %w", err)
	}
	defer dbClient.Close()
	return dbClient.ExecuteQueryContext(ctx, q, args...)
}

// text reads a column as a string whichever Go type the driver returned.
func text(value interface{}) string {
	switch v := value.(type) {
	case nil:
		return ""
	case string:
		return v
	case []byte:
		return string(v)
	default:
		return fmt.Sprint(v)
	}
}
