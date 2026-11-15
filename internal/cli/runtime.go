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
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v2"
)

// runtimeConfig is the part of repository/conf/deployment.yaml the CLI needs:
// where to reach the running server, and which Identity Server the runtime is
// configured to call. The runtime itself loads the whole file; the CLI reads
// only these fields so that an unresolved ${PLACEHOLDER} elsewhere in the file
// — normal before the first start — cannot stop `verify` from answering.
type runtimeConfig struct {
	Addr struct {
		Host string `yaml:"host"`
		Port int    `yaml:"port"`
	} `yaml:"addr"`
	ServerURL string `yaml:"server_url"`
	TLS       struct {
		CertDir    string `yaml:"cert_dir"`
		ServerCert string `yaml:"server_cert"`
		TrustStore string `yaml:"trust_store"`
	} `yaml:"tls"`
	AuthServer struct {
		Host string `yaml:"host"`
		Port int    `yaml:"port"`
	} `yaml:"auth_server"`
}

func readRuntimeConfig(cdsHome string) (*runtimeConfig, error) {

	raw, err := os.ReadFile(path(cdsHome, configFile))
	if err != nil {
		return nil, fmt.Errorf("could not read %s: %w", configFile, err)
	}

	var cfg runtimeConfig
	if err := yaml.Unmarshal(raw, &cfg); err != nil {
		return nil, fmt.Errorf("could not parse %s: %w", configFile, err)
	}
	if cfg.Addr.Port == 0 {
		return nil, fmt.Errorf("%s sets no addr.port, so the CDS endpoint is unknown", configFile)
	}
	if cfg.Addr.Host == "" {
		cfg.Addr.Host = "127.0.0.1"
	}
	return &cfg, nil
}

// serverCertificate is the path of the certificate CDS presents, as the
// configuration names it.
func (c *runtimeConfig) serverCertificate(cdsHome string) string {

	name := c.TLS.ServerCert
	if name == "" {
		name = "server.crt"
	}
	return filepath.Join(c.certDir(cdsHome), name)
}

// trustStore is the path of the Identity Server certificates CDS trusts, or ""
// when the configuration names none and only the system roots apply.
func (c *runtimeConfig) trustStore(cdsHome string) string {

	if c.TLS.TrustStore == "" {
		return ""
	}
	return filepath.Join(c.certDir(cdsHome), c.TLS.TrustStore)
}

// certDir resolves tls.cert_dir as the runtime does: relative to the CDS home,
// and etc/certs under it when unset.
func (c *runtimeConfig) certDir(cdsHome string) string {

	dir := c.TLS.CertDir
	if dir == "" {
		return path(cdsHome, "etc/certs")
	}
	if filepath.IsAbs(dir) {
		return dir
	}
	return filepath.Join(cdsHome, dir)
}

// localBase is the address the CLI itself calls. It is always the configured
// bind address rather than server_url, because server_url is the public name
// an operator may have pointed at a load balancer — a liveness check has to
// reach this process, not the deployment.
func (c *runtimeConfig) localBase() string {

	host := c.Addr.Host
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = "127.0.0.1"
	}
	return "https://" + net.JoinHostPort(host, strconv.Itoa(c.Addr.Port))
}

// addressInUse reports whether something is already listening where CDS is
// configured to listen.
//
// Checked before starting rather than left to the runtime, because the failure
// is otherwise ambiguous in the worst way: the process that cannot bind exits,
// while whatever already holds the port answers the readiness probe, and the
// start looks like it worked.
func (c *runtimeConfig) addressInUse() bool {

	host := c.Addr.Host
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = "127.0.0.1"
	}
	conn, err := net.DialTimeout("tcp", net.JoinHostPort(host, strconv.Itoa(c.Addr.Port)), 2*time.Second)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

// localClient calls the CDS process over its own loopback address. CDS serves
// HTTPS only, and packs are installed with a certificate issued for the
// deployment's public name, which will not match 127.0.0.1. Verification is
// therefore skipped for these local lifecycle probes only; nothing else in the
// CLI reaches a remote host with this client.
func localClient(timeout time.Duration) *http.Client {

	return &http.Client{
		Timeout: timeout,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true, MinVersion: tls.VersionTLS12}, // #nosec G402
		},
	}
}

// probe reports the HTTP status of one CDS endpoint, or an error if the
// process could not be reached at all.
func probe(base, endpoint string, timeout time.Duration) (int, string, error) {

	resp, err := localClient(timeout).Get(base + endpoint)
	if err != nil {
		return 0, "", err
	}
	defer func() { _ = resp.Body.Close() }()

	body := make([]byte, 512)
	n, _ := resp.Body.Read(body)
	return resp.StatusCode, strings.TrimSpace(string(body[:n])), nil
}
