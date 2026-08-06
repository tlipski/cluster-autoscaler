#!/usr/bin/env bash
#
# Splits a raw benchmark log into per-suite files and prints benchstat
# comparisons. Can be rerun on a saved log without touching the cluster:
#
#   ./collect.sh results/raw.log
#
set -euo pipefail
HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "$HERE/lib.sh"

RAW="${1:-$RESULTS_DIR/raw.log}"
[[ -f "$RAW" ]] || fail "no such log: $RAW"

mkdir -p "$RESULTS_DIR"

# Pull out every ###BEGIN <side> <label>### ... ###END### block into its own file.
labels=$(grep -oE '^###BEGIN [a-z]+ [a-z]+###' "$RAW" | awk '{print $3}' | tr -d '#' | sort -u)
[[ -n "$labels" ]] || fail "no benchmark blocks found in $RAW"

for side in baseline candidate; do
  for label in $labels; do
    awk -v s="###BEGIN $side $label###" -v e="###END $side $label###" \
      'index($0,s){f=1;next} index($0,e){f=0} f' "$RAW" > "$RESULTS_DIR/$label-$side.txt"
  done
done

command -v benchstat >/dev/null 2>&1 || {
  log "benchstat not on PATH - install with: go install golang.org/x/perf/cmd/benchstat@latest"
  log "raw per-suite files are in $RESULTS_DIR"
  exit 0
}

SUMMARY="$RESULTS_DIR/summary.txt"
: > "$SUMMARY"
for label in $labels; do
  b="$RESULTS_DIR/$label-baseline.txt"
  c="$RESULTS_DIR/$label-candidate.txt"
  # An empty block means the suite did not run - a malformed SUITE entry or a
  # regex that matched nothing. Say so rather than quietly omitting it.
  if ! grep -q '^Benchmark' "$b" 2>/dev/null || ! grep -q '^Benchmark' "$c" 2>/dev/null; then
    log "WARNING: '$label' produced no benchmark results on one or both sides - check the regex and the SUITE syntax"
    continue
  fi
  {
    echo "================ $label ================"
    # Run from the results dir so benchstat labels the columns
    # 'baseline'/'candidate' instead of printing absolute paths.
    ( cd "$RESULTS_DIR" && benchstat "baseline=$label-baseline.txt" "candidate=$label-candidate.txt" 2>&1 ) || true
    echo
  } | tee -a "$SUMMARY"
done

log "results in $RESULTS_DIR (summary: $SUMMARY)"
