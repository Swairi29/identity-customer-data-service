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

// Package store reads and writes the transformations table.
package store

import (
	"context"
	"encoding/json"
	"fmt"

	dbmodel "github.com/wso2/identity-customer-data-service/internal/system/database/model"
	"github.com/wso2/identity-customer-data-service/internal/system/database/provider"
	"github.com/wso2/identity-customer-data-service/internal/transformation/model"
)

const columns = `transformation_id, org_id, name, mapping, template, version, mi_url, status, error,
	created_at, updated_at`

var insertTransformation = dbmodel.DBQuery{ID: "CDS-TRF-01",
	Query: `INSERT INTO transformations (` + columns + `)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)`}

var updateTransformation = dbmodel.DBQuery{ID: "CDS-TRF-02",
	Query: `UPDATE transformations SET mapping = $1, template = $2, version = $3, mi_url = $4, status = $5,
		error = $6, updated_at = $7 WHERE transformation_id = $8 AND org_id = $9`}

var getTransformation = dbmodel.DBQuery{ID: "CDS-TRF-03",
	Query: `SELECT ` + columns + ` FROM transformations WHERE transformation_id = $1 AND org_id = $2`}

var getTransformationByName = dbmodel.DBQuery{ID: "CDS-TRF-04",
	Query: `SELECT ` + columns + ` FROM transformations WHERE org_id = $1 AND name = $2`}

var listTransformations = dbmodel.DBQuery{ID: "CDS-TRF-05",
	Query: `SELECT ` + columns + ` FROM transformations WHERE org_id = $1 ORDER BY created_at`}

// Insert stores a new transformation.
func Insert(ctx context.Context, t model.Transformation) error {
	mapping, err := json.Marshal(t.Mappings)
	if err != nil {
		return err
	}
	return exec(ctx, insertTransformation, t.Id, t.OrgId, t.Name, string(mapping), t.Template, t.Version,
		t.MIUrl, t.Status, t.Error, t.CreatedAt, t.UpdatedAt)
}

// Update stores the mapping, version and deployment result of a transformation.
func Update(ctx context.Context, t model.Transformation) error {
	mapping, err := json.Marshal(t.Mappings)
	if err != nil {
		return err
	}
	return exec(ctx, updateTransformation, string(mapping), t.Template, t.Version, t.MIUrl, t.Status, t.Error,
		t.UpdatedAt, t.Id, t.OrgId)
}

// Get returns one transformation of an organization, or nil when there is none.
func Get(ctx context.Context, orgId, id string) (*model.Transformation, error) {
	rows, err := query(ctx, getTransformation, id, orgId)
	if err != nil || len(rows) == 0 {
		return nil, err
	}
	t := fromRow(rows[0])
	return &t, nil
}

// GetByName returns the transformation with this name in an organization, or nil.
func GetByName(ctx context.Context, orgId, name string) (*model.Transformation, error) {
	rows, err := query(ctx, getTransformationByName, orgId, name)
	if err != nil || len(rows) == 0 {
		return nil, err
	}
	t := fromRow(rows[0])
	return &t, nil
}

// List returns every transformation of an organization.
func List(ctx context.Context, orgId string) ([]model.Transformation, error) {
	rows, err := query(ctx, listTransformations, orgId)
	if err != nil {
		return nil, err
	}
	result := make([]model.Transformation, 0, len(rows))
	for _, row := range rows {
		result = append(result, fromRow(row))
	}
	return result, nil
}

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

func fromRow(row map[string]interface{}) model.Transformation {
	t := model.Transformation{
		Id:        text(row["transformation_id"]),
		OrgId:     text(row["org_id"]),
		Name:      text(row["name"]),
		Template:  text(row["template"]),
		Version:   text(row["version"]),
		MIUrl:     text(row["mi_url"]),
		Status:    text(row["status"]),
		Error:     text(row["error"]),
		CreatedAt: text(row["created_at"]),
		UpdatedAt: text(row["updated_at"]),
	}
	_ = json.Unmarshal([]byte(text(row["mapping"])), &t.Mappings)
	return t
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
