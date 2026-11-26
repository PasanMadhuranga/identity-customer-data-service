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
	"crypto/sha256"
	"encoding/hex"
	"sync"
	"time"

	"github.com/wso2/identity-customer-data-service/internal/system/config"
)

// maxIntrospectionCacheEntries bounds the introspection results held. When the
// cache is full and none has expired, a new result is not cached.
const maxIntrospectionCacheEntries = 10000

// cachedIntrospection is an active introspection result and when it stops being
// used.
type cachedIntrospection struct {
	claims    map[string]interface{}
	expiresAt time.Time
}

// introspectionCache holds active introspection results for a short time, so a
// client that sends many requests with one token does not cost one introspection
// each. The checks of the token run on every request, with the cached result. An
// inactive result is never cached, so a token that is not active yet is not
// refused for longer than one request.
type introspectionCache struct {
	mu      sync.Mutex
	entries map[string]cachedIntrospection
	// runtime is the configuration the results were cached under. They are
	// dropped when it is replaced.
	runtime *config.CDSRuntime
	now     func() time.Time
}

func newIntrospectionCache() *introspectionCache {
	return &introspectionCache{entries: map[string]cachedIntrospection{}, now: time.Now}
}

// introspectionCacheKey identifies the token for the organizations it is
// introspected for. It is a hash, so the cache does not hold tokens.
func introspectionCacheKey(token, orgHandle, primaryHandle string) string {
	sum := sha256.Sum256([]byte(token + "\x00" + orgHandle + "\x00" + primaryHandle))
	return hex.EncodeToString(sum[:])
}

// get returns a copy of the cached result, so a caller that changes it does not
// change what the next request reads.
func (c *introspectionCache) get(key string) (map[string]interface{}, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.dropIfConfigChanged()
	entry, ok := c.entries[key]
	if !ok {
		return nil, false
	}
	if !c.now().Before(entry.expiresAt) {
		delete(c.entries, key)
		return nil, false
	}
	return copyClaims(entry.claims), true
}

// put caches an active result for ttl, but not past tokenExpiry, when it is set.
func (c *introspectionCache) put(key string, claims map[string]interface{}, ttl time.Duration,
	tokenExpiry time.Time) {
	now := c.now()
	expiresAt := now.Add(ttl)
	if !tokenExpiry.IsZero() && tokenExpiry.Before(expiresAt) {
		expiresAt = tokenExpiry
	}
	if !now.Before(expiresAt) {
		return
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	c.dropIfConfigChanged()
	if _, exists := c.entries[key]; !exists && len(c.entries) >= maxIntrospectionCacheEntries {
		for k, entry := range c.entries {
			if !now.Before(entry.expiresAt) {
				delete(c.entries, k)
			}
		}
		if len(c.entries) >= maxIntrospectionCacheEntries {
			return
		}
	}
	c.entries[key] = cachedIntrospection{claims: copyClaims(claims), expiresAt: expiresAt}
}

// dropIfConfigChanged drops every result when the runtime configuration was
// replaced since they were cached. The caller holds c.mu.
func (c *introspectionCache) dropIfConfigChanged() {
	if current := config.GetCDSRuntime(); current != c.runtime {
		c.entries = map[string]cachedIntrospection{}
		c.runtime = current
	}
}

func copyClaims(claims map[string]interface{}) map[string]interface{} {
	copied := make(map[string]interface{}, len(claims))
	for name, value := range claims {
		copied[name] = value
	}
	return copied
}
