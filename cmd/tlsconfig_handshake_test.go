/*
Copyright 2026 The Kubernetes Authors

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package main

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"math/big"
	"net"
	"testing"
	"time"
)

// newSelfSignedCert builds a minimal self-signed ECDSA certificate for the
// in-memory TLS server used by the handshake tests below.
func newSelfSignedCert(t *testing.T) tls.Certificate {
	t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("failed to generate key: %v", err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "localhost"},
		NotBefore:    time.Now().Add(-1 * time.Hour),
		NotAfter:     time.Now().Add(1 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames:     []string{"localhost"},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("failed to create certificate: %v", err)
	}
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}
}

// serverTLSConfigFromEnv builds a server tls.Config through the operator's own
// configuration path: it sets TLS_GROUPS in the environment, parses it via
// parseTLSSettings, and applies the resulting tlsConfigFunc. This ties the test
// to the real code that ships, not a hand-built tls.Config.
func serverTLSConfigFromEnv(t *testing.T, groups string, cert tls.Certificate) *tls.Config {
	t.Helper()

	t.Setenv(envTLSMinVersion, "VersionTLS13")
	t.Setenv(envTLSCipherSuites, "")
	t.Setenv(envTLSGroups, groups)

	fn := parseTLSSettings().tlsConfigFunc()
	if fn == nil {
		t.Fatal("expected non-nil tlsConfigFunc for configured TLS groups")
	}
	cfg := &tls.Config{Certificates: []tls.Certificate{cert}}
	fn(cfg)
	return cfg
}

// doHandshake performs a TLS handshake between an in-memory client and server
// over net.Pipe and returns the client-observed connection state. The server
// handshake runs concurrently so both sides can make progress; deadlines guard
// against a hang if the handshake never completes.
func doHandshake(t *testing.T, serverCfg, clientCfg *tls.Config) (tls.ConnectionState, error) {
	t.Helper()

	clientConn, serverConn := net.Pipe()
	defer func() { _ = clientConn.Close() }()
	defer func() { _ = serverConn.Close() }()

	deadline := time.Now().Add(10 * time.Second)
	_ = clientConn.SetDeadline(deadline)
	_ = serverConn.SetDeadline(deadline)

	server := tls.Server(serverConn, serverCfg)
	client := tls.Client(clientConn, clientCfg)

	serverErr := make(chan error, 1)
	go func() { serverErr <- server.Handshake() }()

	clientErr := client.Handshake()
	if sErr := <-serverErr; sErr != nil {
		return tls.ConnectionState{}, sErr
	}
	if clientErr != nil {
		return tls.ConnectionState{}, clientErr
	}
	return client.ConnectionState(), nil
}

// TestTLSGroups_HandshakeNegotiatesConfiguredGroup proves that the group the
// operator configures via TLS_GROUPS is the one actually negotiated on the
// wire, including the post-quantum hybrid X25519MLKEM768. This is the
// end-to-end assertion the parsing-only unit tests cannot make.
func TestTLSGroups_HandshakeNegotiatesConfiguredGroup(t *testing.T) {
	cert := newSelfSignedCert(t)

	tests := []struct {
		name        string
		serverGroup string
		clientCurve tls.CurveID
		want        tls.CurveID
	}{
		{"classical X25519", groupX25519, tls.X25519, tls.X25519},
		{"post-quantum hybrid X25519MLKEM768", groupX25519MLKEM768, tls.X25519MLKEM768, tls.X25519MLKEM768},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			serverCfg := serverTLSConfigFromEnv(t, tt.serverGroup, cert)
			clientCfg := &tls.Config{
				InsecureSkipVerify: true, //nolint:gosec // test-only in-memory client, self-signed cert
				MinVersion:         tls.VersionTLS13,
				CurvePreferences:   []tls.CurveID{tt.clientCurve},
			}

			state, err := doHandshake(t, serverCfg, clientCfg)
			if err != nil {
				t.Fatalf("handshake failed: %v", err)
			}
			if state.CurveID != tt.want {
				t.Errorf("negotiated group = %v, want %v", state.CurveID, tt.want)
			}
		})
	}
}

// TestTLSGroups_HandshakeFailsOnGroupMismatch proves the configured group
// restriction actually bites: a client that offers only a group the server was
// not configured for cannot complete the handshake.
func TestTLSGroups_HandshakeFailsOnGroupMismatch(t *testing.T) {
	cert := newSelfSignedCert(t)

	serverCfg := serverTLSConfigFromEnv(t, groupCurveP256, cert)
	clientCfg := &tls.Config{
		InsecureSkipVerify: true, //nolint:gosec // test-only in-memory client, self-signed cert
		MinVersion:         tls.VersionTLS13,
		CurvePreferences:   []tls.CurveID{tls.X25519},
	}

	if _, err := doHandshake(t, serverCfg, clientCfg); err == nil {
		t.Fatal("expected handshake to fail when client and server share no TLS group")
	}
}
