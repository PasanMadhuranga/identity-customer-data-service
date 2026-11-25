/*
 * Copyright (c) 2025, WSO2 LLC. (http://www.wso2.com).
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

// Package managers registers the HTTP services of CDS on the server mux.
package managers

import (
	"net/http"
	"strings"

	"github.com/wso2/identity-customer-data-service/internal/system/config"
	"github.com/wso2/identity-customer-data-service/internal/system/constants"
	"github.com/wso2/identity-customer-data-service/internal/system/services"
	"github.com/wso2/identity-customer-data-service/internal/system/utils"
)

// ServiceManagerInterface registers the HTTP services of CDS.
type ServiceManagerInterface interface {
	RegisterServices() error
}

// ServiceManager registers the services on an HTTP mux, and the sync endpoints on a mux of their own when
// there is a sync listener.
type ServiceManager struct {
	mux     *http.ServeMux
	syncMux *http.ServeMux
}

// NewServiceManager creates a ServiceManager that serves every route on mux.
func NewServiceManager(mux *http.ServeMux) ServiceManagerInterface {
	return &ServiceManager{
		mux: mux,
	}
}

// NewSplitServiceManager creates a ServiceManager that serves the sync endpoints on syncMux, the mux of the
// sync listener, and every other route on mux.
func NewSplitServiceManager(mux, syncMux *http.ServeMux) ServiceManagerInterface {
	return &ServiceManager{
		mux:     mux,
		syncMux: syncMux,
	}
}

// syncRoutes are the patterns of the routes only the identity server calls, with the admin credentials. With
// a sync listener they are served there and nowhere else.
var syncRoutes = map[string]bool{
	"POST " + constants.APIBasePath + "/v1/profiles/sync":            true,
	"POST " + constants.APIBasePath + "/v1/profile-schema/sync":      true,
	"POST " + constants.APIBasePath + "/v1/organizations/event-sync": true,
	"POST " + constants.APIBasePath + "/v1/service-credentials/sync": true,
}

// sharedRoutes are served on both listeners: the admin configuration update takes the Console's token on
// the public listener and the identity server's admin credentials on the sync listener.
var sharedRoutes = map[string]bool{
	"PATCH " + constants.APIBasePath + "/v1/config": true,
}

// RegisterServices mounts the health services under /cds/ and the other services under the tenant path
// /t/{tenant}/. The organization service is mounted only when organization management is enabled. With a
// sync mux, the sync routes and the health services are mounted on it, and the public mux no longer
// serves the sync routes.
func (sm *ServiceManager) RegisterServices() error {
	// Non-tenanted root services (health, ready)
	rootMux := http.NewServeMux()
	_ = services.NewHealthService(rootMux) // registers /cds/api/v1/health, /ready
	sm.mux.Handle("/cds/", rootMux)

	// Initialize services with the shared tenant routes mux so they don't create their own mux
	routesMux := http.NewServeMux()
	_ = services.NewProfileService(routesMux)
	_ = services.NewProfileSchemaService(routesMux)
	_ = services.NewUnificationRulesService(routesMux)
	_ = services.NewConsentCategoryService(routesMux)
	_ = services.NewAdminConfigService(routesMux)
	_ = services.NewIdentityResolutionService(routesMux)
	_ = services.NewServiceCredentialsService(routesMux)
	if config.OrganizationManagementEnabled() {
		_ = services.NewOrganizationService(routesMux)
	}

	if sm.syncMux == nil {
		utils.MountTenantDispatcher(sm.mux, tenantHandler(routesMux, func(string) bool { return true }))
		return nil
	}
	sm.syncMux.Handle("/cds/", rootMux)
	utils.MountTenantDispatcher(sm.mux, tenantHandler(routesMux, func(pattern string) bool {
		return !syncRoutes[pattern]
	}))
	utils.MountTenantDispatcher(sm.syncMux, tenantHandler(routesMux, func(pattern string) bool {
		return syncRoutes[pattern] || sharedRoutes[pattern]
	}))
	return nil
}

// tenantHandler serves the routes of routesMux whose pattern serves accepts, and a 404 for the others. A request
// that matches no pattern is left to routesMux, which answers it with a 404 or a 405.
func tenantHandler(routesMux *http.ServeMux, serves func(pattern string) bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimSuffix(r.URL.Path, "/")
		if path == "" {
			r.URL.Path = "/"
		} else {
			r.URL.Path = path
		}
		if _, pattern := routesMux.Handler(r); pattern != "" && !serves(pattern) {
			http.NotFound(w, r)
			return
		}
		routesMux.ServeHTTP(w, r)
	}
}
