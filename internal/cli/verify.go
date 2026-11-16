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
	"crypto/x509"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"time"
)

// checks accumulates the result of a verification run.
type checks struct {
	run    int
	failed int
}

func (c *checks) passf(format string, a ...any) {
	c.run++
	donef(format, a...)
}

func (c *checks) failf(format string, a ...any) {
	c.run++
	c.failed++
	fmt.Fprintf(os.Stderr, "    %s %s\n", red("x"), fmt.Sprintf(format, a...))
}

// runVerify checks the installation without changing anything.
//
// Every check goes over the network or reads CDS's own files, never the
// Identity Server's, so it answers the same whether the Identity Server runs
// beside it, on another host or in a container.
func runVerify(args []string) error {
	flags := newFlagSet("verify")
	if err := parse(flags, args); err != nil {
		return err
	}

	cdsHome, err := home()
	if err != nil {
		return err
	}
	config, err := readRuntimeConfig(cdsHome)
	if err != nil {
		return err
	}

	results := &checks{}

	stepf("CDS runtime")
	base := config.localBase()
	if status, _, err := probe(base, "/cds/api/v1/ready", 10*time.Second); err != nil {
		results.failf("CDS is not reachable at %s: %v", base, err)
	} else if status != http.StatusOK {
		results.failf("GET /cds/api/v1/ready returned HTTP %d, so the datastore is not ready", status)
	} else {
		results.passf("GET /cds/api/v1/ready returns 200, so the datastore is reachable and initialized")
	}

	stepf("TLS")
	verifyCertificate(results, cdsHome, config)

	stepf("Identity Server")
	verifyIdentityServer(results, cdsHome, config)

	return summarize(results)
}

// verifyCertificate checks the certificate CDS presents: that it is one issued
// for this deployment rather than the pack's default, and that it is valid for
// the host in server_url, which is the name the Identity Server calls CDS on.
func verifyCertificate(results *checks, cdsHome string, config *runtimeConfig) {
	certificate := config.serverCertificate(cdsHome)
	if !fileExists(certificate) {
		results.failf("no TLS certificate at %s", certificate)
		return
	}
	if usingDefaultCertificate(certificate) {
		results.failf("CDS is presenting the default certificate the pack shipped with; " +
			"its private key is in every Identity Server 7.3.0 download, so replace it " +
			"with one issued for this deployment")
	} else {
		results.passf("the TLS certificate was issued for this deployment")
	}

	host := hostOf(config.ServerURL)
	if host == "" {
		results.failf("server_url %q names no host", config.ServerURL)
		return
	}
	covers, err := certificateCovers(certificate, host)
	switch {
	case err != nil:
		results.failf("could not read %s: %v", certificate, err)
	case !covers:
		results.failf("the TLS certificate is not valid for %s, the host in server_url, "+
			"so the Identity Server cannot verify CDS there", host)
	default:
		results.passf("the TLS certificate is valid for %s, the host in server_url", host)
	}
}

// verifyIdentityServer checks that CDS can reach the Identity Server it is
// configured to call, and verify its certificate with the trust store CDS
// itself uses.
func verifyIdentityServer(results *checks, cdsHome string, config *runtimeConfig) {
	if config.AuthServer.Host == "" || config.AuthServer.Port == 0 {
		results.failf("auth_server sets no host and port, so the Identity Server is unknown")
		return
	}
	isBase := "https://" + net.JoinHostPort(config.AuthServer.Host, strconv.Itoa(config.AuthServer.Port))

	roots, err := x509.SystemCertPool()
	if err != nil || roots == nil {
		roots = x509.NewCertPool()
	}
	if trustStore := config.trustStore(cdsHome); trustStore != "" {
		trusted, err := os.ReadFile(filepath.Clean(trustStore))
		if err != nil {
			results.failf("could not read the trust store %s: %v", trustStore, err)
			return
		}
		if !roots.AppendCertsFromPEM(trusted) {
			results.failf("the trust store %s holds no PEM certificate", trustStore)
			return
		}
	}

	client := &http.Client{
		Timeout: 10 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS12},
		},
	}
	resp, err := client.Get(isBase + "/api/health-check/v1.0/health")
	var unverified *tls.CertificateVerificationError
	switch {
	case errors.As(err, &unverified):
		results.failf("the Identity Server at %s presents a certificate CDS does not trust: %v", isBase,
			unverified.Err)
		return
	case err != nil:
		results.failf("the Identity Server is not reachable at %s: %v", isBase, err)
		return
	}
	_ = resp.Body.Close()
	results.passf("the Identity Server at %s is reachable and its certificate is trusted", isBase)
}

func hostOf(rawURL string) string {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}
	return parsed.Hostname()
}

func summarize(results *checks) error {
	fmt.Println()
	if results.failed == 0 {
		donef("all %d checks passed", results.run)
		return nil
	}
	return fmt.Errorf("%d of %d checks failed", results.failed, results.run)
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
