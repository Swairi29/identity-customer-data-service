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

package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	orgModel "github.com/wso2/identity-customer-data-service/internal/organization/model"
	"github.com/wso2/identity-customer-data-service/internal/source/lineage"
	sourceModel "github.com/wso2/identity-customer-data-service/internal/source/model"
	sourceProvider "github.com/wso2/identity-customer-data-service/internal/source/provider"
	sourceService "github.com/wso2/identity-customer-data-service/internal/source/service"
	errors2 "github.com/wso2/identity-customer-data-service/internal/system/errors"
)

// A profile request can name the app that sends it with ?source=<sourceId>. Without it, the request works
// exactly as before. With it, the source must be registered, and a source linked to a transformation sends
// its own format, which goes through the transformation.

// sources returns the source service. Tests replace it to link transformations.
var sources = func() sourceService.SourceServiceInterface {
	return sourceProvider.NewSourceProvider().GetSourceService()
}

// requestSource returns the source the request names with ?source=, or nil when it names none.
func requestSource(ctx context.Context, r *http.Request, org orgModel.Ref) (*sourceModel.Source, error) {
	values, ok := r.URL.Query()["source"]
	if !ok {
		return nil, nil
	}
	return sources().ResolveRequestSource(ctx, org, values[0])
}

// writeContext returns the context for the profile writes of a request: it carries the request's source,
// so the attribute history of the writes names it. Without a source, the history names none.
func writeContext(ctx context.Context, source *sourceModel.Source) context.Context {
	return lineage.WithSource(ctx, source)
}

// bodyToCDS returns a request body in CDS format. A request without a source, or from a source without a
// transformation, already is, so its body is returned as it is; any other goes through the transformation.
// code names the operation in the 400 for a body that is not a JSON object.
func bodyToCDS(ctx context.Context, org orgModel.Ref, source *sourceModel.Source, body []byte,
	code errors2.ErrorMessage) ([]byte, error) {
	if source == nil || source.TransformationID == nil {
		return body, nil
	}
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil || payload == nil {
		return nil, errors2.NewClientError(errors2.ErrorMessage{
			Code:        code.Code,
			Message:     code.Message,
			Description: fmt.Sprintf("The body from source '%s' must be a JSON object.", source.Name),
		}, http.StatusBadRequest)
	}
	transformed, err := sources().ToCDSFormat(ctx, org, *source, payload)
	if err != nil {
		return nil, err
	}
	out, err := json.Marshal(transformed)
	if err != nil {
		return nil, errors2.NewServerError(errors2.ErrorMessage{
			Code:        errors2.TransformSourcePayload.Code,
			Message:     errors2.TransformSourcePayload.Message,
			Description: "Failed to encode the transformed payload.",
		}, err)
	}
	return out, nil
}

// patchToCDS is bodyToCDS for a patch that is already decoded.
func patchToCDS(ctx context.Context, org orgModel.Ref, source *sourceModel.Source,
	patch map[string]interface{}) (map[string]interface{}, error) {
	if source == nil || source.TransformationID == nil {
		return patch, nil
	}
	return sources().ToCDSFormat(ctx, org, *source, patch)
}

// responseInSourceFormat returns the response to send: the value as it is, unless the source has a
// transformation, in which case the profile goes back through it into the source's own field names.
func responseInSourceFormat(ctx context.Context, org orgModel.Ref, source *sourceModel.Source,
	value any) (any, error) {
	if source == nil || source.TransformationID == nil {
		return value, nil
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	var profile map[string]any
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.UseNumber()
	if err := decoder.Decode(&profile); err != nil {
		return nil, err
	}
	return sources().ToSourceFormat(ctx, org, *source, profile)
}
