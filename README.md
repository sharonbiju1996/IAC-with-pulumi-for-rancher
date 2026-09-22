# Keycloak on local Kubernetes — Pulumi (Go) + Rancher k3d

A fully automated, reproducible setup that provisions a local **Rancher k3s** cluster (via **k3d**) and deploys a **hardened Keycloak** backed by PostgreSQL, using **Pulumi with Go** as the single Infrastructure-as-Code tool — including the cluster itself.

One command brings everything up:

```bash
make up        # or: ./scripts/setup.sh
```

---

## Table of contents
1. [Architecture](#architecture)
2. [Prerequisites](#prerequisites)
3. [Quick start](#quick-start)
4. [Keycloak credentials](#keycloak-credentials)
5. [Trusting the certificate](#trusting-the-certificate)
6. [Security / hardening](#security--hardening)
7. [Configuration](#configuration)
8. [Verification](#verification)
9. [Teardown](#teardown)
10. [Repository layout](#repository-layout)
11. [Assumptions](#assumptions)
12. [Troubleshooting](#troubleshooting)
13. [Possible improvements](#possible-improvements)
14. [Time spent](#time-spent)

---

## Architecture

```
 Host (your machine)
 ─────────────────────────────────────────────────────────────────────
  Browser ──HTTPS──▶ 127.0.0.1:8443          kubectl ──▶ 127.0.0.1:6550
                          │                                  │
 ┌────────────────────────┼── k3d (Docker) ──────────────────┼────────┐
 │  k3d load balancer :443│                         k3s API server    │
 │                        ▼                                           │
 │  k3s ServiceLB ─▶ Service "keycloak" (LoadBalancer, 443 only)      │
 │                        │                                           │
 │   namespace: keycloak  │  PodSecurity=restricted, default-deny     │
 │   ┌────────────────────▼───────┐   5432/tcp   ┌─────────────────┐  │
 │   │ Keycloak (start, prod mode)│ ───────────▶ │ PostgreSQL 16   │  │
 │   │ :8443 HTTPS  :9000 mgmt    │  (NetPol)    │ StatefulSet+PVC │  │
 │   └────────────────────────────┘              └─────────────────┘  │
 └────────────────────────────────────────────────────────────────────┘
```

Everything is created by one Pulumi Go program (`infra/`), in this order:

| Step | Resource | Pulumi provider |
|---|---|---|
| 1 | k3d cluster (k3s, Traefik disabled, API + HTTPS bound to localhost) | `pulumi-command` |
| 2 | Kubernetes provider bound to that cluster's kubeconfig | `pulumi-kubernetes` |
| 3 | Private CA + Keycloak server certificate | `pulumi-tls` |
| 4 | Random DB and admin passwords | `pulumi-random` |
| 5 | Namespace (PSS restricted), Secrets, ServiceAccounts, NetworkPolicies | `pulumi-kubernetes` |
| 6 | PostgreSQL StatefulSet + ClusterIP Service | `pulumi-kubernetes` |
| 7 | Keycloak Deployment + LoadBalancer Service | `pulumi-kubernetes` |

**Why no Ingress controller?** Keycloak terminates TLS itself and the Service exposes only port 443. This gives true end-to-end encryption (no plaintext hop between an ingress and Keycloak) and removes a component from the attack surface. k3s's built-in Traefik is therefore disabled.

---

## Prerequisites

| Tool | Tested version | Install |
|---|---|---|
| Docker (Desktop or Engine) | 24+ | https://docs.docker.com/get-docker/ |
| k3d (Rancher) | 5.6+ | `curl -s https://raw.githubusercontent.com/k3d-io/k3d/main/install.sh \| bash` or `brew install k3d` |
| Pulumi CLI | 3.x | `curl -fsSL https://get.pulumi.com \| sh` or `brew install pulumi` |
| Go | 1.22+ | https://go.dev/dl/ |
| kubectl *(optional)* | 1.29+ | only for inspection |
| curl, make | any | usually preinstalled |

Resources: ~2 CPU cores and ~3 GB RAM free for Docker. Host ports **8443** and **6550** must be free (both configurable).

**No Pulumi Cloud account is needed** — the scripts use a local file backend (`.pulumi-state/`) and generate a random passphrase to encrypt secrets in the state.

Tested on Linux and macOS. On Windows, run inside WSL2.

---

## Quick start

```bash
git clone https://github.com/<your-user>/keycloak-k8s-pulumi.git
cd keycloak-k8s-pulumi
make up
```

What `make up` does:
1. Checks prerequisites and that Docker is running.
2. Resolves Go dependencies (`go mod tidy`).
3. Creates/selects the Pulumi stack `dev` on the local backend.
4. Runs `pulumi up`: creates the cluster, certificates, secrets, Postgres and Keycloak, waiting for each to become ready.
5. Writes `keycloak-ca.crt` and `kubeconfig` to the repo root (both git-ignored).
6. Runs an HTTPS smoke test against the OIDC discovery endpoint, verifying the certificate.
7. Prints the URL and admin credentials.

First run takes roughly 3–6 minutes (image pulls). Re-running is idempotent.

Open: **https://keycloak.localtest.me:8443/admin/**

> `localtest.me` is a public DNS name whose subdomains all resolve to `127.0.0.1`, so no `/etc/hosts` edits are needed. See [Troubleshooting](#troubleshooting) if your DNS blocks it.

---

## Keycloak credentials

| Field | Value |
|---|---|
| Admin console | https://keycloak.localtest.me:8443/admin/ |
| Realm | `master` |
| Username | `admin` |
| Password | Generated randomly at deploy time — printed at the end of `make up`, and at any time with `make credentials` |

The password is intentionally **not** hard-coded in this repository: committing credentials would contradict the security requirements. To use a password of your choice instead:

```bash
KEYCLOAK_ADMIN_PASSWORD='Choose-A-Strong-One-123!' make up
```

It is stored as an encrypted Pulumi secret and in the Kubernetes Secret `keycloak/keycloak-admin`.

> Keycloak 26 labels the bootstrap admin as *temporary* and shows a banner recommending a permanent admin. For a long-lived instance, create a permanent admin user in the console (or through Terraform/Pulumi Keycloak providers) and delete the bootstrap one.

---

## Trusting the certificate

The certificate is signed by a private CA generated by Pulumi, so browsers will warn until you trust it. The CA is exported to `keycloak-ca.crt`.

- **macOS:** `sudo security add-trusted-cert -d -r trustRoot -k /Library/Keychains/System.keychain keycloak-ca.crt`
- **Ubuntu/Debian:** `sudo cp keycloak-ca.crt /usr/local/share/ca-certificates/keycloak-local.crt && sudo update-ca-certificates`
- **Firefox:** Settings → Certificates → Authorities → Import
- **CLI only:** `curl --cacert keycloak-ca.crt https://keycloak.localtest.me:8443/...`

Remove it again after the review if you prefer.

---

## Security / hardening

**Encryption**
- HTTPS only — Keycloak's HTTP listener is disabled (`KC_HTTP_ENABLED=false`); TLS 1.3/1.2 only.
- End-to-end TLS: the certificate is served by Keycloak itself, no plaintext hop.
- ECDSA P-256 keys; server cert valid 1 year with SANs for the hostname, `localhost`, `127.0.0.1` and in-cluster DNS names.
- Health/metrics endpoints (management port 9000) also use TLS and are **not** exposed by the Service.

**Minimal network exposure**
- Host exposure: exactly two ports, both bound to **127.0.0.1** — `8443` (Keycloak) and `6550` (Kubernetes API). Nothing is reachable from the LAN.
- No HTTP port published at all; Traefik disabled.
- PostgreSQL is `ClusterIP` only.
- **NetworkPolicies** (enforced by k3s's embedded controller):
  - default deny all ingress and egress in the namespace
  - DNS egress to CoreDNS only
  - Keycloak ingress on 8443/9000 only; egress only to Postgres:5432
  - Postgres ingress only from Keycloak pods on 5432; no egress

**Workload hardening**
- Namespace enforces the **Pod Security Standard `restricted`** profile.
- All containers: non-root UID, `allowPrivilegeEscalation: false`, all Linux capabilities dropped, `seccompProfile: RuntimeDefault`.
- Postgres runs with a read-only root filesystem.
- Dedicated ServiceAccounts with `automountServiceAccountToken: false` (no Kubernetes API tokens in pods).
- `enableServiceLinks: false` to avoid leaking service env vars.
- CPU/memory requests and limits on every container; startup/readiness/liveness probes.
- TLS key mounted read-only with mode `0440`.

**Secrets management**
- No credentials in git. DB and admin passwords come from `pulumi-random`.
- Pulumi state secrets are encrypted with a locally generated passphrase (`.pulumi-state/.passphrase`, mode 600, git-ignored).
- Kubeconfig treated as a Pulumi secret.

**Production-mode Keycloak**
- Runs `kc.sh start` (not `start-dev`), strict hostname, PostgreSQL instead of the embedded dev database.

---

## Configuration

All settings have defaults; override with `pulumi config set <key> <value>` from `infra/` (with the env from `scripts/common.sh`), then `make up`.

| Key | Default | Description |
|---|---|---|
| `clusterName` | `keycloak` | k3d cluster name |
| `k3sImage` | `rancher/k3s:v1.31.5-k3s1` | k3s node image (pinned) |
| `apiPort` | `6550` | Host port for the Kubernetes API (localhost only) |
| `httpsPort` | `8443` | Host port for Keycloak HTTPS (localhost only) |
| `hostname` | `keycloak.localtest.me` | Public hostname Keycloak advertises |
| `namespace` | `keycloak` | Kubernetes namespace |
| `keycloakImage` | `quay.io/keycloak/keycloak:26.3` | Keycloak image |
| `postgresImage` | `postgres:16-alpine` | PostgreSQL image |
| `adminUsername` | `admin` | Bootstrap admin username |
| `adminPassword` *(secret)* | random | Bootstrap admin password |

Stack outputs: `keycloakUrl`, `adminConsoleUrl`, `adminUsername`, `adminPassword` (secret), `caCertificate`, `kubeconfig` (secret).

---

## Verification

```bash
make status
# Pods Running/Ready, Service keycloak type LoadBalancer on 443, 4 NetworkPolicies

# TLS + OIDC discovery with certificate verification
curl --cacert keycloak-ca.crt https://keycloak.localtest.me:8443/realms/master/.well-known/openid-configuration

# Obtain an admin token (proves the admin account works)
curl --cacert keycloak-ca.crt -s \
  -d client_id=admin-cli -d grant_type=password \
  -d username=admin -d "password=$(cd infra && ../scripts/credentials.sh | awk '/Password/{print $3}')" \
  https://keycloak.localtest.me:8443/realms/master/protocol/openid-connect/token | head -c 80; echo

# HTTP is not served
curl -v http://127.0.0.1:8443 2>&1 | tail -n 3      # fails: TLS-only port

# Postgres is unreachable from other pods (NetworkPolicy)
kubectl --kubeconfig kubeconfig -n keycloak run np-test --rm -it --restart=Never \
  --image=busybox:1.36 --overrides='{"spec":{"securityContext":{"runAsNonRoot":true,"runAsUser":65534,"seccompProfile":{"type":"RuntimeDefault"}},"containers":[{"name":"np-test","image":"busybox:1.36","command":["nc","-zvw3","postgres","5432"],"securityContext":{"allowPrivilegeEscalation":false,"capabilities":{"drop":["ALL"]}}}]}}'
# → times out
```

---

## Teardown

```bash
make down     # destroys Keycloak, Postgres and the k3d cluster
make purge    # same, plus deletes local Pulumi state, kubeconfig and CA file
```

---

## Repository layout

```
.
├── Makefile                 # up / down / purge / credentials / status
├── scripts/
│   ├── common.sh            # local Pulumi backend + passphrase handling
│   ├── setup.sh             # prerequisite checks, pulumi up, smoke test
│   ├── credentials.sh       # print URL + admin credentials
│   └── teardown.sh          # pulumi destroy (+ k3d fallback cleanup)
└── infra/                   # Pulumi program (Go)
    ├── Pulumi.yaml
    ├── main.go              # wiring + stack outputs
    ├── config.go            # config with defaults
    ├── cluster.go           # k3d/k3s cluster via pulumi-command
    ├── tls.go               # private CA + server cert via pulumi-tls
    ├── common.go            # namespace, secrets, SAs, security helpers
    ├── network.go           # NetworkPolicies
    ├── postgres.go          # PostgreSQL StatefulSet + Service
    └── keycloak.go          # Keycloak Deployment + Service
```

---

## Assumptions

- Local, single-node evaluation environment; not a production HA topology.
- "Rancher preferred" is satisfied with **k3s via k3d** (both Rancher projects), which runs on any OS with Docker. Rancher Desktop also works: it provides Docker and k3s — just keep using k3d, or point the Kubernetes provider at its kubeconfig.
- A self-signed private CA is acceptable for local HTTPS (no public domain is available for Let's Encrypt).
- `keycloak.localtest.me` resolves to 127.0.0.1 via public DNS.
- Keycloak runs one replica with a local cache; clustering (Infinispan/JGroups) is out of scope.
- The master realm with a bootstrap `admin` user fulfils "an admin account for administrative access".

---

## Troubleshooting

| Symptom | Fix |
|---|---|
| `keycloak.localtest.me` doesn't resolve (DNS-rebinding protection on some routers) | Add `127.0.0.1 keycloak.localtest.me` to `/etc/hosts` |
| Port 8443 or 6550 in use | `cd infra && pulumi config set httpsPort 9443` then `make up` (run with env from `scripts/common.sh`) |
| Keycloak pod restarting / slow start | Give Docker ≥3 GB RAM; watch `kubectl --kubeconfig kubeconfig -n keycloak logs deploy/keycloak -f` |
| `pulumi up` stuck waiting for Service | `kubectl --kubeconfig kubeconfig -n kube-system get pods` — `svclb-keycloak-*` must be Running |
| State out of sync after manual cluster deletion | `make purge && make up` |

---

## Possible improvements

- cert-manager with automatic rotation, or a real ACME certificate for a public domain.
- Keycloak Operator / multiple replicas with Infinispan clustering and a PodDisruptionBudget.
- CloudNativePG operator for HA Postgres with backups.
- Declarative realm/client/user configuration with the Pulumi Keycloak provider.
- Pre-built optimized Keycloak image (`kc.sh build`) to enable `readOnlyRootFilesystem` and faster startup.
- External secret store (Vault / SOPS) and image signature verification.
- CI pipeline running `go vet`, `pulumi preview`, and an ephemeral k3d end-to-end test.

---

## Time spent

**Total: _X hours_** — _(fill in honestly)_
# IAC-with-pulumi-for-rancher
