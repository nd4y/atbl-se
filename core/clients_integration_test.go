//go:build integration

package core

import (
	"context"
	"crypto/rsa"
	"crypto/x509"
	"encoding/hex"
	"encoding/pem"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestDiagnosticPanelIntegration(t *testing.T) {
	node := os.Getenv("AUTOBLOW_TEST_NODE")
	if node == "" {
		t.Fatal("AUTOBLOW_TEST_NODE required")
	}
	s, _ := fixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	for _, script := range []string{"panel-contracts.mjs", "panel-integration.mjs"} {
		cmd := exec.CommandContext(ctx, node, filepath.Join("..", "scripts", script))
		cmd.Env = append(os.Environ(), "AUTOBLOW_TEST_BASE=http://"+s.Address(), "AUTOBLOW_TEST_TOKEN="+s.Token())
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("%s: %v\n%s", script, err, out)
		}
		t.Log(string(out))
	}
}

func TestSDKAndStudioManualContracts(t *testing.T) {
	node, clients := os.Getenv("AUTOBLOW_TEST_NODE"), os.Getenv("AUTOBLOW_TEST_CLIENTS")
	if node == "" || clients == "" {
		t.Fatal("set AUTOBLOW_TEST_NODE and AUTOBLOW_TEST_CLIENTS for the integration target")
	}
	for _, transport := range []string{"http", "https"} {
		t.Run(transport, func(t *testing.T) {
			source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/csv" {
					io.WriteString(w, "0,0\n1000,90\n2000,0\n")
				} else {
					io.WriteString(w, testScript)
				}
			}))
			defer source.Close()
			cert, err := makeCertificate()
			if err != nil {
				t.Fatal(err)
			}
			dir := t.TempDir()
			certFile, keyFile := filepath.Join(dir, "server.crt"), filepath.Join(dir, "server.key")
			if err = os.WriteFile(certFile, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: cert.Certificate[0]}), 0600); err != nil {
				t.Fatal(err)
			}
			key := x509.MarshalPKCS1PrivateKey(cert.PrivateKey.(*rsa.PrivateKey))
			if err = os.WriteFile(keyFile, pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: key}), 0600); err != nil {
				t.Fatal(err)
			}
			s, _ := fixture(t, func(c *Config) {
				c.CompatibilityControl = true
				c.ExperimentalSyncScripts = true
				c.ExperimentalLocalScripts = true
				c.ScriptURLOrigins = []string{source.URL}
				c.MaxSpeed = 100
				c.TLSCertificateFile = certFile
				c.TLSPrivateKeyFile = keyFile
			})
			pair(t, s)
			catalog := readCatalogReference(t)
			binary, err := hex.DecodeString(catalog.Catalog.Hex)
			if err != nil {
				t.Fatal(err)
			}
			if err = ImportLocalScript(s.cfg.DataDir, catalog.Catalog.UUID, binary); err != nil {
				t.Fatal(err)
			}
			base := "http://" + s.Address()
			if transport == "https" {
				base = "https://" + s.DeviceAddress()
			}
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, node, "--experimental-transform-types", "--disable-warning=ExperimentalWarning", filepath.Join("..", "scripts", "client-contracts.mjs"))
			cmd.Env = append(os.Environ(), "AUTOBLOW_TEST_BASE="+base, "AUTOBLOW_TEST_TOKEN="+s.Token(), "AUTOBLOW_TEST_CLIENTS="+clients, "NODE_EXTRA_CA_CERTS="+certFile, "AUTOBLOW_TEST_SCRIPT_SOURCE="+source.URL)
			cmd.Env = append(cmd.Env, "AUTOBLOW_TEST_CATALOG_UUID="+catalog.Catalog.UUID)
			output, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("client contracts failed: %v\n%s", err, output)
			}
			t.Log(string(output))
		})
	}
}
