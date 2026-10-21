#!/usr/bin/env bash
set -euo pipefail

deploy_dir=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
credentials_file="$deploy_dir/local-credentials.txt"
dex_secrets_file="$deploy_dir/dex-secrets.env"

if [ ! -f "$credentials_file" ] || [ ! -f "$dex_secrets_file" ]; then
  printf '%s\n' 'Run ./deploy/kind/setup-local.sh first.' >&2
  exit 1
fi

demo_email=$(sed -n 's/^DEX_DEMO_EMAIL=//p' "$credentials_file")
demo_password=$(sed -n 's/^DEX_DEMO_PASSWORD=//p' "$credentials_file")
resource_client_secret=$(sed -n 's/^DEX_RESOURCE_CLIENT_SECRET=//p' "$dex_secrets_file")

case "$demo_email" in
  ops@ocm-fleet.local) ;;
  *) printf '%s\n' 'Invalid demo email in local-credentials.txt.' >&2; exit 1 ;;
esac
case "$demo_password:$resource_client_secret" in
  *[!0-9a-f:]*|:*|*:) printf '%s\n' 'Invalid local Dex credentials.' >&2; exit 1 ;;
esac

# Both generated secrets are hexadecimal; the other form values are URL encoded.
token=$(
  printf 'user = "resource-manager:%s"\ndata = "grant_type=password&scope=openid%%20profile%%20email&username=ops%%40ocm-fleet.local&password=%s"\nurl = "http://localhost:5556/dex/token"\n' \
    "$resource_client_secret" "$demo_password" |
    curl --fail --silent --show-error --config - |
    python3 -c 'import json, sys; print(json.load(sys.stdin)["access_token"])'
)

# Read curl options from stdin so the bearer token is absent from process args.
printf 'header = "Authorization: Bearer %s"\nurl = "http://localhost:8085/v1alpha/deployments"\n' \
  "$token" | curl --fail --silent --show-error --config -
printf '\n'
