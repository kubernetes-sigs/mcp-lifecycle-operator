#!/usr/bin/env bash
# Copyright 2026 The Kubernetes Authors.
#
# Validates the Grafana dashboard JSON and optionally checks that PromQL queries
# return data from a live Prometheus instance (PROMETHEUS_URL).
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
DASHBOARD="${ROOT}/config/grafana/mcp-lifecycle-operator.json"

echo "==> Validating dashboard JSON structure"
python3 - "${DASHBOARD}" <<'PY'
import json, sys
path = sys.argv[1]
with open(path) as f:
    d = json.load(f)
assert d.get("uid") == "mcp-lifecycle-operator", "unexpected uid"
assert d.get("title"), "missing title"
assert d.get("panels"), "missing panels"
print(f"OK: {path} ({len(d['panels'])} top-level panels, uid={d['uid']})")
PY

echo "==> Running Go dashboard unit tests"
(cd "${ROOT}/config/grafana" && go test -v ./...)

if [[ -n "${PROMETHEUS_URL:-}" ]]; then
  echo "==> Querying live Prometheus at ${PROMETHEUS_URL}"
  queries=(
    'mcpserver_condition_info'
    'controller_runtime_reconcile_total{controller="mcpserver"}'
    'controller_runtime_reconcile_time_seconds_bucket{controller="mcpserver"}'
    'process_cpu_seconds_total'
  )
  for q in "${queries[@]}"; do
    encoded="$(python3 -c "import urllib.parse; print(urllib.parse.quote('''${q}'''))")"
    result="$(curl -sf --connect-timeout 5 --max-time 15 "${PROMETHEUS_URL}/api/v1/query?query=${encoded}")"
    count="$(echo "${result}" | python3 -c "import json,sys; d=json.load(sys.stdin); print(len(d.get('data',{}).get('result',[])))")"
    echo "  query=${q} series=${count}"
    if [[ "${count}" -eq 0 ]]; then
      echo "WARN: no series for ${q}" >&2
    fi
  done
else
  echo "==> Skipping live Prometheus checks (set PROMETHEUS_URL to enable)"
fi

echo "==> Dashboard verification complete"
