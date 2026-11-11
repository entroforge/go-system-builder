#!/usr/bin/env bash
# Full verification job driver. Each invocation owns its Harness/catalog;
# run from the checked-out repository root on a Linux CI runner.
set -uo pipefail
if [[ ${1:-} == internal-phase ]]; then
  [[ $# -ge 3 && -n ${FULL_CI_RESULTS:-} ]] || exit 2
  shift
  name=$1
  shift
  rc=0
  /usr/bin/time -v -o "$FULL_CI_RESULTS/$name.time" "$@" > "$FULL_CI_RESULTS/$name.log" 2> "$FULL_CI_RESULTS/$name.stderr" || rc=$?
  printf '%s\n' "$rc" > "$FULL_CI_RESULTS/$name.exit"
  printf '%s: exit %s\n' "$name" "$rc"
  if (( rc != 0 )); then
    tail -n 80 "$FULL_CI_RESULTS/$name.log" >&2 || true
    tail -n 80 "$FULL_CI_RESULTS/$name.stderr" >&2 || true
  fi
  exit "$rc"
fi
mode=${1:-}
case "$mode" in
  normal|race) [[ $# == 1 ]] || exit 2 ;;
  cli) [[ $# == 2 && $2 =~ ^[0-5]$ ]] || { echo 'cli requires shard 0..5' >&2; exit 2; } ;;
  *) echo 'usage: run.sh normal|race|cli SHARD' >&2; exit 2 ;;
esac
script_dir=$(cd -- "$(dirname -- "$0")" && pwd)
results=${FULL_CI_RESULTS:-"$PWD/full-ci-results"}
mkdir -p "$results" || exit 1
results=$(cd -- "$results" && pwd)
export FULL_CI_RESULTS="$results"
fixture_dir=$(mktemp -d "${RUNNER_TEMP:-${TMPDIR:-/tmp}}/full-ci.XXXXXXXX") || exit 1
started=$(date +%s)
finish() {
  local rc=$?
  printf '%s\n' "$(( $(date +%s) - started ))" > "$results/total-seconds.txt"
  if [[ -n ${GITHUB_STEP_SUMMARY:-} ]]; then
    {
      printf '### Full verification: %s' "$mode"
      if [[ $mode == cli ]]; then printf ' shard %s' "$shard"; fi
      printf '\n\n| Phase | Exit |\n| --- | --- |\n'
      for result in "$results"/*.exit; do
        [[ -f $result ]] || continue
        printf '| %s | %s |\n' "$(basename "$result" .exit)" "$(cat "$result")"
      done
      if [[ -f "$results/summary.txt" ]]; then printf '\n```text\n'; cat "$results/summary.txt"; printf '```\n'; fi
      printf '\nTotal elapsed: %s seconds; driver exit: %s.\n' "$(cat "$results/total-seconds.txt")" "$rc"
    } >> "$GITHUB_STEP_SUMMARY"
  fi
  rm -rf -- "$fixture_dir"
}
# Store shard separately; EXIT trap arguments are not the driver's arguments.
shard=${2:-}
trap finish EXIT
phase() {
  local name=$1 rc=0
  shift
  printf '%s: starting\n' "$name"
  /usr/bin/time -v -o "$results/$name.time" "$@" > "$results/$name.log" 2> "$results/$name.stderr" || rc=$?
  printf '%s\n' "$rc" > "$results/$name.exit"
  printf '%s: exit %s\n' "$name" "$rc"
  if (( rc != 0 )); then
    tail -n 80 "$results/$name.log" >&2 || true
    tail -n 80 "$results/$name.stderr" >&2 || true
  fi
  return "$rc"
}
if [[ $mode == cli ]]; then
  {
    printf 'shard=%s\ncheckout=' "$shard"
    git rev-parse HEAD
    go version
    go env GOOS GOARCH CGO_ENABLED
    uname -a
  } > "$results/runner.txt" 2>&1
else
  {
    printf 'checkout='
    git rev-parse HEAD
    go version
    go env GOOS GOARCH CGO_ENABLED
    uname -a
  } > "$results/runner.txt" 2>&1
fi
# Preserve the original test suite's opt-in environment; CI does not enable
# external live integrations unless the operator explicitly configured them.
unset LOOP_TEST_FIXTURE_CATALOG LOOP_TEST_FIXTURE_CATALOG_PREPARE HOOK_ACCEPTANCE_BINARY
status=0
phase init go run ./cmd/loop-harness init --root . || status=1
phase harness go build -o "$fixture_dir/loop-harness" ./cmd/loop-harness || status=1
export HOOK_ACCEPTANCE_BINARY="$fixture_dir/loop-harness"
mkdir "$fixture_dir/catalog" || status=1
phase catalog env LOOP_TEST_FIXTURE_CATALOG_PREPARE="$fixture_dir/catalog" go test -v -count=1 -timeout=30m ./internal/cli -run '^TestPrepareFixtureCatalog$' || status=1
if ! grep -q '^--- PASS: TestPrepareFixtureCatalog ' "$results/catalog.log"; then
  echo 'fixture catalog test did not actually pass; inspect catalog.log' >&2
  printf '1\n' > "$results/catalog.exit"
  status=1
fi
export LOOP_TEST_FIXTURE_CATALOG="$fixture_dir/catalog"
if [[ $mode == cli ]]; then
  phase discover go test -race -list '^(Test|Example|Fuzz)' ./internal/cli || status=1
  if phase selection python3 "$script_dir/race-summary.py" select "$results" "$shard"; then
    pattern=$(cat "$results/pattern.txt")
    race_rc=0
    printf 'race: starting\n'
    /usr/bin/time -v -o "$results/race.time" go test -race -count=1 -timeout=30m -json ./internal/cli -run "$pattern" > "$results/race.jsonl" 2> "$results/race.stderr" || race_rc=$?
    printf '%s\n' "$race_rc" > "$results/race.exit"
    printf 'race: exit %s\n' "$race_rc"
    (( race_rc == 0 )) || { tail -n 80 "$results/race.stderr" >&2; status=1; }
    phase summary python3 "$script_dir/race-summary.py" summarize "$results" || status=1
    cat "$results/summary.txt" 2>/dev/null || true
  else
    status=1
  fi
elif [[ $mode == normal ]]; then
  phase normal-discover go list ./... || status=1
  export FULL_CI_DRIVER
  FULL_CI_DRIVER=$(realpath "$0")
  observer="$fixture_dir/observer.mk"
  for target in fmt-check vet test doctor doctor-staged validate build; do
    printf '.PHONY: full-ci-%s\nfull-ci-%s:\n\tbash "$(FULL_CI_DRIVER)" internal-phase %s $(MAKE) %s TEST_TIMEOUT=30m TEST_PACKAGES=2\n' "$target" "$target" "$target" "$target" >> "$observer"
  done
  phase checks make -k -f "$observer" full-ci-fmt-check full-ci-vet full-ci-test full-ci-doctor full-ci-doctor-staged full-ci-validate full-ci-build || status=1
  phase coverage python3 "$script_dir/verify-packages.py" "$results" normal || status=1
else
  phase discover go list -race ./... || status=1
  module=$(go list -m) || status=1
  python3 - "$results/discover.log" "$results/packages.txt" "$module/internal/cli" <<'PY'
import pathlib, sys
source, target, excluded = sys.argv[1:]
packages = pathlib.Path(source).read_text().splitlines()
if not packages or len(packages) != len(set(packages)) or packages.count(excluded) != 1:
    raise SystemExit('invalid package discovery or CLI package does not occur exactly once')
selected = [package for package in packages if package != excluded]
if not selected:
    raise SystemExit('no non-CLI packages selected')
pathlib.Path(target).write_text('\n'.join(selected) + '\n')
print(f'Discovered {len(packages)}, excluded exactly {excluded}, selected {len(selected)} packages.')
PY
  selection_rc=$?
  printf '%s\n' "$selection_rc" > "$results/selection.exit"
  if (( selection_rc != 0 )); then
    status=1
  else
    mapfile -t packages < "$results/packages.txt"
    phase race go test -race -count=1 -p 2 -timeout=30m "${packages[@]}" || status=1
    phase coverage python3 "$script_dir/verify-packages.py" "$results" race || status=1
  fi
fi
exit "$status"
