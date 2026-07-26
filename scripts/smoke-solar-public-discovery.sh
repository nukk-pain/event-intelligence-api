#!/usr/bin/env bash
# Run the non-gating operator smoke for Solar-backed, curated public discovery.
#
# With no Solar key this is an intentional, successful skip. With a key, the
# command runs in a fresh process group with a bounded wall-clock timeout. Only
# redacted output is written to the temporary report and printed; the key,
# authorization material, and goal are never emitted. The temporary directory
# and any child process are removed by the EXIT/INT/TERM trap.
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

if [[ -z "${EVENTSINTEL_SOLAR_API_KEY:-}" ]]; then
  printf 'result=SKIPPED_CREDENTIAL_UNAVAILABLE\n'
  printf 'detail=EVENTSINTEL_SOLAR_API_KEY is absent; no network or model command was run.\n'
  exit 0
fi

if ! command -v python3 >/dev/null 2>&1; then
  printf 'result=FAIL\nreason=python3 is required for timeout/process cleanup and output redaction.\n'
  exit 1
fi

umask 077
TMP_DIR="$(mktemp -d "${TMPDIR:-/tmp}/eventsintel-solar-public.XXXXXX")"
PY_PID=""

cleanup() {
  local rc=$?
  trap - EXIT INT TERM
  if [[ -n "$PY_PID" ]] && kill -0 "$PY_PID" >/dev/null 2>&1; then
    kill -TERM "$PY_PID" >/dev/null 2>&1 || true
    wait "$PY_PID" >/dev/null 2>&1 || true
  fi
  rm -rf "$TMP_DIR"
  printf 'cleanup=complete\n'
  exit "$rc"
}
trap cleanup EXIT INT TERM

python3 - "$ROOT" "$TMP_DIR/result.txt" <<'PY' &
import os
import json
import re
import signal
import subprocess
import sys
import time


root, result_path = sys.argv[1:]
goal = "한국 AI 로봇 공식 행사 일정 소스"
command = [
    "go",
    "run",
    "./cmd/eventscout",
    "-backend",
    "solar",
    "-search-provider",
    "public",
    "-rounds",
    "1",
    "-max-tokens",
    "1000",
    "-timeout",
    "20s",
    "-goal",
    goal,
]
key = os.environ.get("EVENTSINTEL_SOLAR_API_KEY", "")
child = None


def as_text(value):
    if value is None:
        return ""
    if isinstance(value, bytes):
        return value.decode("utf-8", errors="replace")
    return value


def signal_child(sig):
    if child is None or child.poll() is not None:
        return
    try:
        os.killpg(child.pid, sig)
    except ProcessLookupError:
        pass
    except OSError:
        try:
            child.send_signal(sig)
        except ProcessLookupError:
            pass


def handle_signal(signum, _frame):
    signal_child(signal.SIGTERM)
    raise SystemExit(128 + signum)


signal.signal(signal.SIGINT, handle_signal)
signal.signal(signal.SIGTERM, handle_signal)


def redact(raw):
    cleaned = raw
    if key:
        cleaned = cleaned.replace(key, "[REDACTED_SOLAR_KEY]")
    cleaned = cleaned.replace(goal, "[REDACTED_GOAL]")
    cleaned = re.sub(r"(?im)^.*(?:authorization|api[-_ ]?key|x-api-key).*$", "[REDACTED_CREDENTIAL_LINE]", cleaned)
    cleaned = re.sub(r"(?i)\bBearer\s+\S+", "Bearer [REDACTED]", cleaned)
    cleaned = re.sub(r"(?im)^(\s*goal\s*:\s*).*$", r"\1[REDACTED_GOAL]", cleaned)
    cleaned = re.sub(r'(?i)("goal"\s*:\s*")[^"\r\n]*(")', r"\1[REDACTED_GOAL]\2", cleaned)
    if len(cleaned) > 16000:
        cleaned = cleaned[:8000] + "\n...[sanitized output truncated]...\n" + cleaned[-8000:]
    return cleaned.strip()


TRACE_KEYS = {
    "outcome",
    "terminal_reason",
    "crawler_validated",
    "offered",
    "prefilter_dropped",
    "proposal_calls",
    "judge_calls",
    "judge_entries_parsed",
    "judge_entries_dropped",
    "accepted",
}
OUTCOMES = {"accepted", "error", "budget_stopped", "candidate_empty", "offered_empty", "judge_empty"}
TERMINAL_REASONS = {
    "proposal_done", "proposal_error", "search_error", "candidate_encode_error", "judge_error",
    "malformed_judge_envelope", "model_call_budget", "token_budget", "round_limit",
    "context_canceled", "deadline_exceeded", "invalid_usage", "none",
}


def extract_yield_trace(output):
    match = re.search(r"discovered \d+ source\(s\):\s*", output)
    if match is None:
        return None
    try:
        payload, _ = json.JSONDecoder().raw_decode(output[match.end():].lstrip())
    except (TypeError, ValueError):
        return None
    trace = payload.get("yield_trace") if isinstance(payload, dict) else None
    if not isinstance(trace, dict) or set(trace) != TRACE_KEYS:
        return None
    if trace["outcome"] not in OUTCOMES or trace["terminal_reason"] not in TERMINAL_REASONS:
        return None
    count_keys = TRACE_KEYS - {"outcome", "terminal_reason"}
    if any(type(trace[key]) is not int or trace[key] < 0 for key in count_keys):
        return None
    candidate_counts = {"crawler_validated", "offered", "prefilter_dropped", "accepted"}
    if any(trace[key] > 30 for key in candidate_counts):
        return None
    if trace["judge_entries_parsed"] > 1000 or trace["judge_entries_dropped"] > 1000:
        return None
    if trace["proposal_calls"] > 2 or trace["judge_calls"] > 2:
        return None
    return trace


def write_result(result, exit_code, timed_out, duration, output, trace, reason=""):
    provider_observed = "search=public" in output or '"provider": "public"' in output or '"provider":"public"' in output
    cleaned = redact(output)
    with open(result_path, "w", encoding="utf-8") as report:
        report.write(f"result={result}\n")
        report.write("provider=public\n")
        report.write(f"provider_observed={'true' if provider_observed else 'false'}\n")
        report.write(f"exit_code={exit_code}\n")
        report.write(f"timed_out={'true' if timed_out else 'false'}\n")
        report.write(f"duration_seconds={duration:.1f}\n")
        if trace is not None:
            report.write(f"yield_outcome={trace['outcome']}\n")
            report.write(f"yield_terminal_reason={trace['terminal_reason']}\n")
            for key in sorted(TRACE_KEYS - {"outcome", "terminal_reason"}):
                report.write(f"yield_{key}={trace[key]}\n")
        if reason:
            report.write(f"reason={reason}\n")
        report.write("command=go run ./cmd/eventscout -backend solar -search-provider public -rounds 1 -max-tokens 1000 -timeout 20s -goal [REDACTED_GOAL]\n")
        if cleaned:
            report.write("sanitized_output_begin\n")
            report.write(cleaned)
            report.write("\nsanitized_output_end\n")
    return provider_observed


started = time.monotonic()
raw = ""
timed_out = False
exit_code = 127
reason = ""
try:
    env = os.environ.copy()
    env["EVENTSINTEL_LOCAL_BASE_URL"] = "off"
    env.pop("EVENTSINTEL_TAVILY_API_KEY", None)
    child = subprocess.Popen(
        command,
        cwd=root,
        env=env,
        stdout=subprocess.PIPE,
        stderr=subprocess.STDOUT,
        text=True,
        start_new_session=True,
    )
    timeout_raw = os.environ.get("EVENTSINTEL_SOLAR_SMOKE_TIMEOUT_SECONDS", "90")
    try:
        timeout_seconds = int(timeout_raw)
    except ValueError:
        timeout_seconds = 0
    if timeout_seconds < 1 or timeout_seconds > 120:
        reason = "EVENTSINTEL_SOLAR_SMOKE_TIMEOUT_SECONDS must be an integer from 1 to 120"
        signal_child(signal.SIGTERM)
        try:
            child.wait(timeout=5)
        except subprocess.TimeoutExpired:
            signal_child(signal.SIGKILL)
            child.wait()
        exit_code = 2
    else:
        try:
            raw, _ = child.communicate(timeout=timeout_seconds)
            exit_code = child.returncode
        except subprocess.TimeoutExpired as exc:
            timed_out = True
            raw = as_text(exc.stdout)
            signal_child(signal.SIGTERM)
            try:
                tail, _ = child.communicate(timeout=5)
            except subprocess.TimeoutExpired:
                signal_child(signal.SIGKILL)
                tail, _ = child.communicate()
            raw += as_text(tail)
            exit_code = child.returncode if child.returncode is not None else 124
            reason = f"wall-clock timeout after {timeout_seconds}s"
except OSError as exc:
    reason = f"runner error: {type(exc).__name__}"
    exit_code = 127
finally:
    if child is not None and child.poll() is None:
        signal_child(signal.SIGTERM)
        try:
            child.wait(timeout=5)
        except subprocess.TimeoutExpired:
            signal_child(signal.SIGKILL)
            child.wait()

duration = time.monotonic() - started
provider_observed = "search=public" in raw or '"provider": "public"' in raw or '"provider":"public"' in raw
yield_trace = extract_yield_trace(raw)
result = "PASS" if exit_code == 0 and not timed_out and provider_observed and yield_trace is not None else "FAIL"
if exit_code == 0 and not timed_out and provider_observed and yield_trace is None and not reason:
    reason = "successful command omitted fixed bounded yield trace"
write_result(
    result,
    exit_code,
    timed_out,
    duration,
    raw,
    yield_trace,
    reason,
)
if exit_code == 0 and not timed_out and provider_observed and yield_trace is not None:
    raise SystemExit(0)
raise SystemExit(1)
PY
PY_PID=$!

if wait "$PY_PID"; then
  python_rc=0
else
  python_rc=$?
fi

if [[ ! -s "$TMP_DIR/result.txt" ]]; then
  printf 'result=FAIL\nreason=smoke runner produced no sanitized result.\n'
  exit 1
fi
cat "$TMP_DIR/result.txt"
if [[ "$python_rc" -ne 0 ]]; then
  exit 1
fi
