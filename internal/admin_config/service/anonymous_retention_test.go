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

package service

import (
	"errors"
	"math"
	"net/http"
	"testing"

	errors2 "github.com/wso2/identity-customer-data-service/internal/system/errors"
)

func TestValidateAnonymousProfileRetention(t *testing.T) {
	for _, days := range []int{0, 1, 90, 365, 366, math.MaxInt32} {
		if err := validateAnonymousProfileRetention(days); err != nil {
			t.Errorf("validateAnonymousProfileRetention(%d) = %v, want nil", days, err)
		}
	}
	for _, days := range []int{-1, math.MinInt32} {
		err := validateAnonymousProfileRetention(days)
		var clientError *errors2.ClientError
		if !errors.As(err, &clientError) || clientError.StatusCode != http.StatusBadRequest {
			t.Errorf("validateAnonymousProfileRetention(%d) = %v, want a 400 client error", days, err)
		}
	}
}
