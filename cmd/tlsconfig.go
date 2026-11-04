package main

import (
	"crypto/tls"
	"fmt"
	"maps"
	"os"
	"strings"

	corev1 "k8s.io/api/core/v1"
	ctrl "sigs.k8s.io/controller-runtime"
)

const (
	envTLSMinVersion   = "TLS_MIN_VERSION"
	envTLSCipherSuites = "TLS_CIPHER_SUITES"
	envTLSGroups       = "TLS_GROUPS"
)

// tlsSettings holds validated TLS configuration parsed from the environment.
// Both the tls.Config mutator and the env vars propagated to MCP server
// containers are derived from this single source of truth.
type tlsSettings struct {
	minVersionStr   string        // original env value, empty if unset or invalid
	minVersionCode  uint16        // parsed TLS version constant, 0 if unset or invalid
	cipherSuitesStr string        // comma-separated list of validated cipher suite names
	cipherSuiteIDs  []uint16      // parsed cipher suite IDs for validated names
	groupsStr       string        // comma-separated list of validated, canonicalized group names
	groupIDs        []tls.CurveID // parsed curve/group IDs for validated names
}

// parseTLSSettings reads TLS_MIN_VERSION, TLS_CIPHER_SUITES and TLS_GROUPS
// from the environment, validates them, and returns a tlsSettings with only
// the values that passed validation. A nonempty TLS_GROUPS value must contain
// at least one group recognized by the Go toolchain used to build the operator.
func parseTLSSettings() (tlsSettings, error) {
	log := ctrl.Log.WithName("setup")

	var s tlsSettings

	if raw := os.Getenv(envTLSMinVersion); raw != "" {
		if v := parseTLSVersion(raw); v > 0 {
			s.minVersionStr = raw
			s.minVersionCode = v
		} else {
			log.Info("Ignoring unknown TLS_MIN_VERSION, falling back to Go defaults", "value", raw)
		}
	}

	if raw := os.Getenv(envTLSCipherSuites); raw != "" {
		ids, names := parseCipherSuites(raw, log)
		if len(ids) > 0 {
			s.cipherSuiteIDs = ids
			s.cipherSuitesStr = strings.Join(names, ",")
		}
	}

	if raw := os.Getenv(envTLSGroups); raw != "" {
		ids, names := parseTLSGroups(raw, log)
		if len(ids) == 0 {
			return tlsSettings{}, fmt.Errorf("%s contains no supported TLS groups: %q", envTLSGroups, raw)
		}
		s.groupIDs = ids
		s.groupsStr = strings.Join(names, ",")
	}

	if s.minVersionCode >= tls.VersionTLS13 && len(s.cipherSuiteIDs) > 0 {
		log.Info("TLS 1.3 manages cipher suites automatically, configured suites will not be applied")
	}

	if s.minVersionStr != "" || s.cipherSuitesStr != "" || s.groupsStr != "" {
		log.Info("Applying TLS profile from environment",
			"minVersion", s.minVersionStr,
			"cipherSuiteCount", len(s.cipherSuiteIDs),
			"groupCount", len(s.groupIDs))
	}

	return s, nil
}

// envVars returns the validated TLS settings as Kubernetes EnvVar entries
// suitable for injection into MCP server containers.
func (s tlsSettings) envVars() []corev1.EnvVar {
	var envVars []corev1.EnvVar
	if s.minVersionStr != "" {
		envVars = append(envVars, corev1.EnvVar{Name: envTLSMinVersion, Value: s.minVersionStr})
	}
	if s.cipherSuitesStr != "" {
		envVars = append(envVars, corev1.EnvVar{Name: envTLSCipherSuites, Value: s.cipherSuitesStr})
	}
	if s.groupsStr != "" {
		envVars = append(envVars, corev1.EnvVar{Name: envTLSGroups, Value: s.groupsStr})
	}
	return envVars
}

// tlsConfigFunc returns a function that applies the validated TLS settings to
// a tls.Config, or nil if no valid settings were parsed.
func (s tlsSettings) tlsConfigFunc() func(*tls.Config) {
	if s.minVersionCode == 0 && len(s.cipherSuiteIDs) == 0 && len(s.groupIDs) == 0 {
		return nil
	}
	return func(c *tls.Config) {
		if s.minVersionCode > 0 {
			c.MinVersion = s.minVersionCode
		}
		// Go manages TLS 1.3 cipher suites automatically
		if s.minVersionCode < tls.VersionTLS13 && len(s.cipherSuiteIDs) > 0 {
			c.CipherSuites = s.cipherSuiteIDs
		}
		if len(s.groupIDs) > 0 {
			c.CurvePreferences = s.groupIDs
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

func parseCipherSuites(s string, log interface{ Info(string, ...any) }) ([]uint16, []string) {
	if s == "" {
		return nil, nil
	}

	lookup := make(map[string]uint16)
	for _, cs := range tls.CipherSuites() {
		lookup[cs.Name] = cs.ID
	}
	for _, cs := range tls.InsecureCipherSuites() {
		lookup[cs.Name] = cs.ID
	}

	raw := strings.Split(s, ",")
	suites := make([]uint16, 0, len(raw))
	names := make([]string, 0, len(raw))

	for _, name := range raw {
		name = strings.TrimSpace(name)
		if id, ok := lookup[name]; ok {
			suites = append(suites, id)
			names = append(names, name)
		} else if name != "" {
			log.Info("Skipping unknown TLS cipher suite", "cipher", name)
		}
	}

	return suites, names
}

// tlsGroupLookup derives canonical names from crypto/tls instead of maintaining a
// second list of supported groups. CurveID is a uint16; String returns a Go name
// for known IDs and "CurveID(<number>)" otherwise. Scan the complete range once
// so rebuilding with a newer Go toolchain also recognizes its new named groups.
// Recognition does not override crypto/tls restrictions such as FIPS mode.
var tlsGroupLookup = func() map[string]tls.CurveID {
	lookup := make(map[string]tls.CurveID)
	for id := range 1 << 16 {
		group := tls.CurveID(id)
		name := group.String()
		if !strings.HasPrefix(name, "CurveID(") {
			lookup[strings.ToLower(name)] = group
		}
	}
	// Preserve the common IANA / OpenSSL aliases for the NIST curves.
	maps.Copy(lookup, map[string]tls.CurveID{
		"p-256": tls.CurveP256, "secp256r1": tls.CurveP256,
		"p-384": tls.CurveP384, "secp384r1": tls.CurveP384,
		"p-521": tls.CurveP521, "secp521r1": tls.CurveP521,
	})
	return lookup
}()

// parseTLSGroups parses a comma-separated list of TLS 1.3 named groups
// (elliptic curves and hybrid key-exchange groups) into CurveIDs suitable for
// tls.Config.CurvePreferences. Unknown names are skipped with a log line,
// mirroring parseCipherSuites. Returned names are canonicalized and
// de-duplicated so re-parsing (e.g. in an operand container) is stable.
func parseTLSGroups(s string, log interface{ Info(string, ...any) }) ([]tls.CurveID, []string) {
	if s == "" {
		return nil, nil
	}

	raw := strings.Split(s, ",")
	groups := make([]tls.CurveID, 0, len(raw))
	names := make([]string, 0, len(raw))
	seen := make(map[tls.CurveID]bool, len(raw))

	for _, name := range raw {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		if g, ok := tlsGroupLookup[strings.ToLower(name)]; ok {
			if seen[g] {
				continue
			}
			seen[g] = true
			groups = append(groups, g)
			names = append(names, g.String())
		} else {
			log.Info("Skipping unknown TLS group", "group", name)
		}
	}

	return groups, names
}
