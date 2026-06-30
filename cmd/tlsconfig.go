package main

import (
	"crypto/tls"
	"os"
	"strings"

	ctrl "sigs.k8s.io/controller-runtime"
)

func tlsConfigFromEnv() func(*tls.Config) {
	log := ctrl.Log.WithName("setup")

	minVersionStr := os.Getenv("TLS_MIN_VERSION")
	cipherSuitesStr := os.Getenv("TLS_CIPHER_SUITES")

	if minVersionStr == "" && cipherSuitesStr == "" {
		return nil
	}

	minVersion := parseTLSVersion(minVersionStr)
	if minVersionStr != "" && minVersion == 0 {
		log.Info("Ignoring unknown TLS_MIN_VERSION, falling back to Go defaults", "value", minVersionStr)
	}
	cipherSuites := parseCipherSuites(cipherSuitesStr, log)

	if minVersion >= tls.VersionTLS13 && len(cipherSuites) > 0 {
		log.Info("TLS 1.3 manages cipher suites automatically, configured suites will not be applied")
	}

	log.Info("Applying TLS profile from environment",
		"minVersion", minVersionStr,
		"cipherSuiteCount", len(cipherSuites))

	return func(c *tls.Config) {
		if minVersion > 0 {
			c.MinVersion = minVersion
		}
		// Go manages TLS 1.3 cipher suites automatically
		if minVersion < tls.VersionTLS13 && len(cipherSuites) > 0 {
			c.CipherSuites = cipherSuites
		}
	}
}

func parseTLSVersion(s string) uint16 {
	switch s {
	case "VersionTLS10":
		return tls.VersionTLS10
	case "VersionTLS11":
		return tls.VersionTLS11
	case "VersionTLS12":
		return tls.VersionTLS12
	case "VersionTLS13":
		return tls.VersionTLS13
	default:
		return 0
	}
}

func parseCipherSuites(s string, log interface{ Info(string, ...any) }) []uint16 {
	if s == "" {
		return nil
	}

	lookup := make(map[string]uint16)
	for _, cs := range tls.CipherSuites() {
		lookup[cs.Name] = cs.ID
	}
	for _, cs := range tls.InsecureCipherSuites() {
		lookup[cs.Name] = cs.ID
	}

	names := strings.Split(s, ",")
	suites := make([]uint16, 0, len(names))

	for _, name := range names {
		name = strings.TrimSpace(name)
		if id, ok := lookup[name]; ok {
			suites = append(suites, id)
		} else if name != "" {
			log.Info("Skipping unknown TLS cipher suite", "cipher", name)
		}
	}

	return suites
}
