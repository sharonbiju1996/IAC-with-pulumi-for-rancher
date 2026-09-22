#!/usr/bin/env bash
# Destroys all resources, including the k3d cluster.
source "$(dirname "${BASH_SOURCE[0]}")/common.sh"
cd "$INFRA_DIR"
CLUSTER="$(pulumi config get clusterName --stack "$STACK" 2>/dev/null || echo keycloak)"

log "Destroying stack '$STACK'"
pulumi destroy --yes --stack "$STACK" || warn "pulumi destroy reported errors; falling back to k3d"

if k3d cluster list "$CLUSTER" >/dev/null 2>&1; then
  log "Removing leftover k3d cluster '$CLUSTER'"
  k3d cluster delete "$CLUSTER"
fi

if [[ "${PURGE:-false}" == "true" ]]; then
  log "Purging stack and local state"
  pulumi stack rm "$STACK" --yes --force || true
  rm -rf "$STATE_DIR" "$ROOT_DIR/kubeconfig" "$ROOT_DIR/keycloak-ca.crt"
fi
log "Done"
