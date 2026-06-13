#!/usr/bin/env bash
set -euo pipefail

DNSDIST_CONFIG_PATH="${DNSDIST_CONFIG_PATH:-/etc/dnsdist/dnsdist.conf}"
DNSDIST_SYNC_CONFIG_PATH="${DNSDIST_SYNC_CONFIG_PATH:-/etc/dnsdist-cert-sync/config.yaml}"
PDNS_RECURSOR_CONFIG_PATH="${PDNS_RECURSOR_CONFIG_PATH:-/etc/powerdns/recursor.conf}"

mkdir -p /var/lib/dns-center /etc/dnsdist /etc/dnsdist-cert-sync /etc/powerdns/recursor.d

if [[ ! -f "$DNSDIST_CONFIG_PATH" ]]; then
  echo "missing dnsdist config: $DNSDIST_CONFIG_PATH" >&2
  exit 1
fi

if [[ ! -f "$DNSDIST_SYNC_CONFIG_PATH" ]]; then
  echo "missing dnsdist-cert-sync config: $DNSDIST_SYNC_CONFIG_PATH" >&2
  exit 1
fi

if [[ ! -f "$PDNS_RECURSOR_CONFIG_PATH" ]]; then
  echo "missing pdns recursor config: $PDNS_RECURSOR_CONFIG_PATH" >&2
  exit 1
fi

pdns_recursor --daemon=no --config-dir=/etc/powerdns --config-name="$(basename "$PDNS_RECURSOR_CONFIG_PATH" .conf)" &
pdns_pid=$!

dnsdist --supervised --disable-syslog --config "$DNSDIST_CONFIG_PATH" &
dnsdist_pid=$!

/usr/local/bin/dnsdist-cert-sync -config "$DNSDIST_SYNC_CONFIG_PATH" &
sync_pid=$!

term() {
  kill -TERM "$sync_pid" "$dnsdist_pid" "$pdns_pid" 2>/dev/null || true
  wait "$sync_pid" "$dnsdist_pid" "$pdns_pid" 2>/dev/null || true
}

trap term INT TERM

wait -n "$sync_pid" "$dnsdist_pid" "$pdns_pid"
status=$?
term
exit "$status"
