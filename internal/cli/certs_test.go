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
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// defaultCertificate is the certificate the pack ships: Identity Server 7.3.0's
// default, public part only.
var defaultCertificate = filepath.Join("testdata", "is-7.3.0-default.crt")

// Its private key is in every Identity Server download, so the runtime has to
// be able to say it is still in use — which depends on the fingerprint
// distribution/default-cert.sh pins.
func TestUsingDefaultCertificate(t *testing.T) {
	dir := t.TempDir()

	if !usingDefaultCertificate(defaultCertificate) {
		t.Error("the certificate the pack ships was not recognized as the default")
	}

	// The same subject on another key is not the default: only the fingerprint counts.
	lookalike := filepath.Join(dir, "lookalike.crt")
	writeTestCertificate(t, lookalike, "localhost")
	if usingDefaultCertificate(lookalike) {
		t.Error("a certificate with the default's subject was reported as the default")
	}

	// A path with nothing at it, and a file that is not a certificate, are
	// both "not the default" rather than errors: the caller is deciding
	// whether to warn, not whether CDS can start.
	if usingDefaultCertificate(filepath.Join(dir, "absent.crt")) {
		t.Error("a missing certificate was reported as the default")
	}
	if err := os.WriteFile(filepath.Join(dir, "junk.crt"), []byte("not a certificate"), 0o600); err != nil {
		t.Fatalf("could not stage the file: %v", err)
	}
	if usingDefaultCertificate(filepath.Join(dir, "junk.crt")) {
		t.Error("an unreadable file was reported as the default")
	}
}

// The default certificate is issued for localhost alone, which is why it only
// serves while CDS and the Identity Server reach each other as localhost.
func TestCertificateCovers(t *testing.T) {
	for host, want := range map[string]bool{"localhost": true, "cds.example.com": false} {
		covers, err := certificateCovers(defaultCertificate, host)
		if err != nil {
			t.Fatalf("certificateCovers: %v", err)
		}
		if covers != want {
			t.Errorf("the default certificate covers %s = %v, want %v", host, covers, want)
		}
	}

	issued := filepath.Join(t.TempDir(), "issued.crt")
	writeTestCertificate(t, issued, "cds.example.com")
	if covers, _ := certificateCovers(issued, "cds.example.com"); !covers {
		t.Error("a certificate issued for cds.example.com does not cover it")
	}
	if _, err := certificateCovers(filepath.Join(t.TempDir(), "absent.crt"), "localhost"); err == nil {
		t.Error("a missing certificate was not reported")
	}
}

func writeTestCertificate(t *testing.T, path, host string) {
	t.Helper()

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("could not generate a key: %v", err)
	}
	template := x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: host, Organization: []string{"WSO2"}},
		DNSNames:     []string{host},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
	}
	der, err := x509.CreateCertificate(rand.Reader, &template, &template, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("could not create the certificate: %v", err)
	}
	if err := os.WriteFile(path, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o600); err != nil {
		t.Fatalf("could not write the certificate: %v", err)
	}
}
