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

# Sides are whatever the log contains, in run order; the first is the base that
# later ones are compared against.
sides=$(grep -oE '^###BEGIN [a-z]+ [a-z]+###' "$RAW" | awk '{print $2}' | awk '!seen[$0]++')
[[ -n "$sides" ]] || fail "no sides found in $RAW"

for side in $sides; do
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
  # A side with an empty block did not produce results for this suite - expected
  # when a benchmark is introduced partway up a stack. Drop the side and say so,
  # rather than dropping the whole suite.
  present=(); absent=()
  for side in $sides; do
    if grep -q '^Benchmark' "$RESULTS_DIR/$label-$side.txt" 2>/dev/null; then
      present+=("$side")
    else
      absent+=("$side")
    fi
  done
  if [[ ${#present[@]} -eq 0 ]]; then
    log "WARNING: '$label' produced no results on any side - check the regex and SUITE syntax"
    continue
  fi
  {
    echo "================ $label ================"
    [[ ${#absent[@]} -gt 0 ]] && echo "(no results at: ${absent[*]} - benchmark absent at those refs)"
    if [[ ${#present[@]} -eq 1 ]]; then
      echo "(only ${present[0]} has results - nothing to compare against)"
      ( cd "$RESULTS_DIR" && benchstat "${present[0]}=$label-${present[0]}.txt" 2>&1 ) || true
    else
      args=(); for side in "${present[@]}"; do args+=("$side=$label-$side.txt"); done
      ( cd "$RESULTS_DIR" && benchstat "${args[@]}" 2>&1 ) || true
      # All-against-base does not show what each step contributed; add the
      # consecutive deltas when there are three or more sides.
      if [[ ${#present[@]} -gt 2 ]]; then
        for (( i = 1; i < ${#present[@]}; i++ )); do
          prev="${present[i-1]}"; cur="${present[i]}"
          echo; echo "---- step: $prev -> $cur ----"
          ( cd "$RESULTS_DIR" && benchstat "$prev=$label-$prev.txt" "$cur=$label-$cur.txt" 2>&1 ) || true
        done
      fi
    fi
    echo
  } | tee -a "$SUMMARY"
done

log "results in $RESULTS_DIR (summary: $SUMMARY)"
