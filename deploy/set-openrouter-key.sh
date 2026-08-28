#!/usr/bin/env bash
# Store an OpenRouter API key as a host-encrypted systemd credential.
# This script deliberately does not configure or switch the runtime provider.
set -euo pipefail

credential_dir="/etc/credstore.encrypted"
credential_name="eventsintel-openrouter-api-key"
credential_file="$credential_dir/$credential_name"

usage() {
  cat <<'EOF'
Usage: set-openrouter-key.sh [--replace|--check|--help]

With no option, securely prompt for an OpenRouter API key and store it as a
host-encrypted systemd credential. Existing credentials are not overwritten.

  --replace  Deliberately replace an existing encrypted credential.
  --check    Verify that the stored credential can be decrypted; print no key.
  --help     Show this help.

This stores the key only. Direct Upstage Solar remains the active provider.
EOF
}

die() {
  printf 'set-openrouter-key: %s\n' "$1" >&2
  exit 1
}

mode="store"
case "${1:-}" in
  "") ;;
  --replace) mode="replace" ;;
  --check) mode="check" ;;
  --help|-h)
    usage
    exit 0
    ;;
  *)
    printf 'set-openrouter-key: unknown option: %s\n' "$1" >&2
    usage >&2
    exit 2
    ;;
esac
if [[ $# -gt 1 ]]; then
  printf 'set-openrouter-key: expected at most one option\n' >&2
  usage >&2
  exit 2
fi

[[ "$(id -u)" == "0" ]] || die "run this script as root (for example, enter the VPS and use sudo)"
command -v systemd-creds >/dev/null || die "systemd-creds is required"

if [[ "$mode" == "check" ]]; then
  [[ -f "$credential_file" ]] || die "no encrypted OpenRouter credential is stored"
  if systemd-creds decrypt "$credential_file" - >/dev/null; then
    printf 'OpenRouter credential is present and decrypts successfully; key was not printed.\n'
    printf 'Direct Upstage Solar remains the active provider.\n'
    exit 0
  fi
  die "the encrypted OpenRouter credential could not be decrypted"
fi

[[ -t 0 && -t 1 ]] || die "an interactive terminal is required; do not pipe or pass the key on the command line"
command -v systemd-ask-password >/dev/null || die "systemd-ask-password is required"

if [[ "$mode" == "store" && -e "$credential_file" ]]; then
  die "credential already exists; rerun with --replace only if replacement is intended"
fi

install -d -o root -g root -m 0700 "$credential_dir"
systemd-creds setup >/dev/null

encrypted_tmp="$(mktemp "$credential_dir/.${credential_name}.XXXXXX")"
cleanup() {
  if [[ -n "${encrypted_tmp:-}" ]]; then
    rm -f -- "$encrypted_tmp"
  fi
}
trap cleanup EXIT

systemd-ask-password --echo=masked -n 'OpenRouter API key:' |
  systemd-creds encrypt --name="$credential_name" --with-key=host - "$encrypted_tmp"

chown root:root "$encrypted_tmp"
chmod 0600 "$encrypted_tmp"
systemd-creds decrypt --name="$credential_name" "$encrypted_tmp" - >/dev/null

if [[ "$mode" == "replace" ]]; then
  mv -f -- "$encrypted_tmp" "$credential_file"
else
  # A hard link makes the no-overwrite install atomic in the same root-only dir.
  ln -- "$encrypted_tmp" "$credential_file"
  rm -f -- "$encrypted_tmp"
fi
encrypted_tmp=""
trap - EXIT

printf 'OpenRouter key stored as an encrypted systemd credential at %s.\n' "$credential_file"
printf 'Direct Upstage Solar remains the active provider; no provider switch was performed.\n'
