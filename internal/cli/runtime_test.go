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

package cli

import (
	"net"
	"path/filepath"
	"testing"
)

func TestRuntimeDefaultsCoverTheLogLevel(t *testing.T) {
	if runtimeDefaults["LOG_LEVEL"] == "" {
		t.Error("LOG_LEVEL has no default, so a pack cannot start without one being supplied")
	}
}

// start refuses an address something else holds, so the operator reads why
// rather than a bind error from deep in the server's startup.
func TestAddressInUseIsDetected(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("could not take a port: %v", err)
	}
	defer func() { _ = listener.Close() }()

	port := listener.Addr().(*net.TCPAddr).Port

	var config runtimeConfig
	config.Addr.Host = "127.0.0.1"
	config.Addr.Port = port
	if !config.addressInUse() {
		t.Error("a port held by another listener was reported as free")
	}

	// And the same address once it is released.
	_ = listener.Close()
	var freed runtimeConfig
	freed.Addr.Host = "127.0.0.1"
	freed.Addr.Port = port
	if freed.addressInUse() {
		t.Error("a released port was reported as in use")
	}
}

// The CLI looks for the certificates where the runtime does, or verify would
// report on files the server never reads.
func TestCertificatePathsResolveAsTheRuntimeDoes(t *testing.T) {
	home := "/opt/cdd"
	cases := []struct {
		name    string
		certDir string
		want    string
	}{
		{"unset", "", "/opt/cdd/etc/certs/server.crt"},
		{"relative to the home, as a pack sets it", "repository/resources/security",
			"/opt/cdd/repository/resources/security/server.crt"},
		{"absolute, as a container mounts it", "/etc/certs", "/etc/certs/server.crt"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var config runtimeConfig
			config.TLS.CertDir = c.certDir
			if got := config.serverCertificate(home); got != filepath.FromSlash(c.want) {
				t.Errorf("serverCertificate = %q, want %q", got, c.want)
			}
		})
	}

	var config runtimeConfig
	if got := config.trustStore(home); got != "" {
		t.Errorf("with no trust_store configured, trustStore = %q, want none", got)
	}
	config.TLS.CertDir = "repository/resources/security"
	config.TLS.TrustStore = "trust_store.pem"
	want := filepath.FromSlash("/opt/cdd/repository/resources/security/trust_store.pem")
	if got := config.trustStore(home); got != want {
		t.Errorf("trustStore = %q, want %q", got, want)
	}
}
