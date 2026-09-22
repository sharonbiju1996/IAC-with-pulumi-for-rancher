#!/usr/bin/env bash
# One-shot, idempotent setup: cluster + Keycloak. Safe to re-run.
source "$(dirname "${BASH_SOURCE[0]}")/common.sh"

log "Checking prerequisites"
for bin in docker k3d pulumi go; do
  command -v "$bin" >/dev/null 2>&1 || die "'$bin' not found. See README → Prerequisites."
done
docker info >/dev/null 2>&1 || die "Docker daemon is not running."

cd "$INFRA_DIR"

log "Resolving Go dependencies"
go mod tidy

log "Selecting Pulumi stack '$STACK' (backend: $PULUMI_BACKEND_URL)"
pulumi stack select "$STACK" --create >/dev/null

if [[ -n "${KEYCLOAK_ADMIN_PASSWORD:-}" ]]; then
  log "Using admin password from KEYCLOAK_ADMIN_PASSWORD"
  pulumi config set --secret adminPassword "$KEYCLOAK_ADMIN_PASSWORD" >/dev/null
fi

log "Provisioning cluster and deploying Keycloak (takes ~3-6 min on first run)"
pulumi up --yes --stack "$STACK"

pulumi stack output caCertificate --stack "$STACK" > "$ROOT_DIR/keycloak-ca.crt"
pulumi stack output kubeconfig --show-secrets --stack "$STACK" > "$ROOT_DIR/kubeconfig"
chmod 600 "$ROOT_DIR/kubeconfig"

URL="$(pulumi stack output keycloakUrl --stack "$STACK")"
USER_="$(pulumi stack output adminUsername --stack "$STACK")"
PASS="$(pulumi stack output adminPassword --show-secrets --stack "$STACK")"

log "Smoke test: OIDC discovery over verified TLS"
for i in $(seq 1 30); do
  if curl -fsS --cacert "$ROOT_DIR/keycloak-ca.crt" "$URL/realms/master/.well-known/openid-configuration" >/dev/null 2>&1; then
    log "Keycloak is up and serving HTTPS"; break
  fi
  [[ $i -eq 30 ]] && warn "Smoke test did not pass yet; check: kubectl --kubeconfig kubeconfig -n keycloak get pods"
  sleep 5
done

cat <<INFO

  Keycloak admin console : $URL/admin/
  Username               : $USER_
  Password               : $PASS
  CA certificate         : $ROOT_DIR/keycloak-ca.crt  (import to avoid browser warnings)
  Kubeconfig             : $ROOT_DIR/kubeconfig

  Show credentials again : make credentials
  Tear everything down   : make down

INFO
