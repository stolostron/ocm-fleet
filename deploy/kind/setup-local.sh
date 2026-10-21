#!/usr/bin/env bash
set -euo pipefail

deploy_dir=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
umask 077

die() {
  printf '%s\n' "$*" >&2
  exit 1
}

require_command() {
  command -v "$1" >/dev/null 2>&1 || die "$1 is required; install it and rerun this script"
}

random_hex() {
  require_command python3
  python3 -c 'import secrets, sys; print(secrets.token_hex(int(sys.argv[1])))' "$1"
}

bcrypt_hash() {
  local password=$1 hash_line
  require_command htpasswd
  hash_line=$(printf '%s\n' "$password" |
    htpasswd -nBi -C 12 demo 2>/dev/null) || die 'htpasswd could not hash the demo password'
  case "$hash_line" in
    'demo:$2a$12$'*|'demo:$2b$12$'*|'demo:$2y$12$'*)
      printf '%s\n' "${hash_line#demo:}"
      ;;
    *) die 'htpasswd returned an unexpected hash' ;;
  esac
}

atomic_write() {
  local destination=$1 temporary
  temporary=$(mktemp "$destination.tmp.XXXXXX") || return 1
  if cat > "$temporary" && mv "$temporary" "$destination"; then
    return 0
  fi
  rm -f "$temporary"
  return 1
}

if [ ! -f "$deploy_dir/config.env" ]; then
  atomic_write "$deploy_dir/config.env" < "$deploy_dir/config.env.example"
fi

if [ ! -f "$deploy_dir/secrets.env" ]; then
  database_password=$(random_hex 24)
  printf 'POSTGRES_USER=ocm-fleet\nPOSTGRES_PASSWORD=%s\nPOSTGRES_DB=ocm-fleet\nDATABASE_URL=postgres://ocm-fleet:%s@postgres:5432/ocm-fleet?sslmode=disable\n' \
    "$database_password" "$database_password" | atomic_write "$deploy_dir/secrets.env"
fi

credentials_file="$deploy_dir/local-credentials.txt"
dex_secrets_file="$deploy_dir/dex-secrets.env"

if [ -f "$dex_secrets_file" ] && [ ! -f "$credentials_file" ]; then
  die 'Dex credentials exist, but local-credentials.txt is missing; remove dex-secrets.env to issue a new demo password'
fi

if [ ! -f "$credentials_file" ]; then
  demo_password=$(random_hex 18)
  printf 'DEX_DEMO_EMAIL=ops@ocm-fleet.local\nDEX_DEMO_PASSWORD=%s\n' \
    "$demo_password" | atomic_write "$credentials_file"
fi

if [ ! -f "$dex_secrets_file" ]; then
  demo_password=$(sed -n 's/^DEX_DEMO_PASSWORD=//p' "$credentials_file")
  case "$demo_password" in
    ''|*[!0-9a-f]*) die 'local-credentials.txt contains an invalid demo password' ;;
  esac
  demo_hash=$(bcrypt_hash "$demo_password")
  resource_client_secret=$(random_hex 24)
  printf 'DEX_OPERATOR_PASSWORD_HASH=%s\nDEX_RESOURCE_CLIENT_SECRET=%s\n' \
    "$demo_hash" "$resource_client_secret" | atomic_write "$dex_secrets_file"
fi

printf '%s\n' 'Local KinD configuration is ready. Demo login is in deploy/kind/local-credentials.txt.'
