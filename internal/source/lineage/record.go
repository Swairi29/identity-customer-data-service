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

package lineage

import (
	"context"
	"time"

	"github.com/wso2/identity-customer-data-service/internal/source/model"
)

type sourceKey struct{}

// WithSource returns a context that carries the source a profile request names with ?source=, so the
// profile writes under it record that source in the attribute history. A nil source records no source.
func WithSource(ctx context.Context, source *model.Source) context.Context {
	if source == nil {
		return ctx
	}
	copied := *source
	return context.WithValue(ctx, sourceKey{}, &copied)
}

// SourceFrom returns the source the context carries, or nil when it carries none.
func SourceFrom(ctx context.Context) *model.Source {
	source, _ := ctx.Value(sourceKey{}).(*model.Source)
	return source
}

// Changes returns the history entries for a write of a profile: one per attribute whose value differs
// between before and after, all at the time of the write, naming the source the context carries (or no
// source). The entries go to profileID, the profile whose data the write changes.
func Changes(ctx context.Context, profileID string, before, after Attributes, at time.Time) []model.AttributeChange {
	diff := Diff(before, after)
	if len(diff) == 0 {
		return nil
	}
	var sourceID, sourceName *string
	if source := SourceFrom(ctx); source != nil {
		id, name := source.SourceID, source.Name
		sourceID, sourceName = &id, &name
	}
	changes := make([]model.AttributeChange, 0, len(diff))
	for _, change := range diff {
		changes = append(changes, model.AttributeChange{
			ProfileID:  profileID,
			Attribute:  change.Attribute,
			Action:     change.Kind,
			SourceID:   sourceID,
			SourceName: sourceName,
			ChangedAt:  at.UTC(),
		})
	}
	return changes
}
