#!/usr/bin/env bash
set -euo pipefail

SERVICE=${NLI_CONTROLLER_SERVICE:-nli-controller-agent.service}
CONFIG=${NLI_CONTROLLER_CONFIG:-/etc/nli/controller.json}
ENV_FILE=${NLI_CONTROLLER_ENV_FILE:-/etc/nli/controller.env}
BINARY=${NLI_CONTROLLER_BINARY:-/usr/bin/nli-controller-agent}
LOCAL_URL=${NLI_CONTROLLER_LOCAL_URL:-http://127.0.0.1:12382}

fail() { printf 'nli-admin-reset: %s\n' "$*" >&2; exit 1; }
[[ ${EUID:-$(id -u)} -eq 0 ]] || fail 'run as root: sudo nli-admin-reset'
[[ -x $BINARY ]] || fail "$BINARY is missing"
[[ -f $CONFIG ]] || fail "$CONFIG is missing"
[[ -f $ENV_FILE ]] || fail "$ENV_FILE is missing"

printf 'This resets only the existing admin password and preserves every user.\n'
read -r -s -p 'New admin password (minimum 12 characters): ' password
printf '\n'
read -r -s -p 'Confirm new admin password: ' confirmation
printf '\n'
[[ $password == "$confirmation" ]] || fail 'passwords do not match'
[[ $(LC_ALL=C printf '%s' "$password" | wc -c) -ge 12 ]] || fail 'password must contain at least 12 bytes'
[[ $password != *$'\n'* && $password != *$'\r'* ]] || fail 'password cannot contain a newline'

was_active=0
systemctl is-active --quiet "$SERVICE" && was_active=1 || true
restart_on_failure() {
  password=''
  confirmation=''
  if [[ $was_active -eq 1 ]] && ! systemctl is-active --quiet "$SERVICE"; then
    systemctl start "$SERVICE" || true
  fi
}
trap restart_on_failure EXIT

systemctl stop "$SERVICE"
printf '%s\n' "$password" | "$BINARY" --config "$CONFIG" --reset-admin-password-stdin

umask 077
tmp=$(mktemp "${ENV_FILE}.XXXXXX")
seen=0
while IFS= read -r line || [[ -n $line ]]; do
  case "$line" in
    NLI_BOOTSTRAP_ADMIN_PASSWORD=*) printf 'NLI_BOOTSTRAP_ADMIN_PASSWORD=\n' >>"$tmp"; seen=1 ;;
    *) printf '%s\n' "$line" >>"$tmp" ;;
  esac
done <"$ENV_FILE"
[[ $seen -eq 1 ]] || printf 'NLI_BOOTSTRAP_ADMIN_PASSWORD=\n' >>"$tmp"
install -o root -g nli -m 0640 "$tmp" "$ENV_FILE"
rm -f "$tmp"

password=''
confirmation=''
systemctl start "$SERVICE"
for _ in $(seq 1 30); do
  systemctl is-active --quiet "$SERVICE" && curl -fsS "$LOCAL_URL/api/v1/health" >/dev/null && break
  sleep 1
done
systemctl is-active --quiet "$SERVICE" || fail 'controller did not restart after password reset'
curl -fsS "$LOCAL_URL/api/v1/health" >/dev/null || fail 'controller health check failed after password reset'
trap - EXIT
printf 'nli-admin-reset: PASS; sign in as admin with the new password\n'
