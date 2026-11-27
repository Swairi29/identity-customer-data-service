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
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/wso2/identity-customer-data-service/internal/source/model"
)

var (
	day1 = time.Date(2026, 11, 20, 10, 0, 0, 0, time.UTC)
	day2 = time.Date(2026, 11, 25, 9, 0, 0, 0, time.UTC)
	day3 = time.Date(2026, 11, 26, 12, 0, 0, 0, time.UTC)
	day4 = time.Date(2026, 11, 27, 9, 15, 0, 0, time.UTC)
)

func str(s string) *string { return &s }

func row(attribute, action string, source *string, at time.Time) model.AttributeChange {
	return model.AttributeChange{Attribute: attribute, Action: action, SourceID: source, SourceName: source,
		ChangedAt: at}
}

func current(attribute string, source *string, createdAt, updatedAt time.Time) model.AttributeSource {
	return model.AttributeSource{Attribute: attribute, SourceID: source, SourceName: source, CreatedAt: createdAt,
		UpdatedAt: updatedAt}
}

func TestTheCurrentSourceIsTheLatestChange(t *testing.T) {
	history := []model.AttributeChange{
		row("traits.email", model.ActionSet, str("CRM"), day4),
		row("traits.phone", model.ActionSet, str("Booking App"), day2),
		row("traits.email", model.ActionSet, str("Booking App"), day2),
		row("traits.email", model.ActionSet, str("CRM"), day1),
	}
	assert.Equal(t, []model.AttributeSource{
		current("traits.email", str("CRM"), day1, day4),
		current("traits.phone", str("Booking App"), day2, day2),
	}, CurrentSources(history), "created_at is the first SET, updated_at and the source the latest change")
}

func TestARemovedAttributeIsLeftOut(t *testing.T) {
	history := []model.AttributeChange{
		row("traits.phone", model.ActionRemoved, nil, day3),
		row("traits.phone", model.ActionSet, str("CRM"), day1),
		row("traits.city", model.ActionSet, nil, day1),
	}
	assert.Equal(t, []model.AttributeSource{current("traits.city", nil, day1, day1)}, CurrentSources(history),
		"a change without a source gives a nil source")
}

func TestAnAttributeSetAgainAfterARemovalStartsAgain(t *testing.T) {
	history := []model.AttributeChange{
		row("traits.phone", model.ActionSet, str("CRM"), day1),
		row("traits.phone", model.ActionRemoved, nil, day2),
		row("traits.phone", model.ActionSet, str("Booking App"), day3),
		row("traits.phone", model.ActionSet, str("CRM"), day4),
	}
	assert.Equal(t, []model.AttributeSource{current("traits.phone", str("CRM"), day3, day4)},
		CurrentSources(history), "created_at is when it was set again, not the first SET before the removal")
}

func TestAMergedChangeCounts(t *testing.T) {
	history := []model.AttributeChange{
		row("traits.city", model.ActionSet, str("CRM"), day1),
		row("traits.city", model.ActionMerged, str("Booking App"), day2),
	}
	assert.Equal(t, []model.AttributeSource{current("traits.city", str("Booking App"), day1, day2)},
		CurrentSources(history))
}

func TestNoHistoryNoAttributes(t *testing.T) {
	assert.Equal(t, []model.AttributeSource{}, CurrentSources(nil))
}
