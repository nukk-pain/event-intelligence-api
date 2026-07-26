#!/usr/bin/env bash
# Exercise the Solar public-discovery smoke report without a network or real key.
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
SMOKE_SCRIPT="${SMOKE_SCRIPT:-$ROOT/scripts/smoke-solar-public-discovery.sh}"
TEST_DIR="$(mktemp -d "${TMPDIR:-/tmp}/eventsintel-smoke-test.XXXXXX")"
SMOKE_TMP_ROOT="$TEST_DIR/smoke-tmp"
FAKE_BIN="$TEST_DIR/bin"
INVOCATIONS="$TEST_DIR/go-invocations"
PRESENT_REPORT="$TEST_DIR/present-report"
NO_KEY_REPORT="$TEST_DIR/no-key-report"
SOLAR_KEY_ENV_NAME="EVENTSINTEL_SOLAR""_API_KEY"

cleanup() {
  local rc=$?
  trap - EXIT INT TERM
  if ! rm -rf "$TEST_DIR" || [[ -e "$TEST_DIR" ]]; then
    printf 'cleanup=failed\n' >&2
    exit 1
  fi
  printf 'cleanup=complete\n'
  exit "$rc"
}
trap cleanup EXIT INT TERM

fail() {
  printf 'result=FAIL\n'
  printf '%s\n' "$1"
  exit 1
}

assert_no_smoke_temp_dirs() {
  if find "$SMOKE_TMP_ROOT" -mindepth 1 -maxdepth 1 -type d -name 'eventsintel-solar-public.*' -print -quit | grep -q .; then
    fail 'smoke_temp_dirs_residual=true'
  fi
}

mkdir -p "$FAKE_BIN" "$SMOKE_TMP_ROOT"
apply_fake_go() {
  cat > "$FAKE_BIN/go" <<'FAKE_GO'
#!/usr/bin/env bash
set -euo pipefail
printf 'invoked\n' >> "$SMOKE_FAKE_GO_INVOCATIONS"
printf '%s\n' 'source discovery — backend=solar(solar-open2) search=public rounds=1'
printf '%s\n' 'discovered 1 source(s):'
printf '%s\n' '{"sources":[{"title":"SMOKE_PRIVATE_SOURCE_MARKER"}],"provider":"public","candidate":"SMOKE_PRIVATE_CANDIDATE_MARKER","model_reason":"SMOKE_PRIVATE_MODEL_REASON_MARKER","yield_trace":{"outcome":"accepted","terminal_reason":"proposal_done","crawler_validated":2,"offered":1,"prefilter_dropped":1,"prefilter_reasons":{"invalid_url":0,"url_pattern":0,"missing_title":1,"missing_location":0,"missing_date":0,"past_date":0},"proposal_calls":1,"judge_calls":1,"judge_entries_parsed":1,"judge_entries_dropped":0,"accepted":1}}'
FAKE_GO
  chmod 700 "$FAKE_BIN/go"
}

apply_misleading_go() {
  cat > "$FAKE_BIN/go" <<'FAKE_GO'
#!/usr/bin/env bash
set -euo pipefail
printf 'invoked\n' >> "$SMOKE_FAKE_GO_INVOCATIONS"
printf '%s\n' 'misleading-success-signal'
FAKE_GO
  chmod 700 "$FAKE_BIN/go"
}

apply_fake_go

# Given a successful public-provider transcript containing synthetic private data.
# When the present-key smoke is run through a fake local go command.
if ! env "$SOLAR_KEY_ENV_NAME=synthetic-smoke-key" \
  SMOKE_FAKE_GO_INVOCATIONS="$INVOCATIONS" \
  TMPDIR="$SMOKE_TMP_ROOT" \
  PATH="$FAKE_BIN:$PATH" \
  "$SMOKE_SCRIPT" > "$PRESENT_REPORT"; then
  fail 'present_key_fake_cli_result=unexpected_failure'
fi
assert_no_smoke_temp_dirs

# Then no arbitrary child transcript or marker may survive in the report.
if grep -Fq -e 'SMOKE_PRIVATE_SOURCE_MARKER' -e 'SMOKE_PRIVATE_CANDIDATE_MARKER' -e 'SMOKE_PRIVATE_MODEL_REASON_MARKER' "$PRESENT_REPORT"; then
  fail 'privacy_markers_present=true'
fi

if ! grep -Fxq 'result=PASS' "$PRESENT_REPORT" ||
  ! grep -Fxq 'provider=public' "$PRESENT_REPORT" ||
  ! grep -Fxq 'provider_observed=true' "$PRESENT_REPORT" ||
  ! grep -Fxq 'timed_out=false' "$PRESENT_REPORT" ||
  ! grep -Fxq 'yield_outcome=accepted' "$PRESENT_REPORT" ||
  ! grep -Fxq 'yield_terminal_reason=proposal_done' "$PRESENT_REPORT" ||
  ! grep -Eq '^duration_seconds=[0-9]+\.[0-9]$' "$PRESENT_REPORT"; then
  fail 'required_scalar_fields_present=false'
fi

# The per-reason breakdown is the only thing that tells a zero-source run apart
# from a run the model rejected, so its absence must fail the smoke.
for reason_key in invalid_url url_pattern missing_title missing_location missing_date past_date; do
  if ! grep -Eq "^yield_prefilter_reason_${reason_key}=[0-9]+$" "$PRESENT_REPORT"; then
    fail 'prefilter_reason_breakdown_present=false'
  fi
done
if ! grep -Fxq 'yield_prefilter_reason_missing_title=1' "$PRESENT_REPORT"; then
  fail 'prefilter_reason_attribution_correct=false'
fi

allowed_report_line='^(result=PASS|provider=public|provider_observed=true|exit_code=0|timed_out=false|duration_seconds=[0-9]+\.[0-9]|yield_outcome=accepted|yield_terminal_reason=proposal_done|yield_(accepted|crawler_validated|judge_calls|judge_entries_dropped|judge_entries_parsed|offered|prefilter_dropped|proposal_calls)=[0-9]+|yield_prefilter_reason_(invalid_url|url_pattern|missing_title|missing_location|missing_date|past_date)=[0-9]+|command=go run \./cmd/eventscout -backend solar -search-provider public -rounds 1 -max-tokens 1000 -timeout 20s -goal \[REDACTED_GOAL\]|cleanup=complete)$'
if grep -Ev "$allowed_report_line" "$PRESENT_REPORT" >/dev/null; then
  fail 'report_allowlist_only=false'
fi

if [[ "$(awk 'END { print NR }' "$INVOCATIONS")" != "1" ]]; then
  fail 'present_key_fake_cli_invoked=false'
fi

# Given no credential. When the smoke is run again. Then it must skip before go.
if ! env -u "$SOLAR_KEY_ENV_NAME" \
  SMOKE_FAKE_GO_INVOCATIONS="$INVOCATIONS" \
  TMPDIR="$SMOKE_TMP_ROOT" \
  PATH="$FAKE_BIN:$PATH" \
  "$SMOKE_SCRIPT" > "$NO_KEY_REPORT"; then
  fail 'no_key_result=unexpected_failure'
fi
assert_no_smoke_temp_dirs

if ! grep -Fxq 'result=SKIPPED_CREDENTIAL_UNAVAILABLE' "$NO_KEY_REPORT" ||
  [[ "$(awk 'END { print NR }' "$INVOCATIONS")" != "1" ]]; then
  fail 'no_key_fake_cli_invoked=true'
fi

apply_misleading_go
if env "$SOLAR_KEY_ENV_NAME=synthetic-smoke-key" \
  SMOKE_FAKE_GO_INVOCATIONS="$INVOCATIONS" \
  TMPDIR="$SMOKE_TMP_ROOT" \
  PATH="$FAKE_BIN:$PATH" \
  "$SMOKE_SCRIPT" > "$PRESENT_REPORT"; then
  fail 'misleading_exit_zero_rejected=false'
fi
assert_no_smoke_temp_dirs

if ! grep -Fxq 'result=FAIL' "$PRESENT_REPORT" ||
  ! grep -Fxq 'provider_observed=false' "$PRESENT_REPORT" ||
  grep -Fq 'misleading-success-signal' "$PRESENT_REPORT" ||
  [[ "$(awk 'END { print NR }' "$INVOCATIONS")" != "2" ]]; then
  fail 'misleading_exit_zero_rejected=false'
fi

printf 'result=PASS\n'
printf 'privacy_markers_present=false\n'
printf 'required_scalar_fields_present=true\n'
printf 'report_allowlist_only=true\n'
printf 'present_key_fake_cli_invoked=true\n'
printf 'no_key_fake_cli_invoked=false\n'
printf 'misleading_exit_zero_rejected=true\n'
printf 'smoke_temp_dirs_residual=false\n'
