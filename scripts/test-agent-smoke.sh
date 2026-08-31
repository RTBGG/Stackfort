#!/usr/bin/env bash
# SPDX-License-Identifier: AGPL-3.0-or-later

set -euo pipefail

if [[ "$(uname -s)" != "Linux" ]]; then
  printf 'The agent smoke test requires Linux.\n' >&2
  exit 1
fi

repository_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
temporary_root="$(mktemp -d)"
agent_pid=""

cleanup() {
  if [[ -n "$agent_pid" ]] && kill -0 "$agent_pid" 2>/dev/null; then
    kill "$agent_pid"
    wait "$agent_pid" 2>/dev/null || true
  fi
  rm -rf -- "$temporary_root"
}
trap cleanup EXIT

export GOTOOLCHAIN=local
socket_path="$temporary_root/agent.sock"
binary_path="$temporary_root/stackfort-agent"
log_path="$temporary_root/agent.log"

cd "$repository_root"
go build -trimpath -ldflags "-X main.configuredSocketPath=$socket_path -X main.configuredControlAPIUID=$(id -u) -X main.configuredControlAPIGID=$(id -g)" \
  -o "$binary_path" ./cmd/stackfort-agent
"$binary_path" >"$log_path" 2>&1 &
agent_pid=$!

for _ in {1..100}; do
  if [[ -S "$socket_path" ]]; then
    break
  fi
  if ! kill -0 "$agent_pid" 2>/dev/null; then
    cat "$log_path" >&2
    exit 1
  fi
  sleep 0.05
done

if [[ ! -S "$socket_path" ]]; then
  printf 'Agent did not create its Unix socket.\n' >&2
  cat "$log_path" >&2
  exit 1
fi

mode="$(stat -c '%a' "$socket_path")"
if [[ "$mode" != "660" ]]; then
  printf 'Agent socket mode is %s; expected 660.\n' "$mode" >&2
  exit 1
fi

response="$(curl --fail --silent --show-error --unix-socket "$socket_path" http://localhost/v1/health)"
if [[ "$response" != *'"service":"stackfort-agent"'* || "$response" != *'"status":"ok"'* ]]; then
  printf 'Unexpected agent health response: %s\n' "$response" >&2
  exit 1
fi

handshake_request='{"protocolVersion":1,"requestId":"smoke-request-1","idempotencyKey":"smoke-handshake-1","operation":"protocol.handshake","handshake":{"minimumVersion":1,"maximumVersion":1,"clientBuild":{"version":"dev","commit":"unknown","buildDate":"unknown"}}}'
handshake_response="$(curl --fail --silent --show-error \
  --unix-socket "$socket_path" \
  --header 'Content-Type: application/json' \
  --data "$handshake_request" \
  http://localhost/rpc/v1)"
if [[ "$handshake_response" != *'"selectedVersion":1'* || "$handshake_response" != *'"protocol.handshake"'* ]]; then
  printf 'Unexpected agent handshake response: %s\n' "$handshake_response" >&2
  exit 1
fi

capability_request='{"protocolVersion":1,"requestId":"smoke-request-2","idempotencyKey":"smoke-capabilities-1","operation":"host.capabilities.inspect","inspectCapabilities":{}}'
capability_response="$(curl --fail --silent --show-error \
  --unix-socket "$socket_path" \
  --header 'Content-Type: application/json' \
  --data "$capability_request" \
  http://localhost/rpc/v1)"
if [[ "$capability_response" != *'"distributionId"'* ||
      "$capability_response" != *'"projectQuota"'* ||
      "$capability_response" != *'"packages"'* ||
      "$capability_response" != *'"services"'* ]]; then
  printf 'Unexpected agent capability response: %s\n' "$capability_response" >&2
  exit 1
fi

printf 'Agent Unix-socket peer, protocol, and capability smoke test passed.\n'
