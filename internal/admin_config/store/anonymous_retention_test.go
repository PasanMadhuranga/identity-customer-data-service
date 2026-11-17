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
	"testing"

	"github.com/wso2/identity-customer-data-service/internal/system/constants"
)

func TestParseRetentionDays(t *testing.T) {
	tests := []struct {
		value  string
		want   int
		wantOK bool
	}{
		{"1", 1, true},
		{"90", 90, true},
		{"365", 365, true},
		{"0", 0, false},
		{"-1", 0, false},
		{"366", 366, true},
		{"2147483647", 2147483647, true},
		{"2147483648", 0, false},
		{"ninety", 0, false},
		{"", 0, false},
		{"30.5", 0, false},
	}
	for _, tt := range tests {
		t.Run(tt.value, func(t *testing.T) {
			got, ok := parseRetentionDays(tt.value)
			if got != tt.want || ok != tt.wantOK {
				t.Errorf("parseRetentionDays(%q) = %d, %v; want %d, %v", tt.value, got, ok, tt.want, tt.wantOK)
			}
		})
	}
}

// An organization that has stored no retention reads as unset, so the default applies.
func TestAdminConfigRowsLeaveRetentionUnsetByDefault(t *testing.T) {
	config := scanAdminConfigRows([]map[string]interface{}{
		{"config": constants.ConfigCDSEnabled, "value": "true"},
	})
	if config.AnonymousProfileRetentionDays != 0 {
		t.Errorf("AnonymousProfileRetentionDays = %d, want 0 (unset)", config.AnonymousProfileRetentionDays)
	}
}

func TestAdminConfigRowsReadAStoredRetention(t *testing.T) {
	config := scanAdminConfigRows([]map[string]interface{}{
		{"config": constants.ConfigAnonymousProfileRetentionDays, "value": "30"},
	})
	if config.AnonymousProfileRetentionDays != 30 {
		t.Errorf("AnonymousProfileRetentionDays = %d, want 30", config.AnonymousProfileRetentionDays)
	}
}

// A stored value outside the allowed range is ignored.
func TestAdminConfigRowsIgnoreAnOutOfRangeRetention(t *testing.T) {
	for _, value := range []string{"0", "-3", "2147483648", "abc"} {
		config := scanAdminConfigRows([]map[string]interface{}{
			{"config": constants.ConfigAnonymousProfileRetentionDays, "value": value},
		})
		if config.AnonymousProfileRetentionDays != 0 {
			t.Errorf("value %q: AnonymousProfileRetentionDays = %d, want 0 (unset)", value,
				config.AnonymousProfileRetentionDays)
		}
	}
}
