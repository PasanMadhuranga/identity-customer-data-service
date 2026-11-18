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
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

// The secrets file is the operator's: comments, blank lines and quoted values
// are all things a person writes by hand.
func TestLoadSecrets(t *testing.T) {
	cdsHome := t.TempDir()
	if err := os.MkdirAll(filepath.Join(cdsHome, confDir), 0o750); err != nil {
		t.Fatalf("could not stage the configuration directory: %v", err)
	}
	content := "# set by the operator\n\nAUTH_SERVER_ADMIN_PASSWORD=plain\nDB_PASSWORD=\"quoted\"\nnot a pair\n"
	if err := os.WriteFile(filepath.Join(cdsHome, secretsFile), []byte(content), 0o600); err != nil {
		t.Fatalf("could not write the secrets file: %v", err)
	}

	secrets, err := loadSecrets(cdsHome)
	if err != nil {
		t.Fatalf("loadSecrets: %v", err)
	}
	if len(secrets) != 2 || secrets["AUTH_SERVER_ADMIN_PASSWORD"] != "plain" || secrets["DB_PASSWORD"] != "quoted" {
		t.Errorf("loadSecrets = %v", secrets)
	}

	// A variable already in the environment wins over the file.
	t.Setenv("DB_PASSWORD", "from-the-environment")
	env := environment(secrets)
	if len(env) != 1 || env[0] != "AUTH_SERVER_ADMIN_PASSWORD=plain" {
		t.Errorf("environment = %v, want only the variable the environment does not already set", env)
	}
}

func TestLoadSecretsWithoutAFile(t *testing.T) {
	// An installation whose secrets come from a secret manager has no file,
	// and that is not a failure.
	secrets, err := loadSecrets(t.TempDir())
	if err != nil {
		t.Fatalf("loadSecrets: %v", err)
	}
	if len(secrets) != 0 {
		t.Errorf("expected no secrets, got %d", len(secrets))
	}
}

// verify trusts the Identity Server only through the trust store CDD itself
// uses, so a certificate missing from it is reported, and found once added.
func TestVerifyIdentityServerUsesTheTrustStore(t *testing.T) {
	is := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer is.Close()
	address, _ := url.Parse(is.URL)
	port, _ := strconv.Atoi(address.Port())

	cdsHome := t.TempDir()
	var config runtimeConfig
	config.AuthServer.Host = address.Hostname()
	config.AuthServer.Port = port
	config.TLS.CertDir = "certs"
	config.TLS.TrustStore = "trust_store.pem"
	if err := os.MkdirAll(filepath.Join(cdsHome, "certs"), 0o750); err != nil {
		t.Fatalf("could not stage the certificate directory: %v", err)
	}
	trustStore := filepath.Join(cdsHome, "certs", "trust_store.pem")

	// Every httptest server shares one certificate, so the untrusted one is
	// generated rather than taken from a second server.
	writeTestCertificate(t, trustStore, "localhost")
	untrusted := &checks{}
	verifyIdentityServer(untrusted, cdsHome, &config)
	if untrusted.failed != 1 {
		t.Error("an Identity Server whose certificate is not in the trust store was reported as trusted")
	}

	writePEM(t, trustStore, is.Certificate().Raw)
	trusted := &checks{}
	verifyIdentityServer(trusted, cdsHome, &config)
	if trusted.failed != 0 || trusted.run != 1 {
		t.Errorf("an Identity Server in the trust store was not trusted: %d of %d failed", trusted.failed,
			trusted.run)
	}
}

func writePEM(t *testing.T, path string, der []byte) {
	t.Helper()
	if err := os.WriteFile(path, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o600); err != nil {
		t.Fatalf("could not write %s: %v", path, err)
	}
}
