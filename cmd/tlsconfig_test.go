package main

import (
	"crypto/tls"
	"testing"
)

type noopLogger struct{}

func (noopLogger) Info(_ string, _ ...any) {}

func TestParseTLSVersion(t *testing.T) {
	tests := []struct {
		input string
		want  uint16
	}{
		{"VersionTLS10", tls.VersionTLS10},
		{"VersionTLS11", tls.VersionTLS11},
		{"VersionTLS12", tls.VersionTLS12},
		{"VersionTLS13", tls.VersionTLS13},
		{"", 0},
		{"unknown", 0},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := parseTLSVersion(tt.input)
			if got != tt.want {
				t.Errorf("parseTLSVersion(%q) = %d, want %d", tt.input, got, tt.want)
			}
		})
	}
}

func TestParseCipherSuites(t *testing.T) {
	log := noopLogger{}

	t.Run("known IANA names", func(t *testing.T) {
		suites := parseCipherSuites("TLS_AES_128_GCM_SHA256,TLS_AES_256_GCM_SHA384", log)
		if len(suites) != 2 {
			t.Fatalf("expected 2 suites, got %d", len(suites))
		}
		if suites[0] != tls.TLS_AES_128_GCM_SHA256 {
			t.Errorf("suites[0] = %#x, want %#x", suites[0], tls.TLS_AES_128_GCM_SHA256)
		}
		if suites[1] != tls.TLS_AES_256_GCM_SHA384 {
			t.Errorf("suites[1] = %#x, want %#x", suites[1], tls.TLS_AES_256_GCM_SHA384)
		}
	})

	t.Run("empty input", func(t *testing.T) {
		suites := parseCipherSuites("", log)
		if suites != nil {
			t.Errorf("expected nil, got %v", suites)
		}
	})

	t.Run("unknown names are skipped", func(t *testing.T) {
		suites := parseCipherSuites("TLS_AES_128_GCM_SHA256,UNKNOWN_CIPHER", log)
		if len(suites) != 1 {
			t.Fatalf("expected 1 suite, got %d", len(suites))
		}
	})

	t.Run("TLS 1.2 ECDHE cipher", func(t *testing.T) {
		suites := parseCipherSuites("TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256", log)
		if len(suites) != 1 {
			t.Fatalf("expected 1 suite, got %d", len(suites))
		}
		if suites[0] != tls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256 {
			t.Errorf("suites[0] = %#x, want %#x", suites[0], tls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256)
		}
	})
}

func TestTlsConfigFromEnv_Unset(t *testing.T) {
	t.Setenv("TLS_MIN_VERSION", "")
	t.Setenv("TLS_CIPHER_SUITES", "")

	result := tlsConfigFromEnv()
	if result != nil {
		t.Error("expected nil when env vars are unset")
	}
}

func TestTlsConfigFromEnv_AppliesConfig(t *testing.T) {
	t.Setenv("TLS_MIN_VERSION", "VersionTLS12")
	t.Setenv("TLS_CIPHER_SUITES", "TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256,TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256")

	fn := tlsConfigFromEnv()
	if fn == nil {
		t.Fatal("expected non-nil TLS config function")
	}

	cfg := &tls.Config{}
	fn(cfg)

	if cfg.MinVersion != tls.VersionTLS12 {
		t.Errorf("MinVersion = %d, want %d", cfg.MinVersion, tls.VersionTLS12)
	}
	if len(cfg.CipherSuites) != 2 {
		t.Fatalf("expected 2 cipher suites, got %d", len(cfg.CipherSuites))
	}
}

func TestTlsConfigFromEnv_TLS13_SkipsCipherSuites(t *testing.T) {
	t.Setenv("TLS_MIN_VERSION", "VersionTLS13")
	t.Setenv("TLS_CIPHER_SUITES", "TLS_AES_128_GCM_SHA256,TLS_AES_256_GCM_SHA384")

	fn := tlsConfigFromEnv()
	if fn == nil {
		t.Fatal("expected non-nil TLS config function")
	}

	cfg := &tls.Config{}
	fn(cfg)

	if cfg.MinVersion != tls.VersionTLS13 {
		t.Errorf("MinVersion = %d, want %d", cfg.MinVersion, tls.VersionTLS13)
	}
	if cfg.CipherSuites != nil {
		t.Errorf("CipherSuites should be nil for TLS 1.3, got %v", cfg.CipherSuites)
	}
}
