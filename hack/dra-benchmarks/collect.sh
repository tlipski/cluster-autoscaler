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
  # A suite with no results on either side is not worth a benchstat error.
  if [[ ! -s "$b" || ! -s "$c" ]]; then
    log "skipping '$label' - missing results on one side"
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
