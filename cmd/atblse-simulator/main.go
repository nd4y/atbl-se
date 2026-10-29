// The simulator never connects to the official service or a physical device.
package main

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"flag"
	"fmt"
	"math/big"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"time"

	"github.com/nd4y/atblse/core"
	"github.com/nd4y/atblse/internal/simulator"
)

func main() {
	if e := run(); e != nil {
		fmt.Fprintln(os.Stderr, e)
		os.Exit(1)
	}
}
func run() error {
	lab := flag.Bool("lab", false, "start an isolated loopback server with ephemeral data and TLS")
	endpoint := flag.String("url", "wss://127.0.0.1:20443/device-backend/ws", "loopback server endpoint")
	ca := flag.String("ca", "", "PEM certificate of the local test server")
	duration := flag.Duration("duration", 60*time.Second, "lifetime, from 1s to 1h")
	flag.Parse()
	if flag.NArg() != 0 || *duration < time.Second || *duration > time.Hour {
		return fmt.Errorf("unexpected arguments or duration outside 1s..1h")
	}
	signalCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	ctx, cancel := context.WithTimeout(signalCtx, *duration)
	defer cancel()
	var roots *x509.CertPool
	if *lab {
		if *ca != "" || *endpoint != "wss://127.0.0.1:20443/device-backend/ws" {
			return fmt.Errorf("--lab creates its own endpoint and certificate")
		}
		s, cert, cleanup, e := startLab()
		if e != nil {
			return e
		}
		defer cleanup()
		roots = x509.NewCertPool()
		roots.AppendCertsFromPEM(cert)
		*endpoint = "wss://" + s.DeviceAddress() + "/device-backend/ws"
		// Deliberate developer output: this key only controls the ephemeral loopback lab.
		json.NewEncoder(os.Stdout).Encode(map[string]any{"synthetic": true, "physicalDevice": false, "version": core.Version, "api": "http://" + s.Address(), "controlToken": s.Token(), "pairMAC": simulator.MAC, "durationSeconds": duration.Seconds()})
	} else {
		if *ca == "" {
			return fmt.Errorf("--ca is required; use --lab for a self-contained local server")
		}
		cert, e := os.ReadFile(*ca)
		if e != nil {
			return e
		}
		roots = x509.NewCertPool()
		if !roots.AppendCertsFromPEM(cert) {
			return fmt.Errorf("invalid CA PEM")
		}
	}
	return simulator.Run(ctx, *endpoint, roots)
}

func startLab() (*core.Server, []byte, func(), error) {
	dir, e := os.MkdirTemp("", "atblse-lab-")
	if e != nil {
		return nil, nil, nil, e
	}
	cleanupDir := func() { os.RemoveAll(dir) }
	key, e := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if e != nil {
		cleanupDir()
		return nil, nil, nil, e
	}
	serial, e := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 120))
	if e != nil {
		cleanupDir()
		return nil, nil, nil, e
	}
	cert := &x509.Certificate{SerialNumber: serial, Subject: pkix.Name{CommonName: "atblse synthetic lab"}, NotBefore: time.Now().Add(-time.Minute), NotAfter: time.Now().Add(2 * time.Hour), IPAddresses: []net.IP{net.ParseIP("127.0.0.1")}, KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
	der, e := x509.CreateCertificate(rand.Reader, cert, cert, &key.PublicKey, key)
	if e != nil {
		cleanupDir()
		return nil, nil, nil, e
	}
	private, e := x509.MarshalECPrivateKey(key)
	if e != nil {
		cleanupDir()
		return nil, nil, nil, e
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	certPath, keyPath := filepath.Join(dir, "lab.crt"), filepath.Join(dir, "lab.key")
	if e = os.WriteFile(certPath, certPEM, 0600); e != nil {
		cleanupDir()
		return nil, nil, nil, e
	}
	if e = os.WriteFile(keyPath, pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: private}), 0600); e != nil {
		cleanupDir()
		return nil, nil, nil, e
	}
	cfg := core.DefaultConfig()
	cfg.APIListen = "127.0.0.1:0"
	cfg.DeviceListen = "127.0.0.1:0"
	cfg.DataDir = filepath.Join(dir, "data")
	cfg.TLSCertificateFile = certPath
	cfg.TLSPrivateKeyFile = keyPath
	cfg.CompatibilityControl = true
	cfg.ExperimentalSyncScripts = true
	cfg.ExperimentalLocalScripts = true
	cfg.MaxSpeed = 100
	s, e := core.New(cfg)
	if e != nil {
		cleanupDir()
		return nil, nil, nil, e
	}
	if e = s.Start(); e != nil {
		cleanupDir()
		return nil, nil, nil, e
	}
	cleanup := func() {
		ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
		defer cancel()
		s.Close(ctx)
		cleanupDir()
	}
	return s, certPEM, cleanup, nil
}
