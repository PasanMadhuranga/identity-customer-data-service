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

package authn

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/wso2/identity-customer-data-service/internal/system/config"
)

func newTestIntrospectionCache(clock *fakeClock) *introspectionCache {
	cache := newIntrospectionCache()
	cache.now = clock.Now
	return cache
}

func TestIntrospectionCache_keepsAResultForTheTTL(t *testing.T) {
	initRuntime(t)
	clock := &fakeClock{now: time.Unix(1_000_000, 0)}
	cache := newTestIntrospectionCache(clock)
	key := introspectionCacheKey("token", "acme", "acme")

	cache.put(key, map[string]interface{}{"active": true}, time.Minute, time.Time{})
	_, ok := cache.get(key)
	assert.True(t, ok)

	clock.Add(time.Minute)
	_, ok = cache.get(key)
	assert.False(t, ok)
}

func TestIntrospectionCache_neverKeepsAResultPastTheTokenExpiry(t *testing.T) {
	initRuntime(t)
	clock := &fakeClock{now: time.Unix(1_000_000, 0)}
	cache := newTestIntrospectionCache(clock)
	key := introspectionCacheKey("token", "acme", "acme")

	cache.put(key, map[string]interface{}{"active": true}, time.Minute, clock.Now().Add(10*time.Second))
	clock.Add(10 * time.Second)
	_, ok := cache.get(key)
	assert.False(t, ok)

	// A token that has already expired is not cached.
	cache.put(key, map[string]interface{}{"active": true}, time.Minute, clock.Now().Add(-time.Second))
	_, ok = cache.get(key)
	assert.False(t, ok)
}

func TestIntrospectionCache_keyDependsOnTheTokenAndTheOrganization(t *testing.T) {
	key := introspectionCacheKey("token", "acme", "acme")
	assert.NotContains(t, key, "token")
	assert.NotEqual(t, key, introspectionCacheKey("token", "other", "other"))
	assert.NotEqual(t, key, introspectionCacheKey("token2", "acme", "acme"))
}

func TestIntrospectionCache_returnsACopy(t *testing.T) {
	initRuntime(t)
	cache := newIntrospectionCache()
	key := introspectionCacheKey("token", "acme", "acme")
	cache.put(key, map[string]interface{}{"scope": "a"}, time.Minute, time.Time{})

	got, _ := cache.get(key)
	got["scope"] = "changed"

	again, _ := cache.get(key)
	assert.Equal(t, "a", again["scope"])
}

func TestIntrospectionCache_dropsResultsWhenTheConfigurationIsReplaced(t *testing.T) {
	initRuntime(t)
	cache := newIntrospectionCache()
	key := introspectionCacheKey("token", "acme", "acme")
	cache.put(key, map[string]interface{}{"active": true}, time.Minute, time.Time{})

	config.OverrideCDSRuntime(config.Config{})
	_, ok := cache.get(key)
	assert.False(t, ok)
}
