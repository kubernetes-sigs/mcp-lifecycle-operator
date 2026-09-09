# TLS configuration

The MCP Lifecycle Operator has two TLS surfaces:

- **Serving endpoints** on the controller manager - the webhook (`9443`) and the HTTPS metrics endpoint (`8443`).
- The **client** connection the operator opens to each managed MCP server to perform its handshake verification.

Both surfaces read the same TLS profile from three environment variables on the operator Deployment: `TLS_MIN_VERSION`, `TLS_CIPHER_SUITES`, and `TLS_GROUPS`. Whatever deploys the operator is responsible for setting them, the same way it sets any other container environment variable.

This page is aimed at **platform and cluster operators** who tune the operator's crypto posture - for example to align with an organization-wide policy, to restrict curves in FIPS environments, or to prefer post-quantum hybrid key exchange.

!!! note
    When the operator is installed by a platform that surfaces a cluster-wide TLS policy (for example an OpenShift `TLSSecurityProfile`), that policy is the source of these values and there is no per-operator override - the deploying component translates the policy into the environment variables below. On a plain Kubernetes install you set them directly on the Deployment.

## Environment variables

| Variable | Applies to | Value |
| --- | --- | --- |
| `TLS_MIN_VERSION` | serving + client | One of `VersionTLS10`, `VersionTLS11`, `VersionTLS12`, `VersionTLS13`. |
| `TLS_CIPHER_SUITES` | serving + client | Comma-separated Go cipher-suite names (for example `TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256`). Only applied below TLS 1.3. |
| `TLS_GROUPS` | serving + client | Comma-separated TLS 1.3 named groups (elliptic curves and hybrid key-exchange groups). See below. |

Unset variables leave Go's defaults in place. Unknown or malformed values are **skipped with a log line** rather than failing startup, and the operator falls back to the default for that setting.

!!! warning "TLS 1.3 manages cipher suites automatically"
    When `TLS_MIN_VERSION=VersionTLS13`, Go selects the TLS 1.3 cipher suites itself and any `TLS_CIPHER_SUITES` value is ignored. Cipher-suite pinning only affects TLS 1.0-1.2.

## TLS 1.3 named groups (`TLS_GROUPS`)

TLS 1.3 decoupled key exchange from cipher suites: the named groups (elliptic curves and key-exchange groups, including the post-quantum hybrid `X25519MLKEM768`) are negotiated separately through the `supported_groups` / `key_share` extensions. `TLS_GROUPS` sets the operator's preference order for those groups; it is applied as `tls.Config.CurvePreferences` on both the serving endpoints and the MCP-server client connection.

Names are matched **case-insensitively** and accept the Go canonical name plus the common IANA / OpenSSL aliases, so a value taken from a platform TLS policy needs no translation. Unrecognized names are skipped, and the accepted names are de-duplicated and re-emitted in canonical form.

| Canonical name | Accepted aliases | Notes |
| --- | --- | --- |
| `X25519` | - | Fast, widely supported. |
| `CurveP256` | `P-256`, `secp256r1` | NIST curve. |
| `CurveP384` | `P-384`, `secp384r1` | NIST curve. |
| `CurveP521` | `P-521`, `secp521r1` | NIST curve. |
| `X25519MLKEM768` | - | Post-quantum hybrid key exchange. See availability below. |

Example - prefer the PQC hybrid, then X25519, then P-256:

```
TLS_GROUPS=X25519MLKEM768,X25519,CurveP256
```

### `X25519MLKEM768` availability

`X25519MLKEM768` is a TLS 1.3-only hybrid that combines X25519 with the ML-KEM-768 post-quantum KEM. Two conditions must hold for it to be negotiated:

- The operator binary must be built with the **Go 1.24** toolchain or newer, where `X25519MLKEM768` was added to the standard library and enabled by default.
- Under a FIPS build that routes crypto through the system OpenSSL provider, ML-KEM availability additionally depends on that provider supporting the group.

If either condition is not met, listing `X25519MLKEM768` is harmless - it is simply not offered, and the remaining groups in the list are used.

### The client-path TLS 1.2 floor

The connection the operator opens to each MCP server is built with a **minimum version of TLS 1.2**. The TLS profile can *raise* that floor (setting `TLS_MIN_VERSION=VersionTLS13` requires TLS 1.3 for these connections) but never *lower* it, so a permissive `TLS_MIN_VERSION` cannot silently downgrade the operator's outbound handshakes below TLS 1.2.

Group preferences and the `X25519MLKEM768` hybrid only take effect once TLS 1.3 is actually negotiated. Because the client keeps the TLS 1.2 floor by default, it can still reach MCP servers that do not speak TLS 1.3 - those connections simply fall back to the classic (non-PQC) curves.

## Use cases

- **FIPS curve restriction** - list only NIST curves (for example `TLS_GROUPS=CurveP256,CurveP384`) to exclude X25519 where policy requires it.
- **Post-quantum readiness** - put `X25519MLKEM768` first so hybrid key exchange is preferred wherever both peers support it, while classic curves remain available as a fallback.

## Propagation to MCP server containers

When the operator runs with `PROPAGATE_TLS_ENV_VARS=true`, the validated (canonicalized, de-duplicated) `TLS_MIN_VERSION`, `TLS_CIPHER_SUITES`, and `TLS_GROUPS` values are injected as environment variables into the managed MCP server containers, so operands that honor the same variables inherit a consistent policy.
