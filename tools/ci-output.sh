#!/usr/bin/env bash
# GitHub Actions custom shell: capture complete output without buffering it in
# memory. Keep the log on disk while running, including if the runner stops us.
set -euo pipefail

mkdir -p "${RUNNER_TEMP:?}/ci-logs"
log=$(mktemp "$RUNNER_TEMP/ci-logs/step-XXXXXXXXXXXX")

# Inline failed task output, including nested Nx commands, while collapsing
# passing tasks. Summary output would hide diagnostics in runner-local files.
export NX_DEFAULT_OUTPUT_STYLE=static-failures-only
export NX_TUI=false

status=0
bash --noprofile --norc -e -o pipefail "$1" >"$log" 2>&1 || status=$?
if [ "$status" -eq 0 ]; then
  rm -- "$log"
  exit 0
fi

name=$(basename "$log")
printf '::error::Command failed (exit %s). Complete output below; download ci-logs artifacts for the original log (%s).\n' "$status" "$name"
if [ -n "${GITHUB_STEP_SUMMARY:-}" ]; then
  printf '\nCommand failed (exit %s). See the failed step for complete output or download the **ci-logs** artifact (`%s`).\n' "$status" "$name" >> "$GITHUB_STEP_SUMMARY"
fi

printf '::group::Complete failure output (%s)\n' "$name"
# Logs are data: tools may print workflow commands or close groups themselves.
# A fresh unpredictable token prevents replay from executing those commands.
token="replay-$(basename "$log")"
printf '::stop-commands::%s\n' "$token"
cat -- "$log"
printf '\n::%s::\n' "$token"
printf '::endgroup::\n'
exit "$status"
