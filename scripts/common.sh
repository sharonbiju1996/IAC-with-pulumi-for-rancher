#!/usr/bin/env bash
# Shared environment: local Pulumi backend (no Pulumi Cloud account needed)
# and a locally generated passphrase used to encrypt secrets in the state.
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
INFRA_DIR="$ROOT_DIR/infra"
STATE_DIR="$ROOT_DIR/.pulumi-state"
STACK="${STACK:-dev}"

mkdir -p "$STATE_DIR"
export PULUMI_BACKEND_URL="${PULUMI_BACKEND_URL:-file://$STATE_DIR}"
export PULUMI_SKIP_UPDATE_CHECK=true

if [[ -z "${PULUMI_CONFIG_PASSPHRASE:-}" && -z "${PULUMI_CONFIG_PASSPHRASE_FILE:-}" ]]; then
  PASSFILE="$STATE_DIR/.passphrase"
  if [[ ! -f "$PASSFILE" ]]; then
    (umask 077; head -c 32 /dev/urandom | base64 > "$PASSFILE")
  fi
  export PULUMI_CONFIG_PASSPHRASE_FILE="$PASSFILE"
fi

log()  { printf '\033[1;34m==>\033[0m %s\n' "$*"; }
warn() { printf '\033[1;33m[warn]\033[0m %s\n' "$*"; }
die()  { printf '\033[1;31m[error]\033[0m %s\n' "$*" >&2; exit 1; }
