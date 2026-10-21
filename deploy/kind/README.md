# Deploy OCM Fleet on KinD

This runs the OCM Fleet server, console, PostgreSQL, and Dex inside a local
KinD cluster. The REST API, console, gRPC API, and login are available on your
host at `localhost`.

## Prerequisites

Install `kind`, `kubectl`, Podman, Bash, `curl`, Python 3, and
`htpasswd` (`httpd-tools` on Fedora/RHEL, `apache2-utils` on Debian/Ubuntu).
Run the commands below in a terminal on the host, from the repository root.
Ports 8085, 5556, and 50051 must be free.

## Build and deploy

```sh
./deploy/kind/setup-local.sh

KIND_EXPERIMENTAL_PROVIDER=podman kind create cluster \
  --config deploy/kind/kind-cluster.yaml \
  --kubeconfig deploy/kind/kubeconfig --wait 300s

podman build -t localhost/ocm-fleet-server:kind .
podman build --ulimit nofile=65536:65536 -f Dockerfile.web \
  -t localhost/ocm-fleet-web:kind .

KIND_EXPERIMENTAL_PROVIDER=podman kind load docker-image \
  localhost/ocm-fleet-server:kind --name ocm-fleet
KIND_EXPERIMENTAL_PROVIDER=podman kind load docker-image \
  localhost/ocm-fleet-web:kind --name ocm-fleet

kubectl --kubeconfig deploy/kind/kubeconfig --context kind-ocm-fleet \
  apply -k deploy/kind
kubectl --kubeconfig deploy/kind/kubeconfig --context kind-ocm-fleet \
  -n ocm-fleet rollout status statefulset/postgres --timeout=300s
kubectl --kubeconfig deploy/kind/kubeconfig --context kind-ocm-fleet \
  -n ocm-fleet rollout status deployment/resource-manager --timeout=300s
```

`setup-local.sh` creates the local config, database and Dex secrets, and a
random demo password. It keeps existing files when run again. The generated
files and dedicated kubeconfig are Git-ignored.

## Sign in and use the API

Read the demo login, then open **http://localhost:8085/app/**:

```sh
cat deploy/kind/local-credentials.txt
curl --fail --show-error http://localhost:8085/readyz
curl --fail --show-error \
  http://localhost:5556/dex/.well-known/openid-configuration
./deploy/kind/test-api.sh
```

`test-api.sh` signs in as the demo user and lists deployments through the
authenticated REST API. It reads ignored local credentials and prints only the
API response. To use the CLI, build `fleetctl` as described in the
[CLI guide](../../client/cli/README.md), then run:

```sh
export FLEETCTL_SERVER=http://localhost:8085
fleetctl auth setup \
  --issuer-url http://localhost:5556/dex \
  --client-id fleetctl \
  --key-enrollment-client-id resource-manager-signing \
  --scopes 'openid,profile,email,audience:server:client_id:resource-manager'
fleetctl auth login
fleetctl auth inspect-token
fleetctl deployment list
```

Use the email and password from `local-credentials.txt` when Dex opens in your
browser. The inspected token should include `resource-manager` in its audience.

| Endpoint | Purpose |
| --- | --- |
| `http://localhost:8085/v1alpha/` | Core REST API |
| `http://localhost:8085/apis/` | Platform and extension REST APIs |
| `http://localhost:8085/readyz` | Server readiness |
| `http://localhost:5556/dex/` | Local OIDC provider |
| `localhost:50051` | gRPC API |

The host ports bind to loopback. PostgreSQL and Dex use KinD's default storage
class, so their data survives Pod restarts. Deleting the cluster deletes the
volumes. Keep `secrets.env` with the PostgreSQL volume.

## Remote targets

The demo account is enough to sign in and use the local API. For signed
deliveries to remote targets, set up an HTTPS OIDC issuer that target agents can
reach and configure a matching signing key registry in `config.env`. Do this
before the first deployment: the server stores its initial OIDC settings in
PostgreSQL. Register `http://localhost:8085/app/auth/callback` and
`http://localhost:8085/app/silent-renew.html` as console redirect URLs.
