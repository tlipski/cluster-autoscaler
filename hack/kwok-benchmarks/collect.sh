#!/usr/bin/env bash
#
# Summarises a results directory. Needs no VM configuration, so it can be rerun
# on results pulled from an interrupted run:
#
#   ./collect.sh [results-dir]
#
# Handles both shapes: a single run (metrics at the top level) and a sweep (one
# subdirectory per configuration, plus a comparison table).
set -euo pipefail
HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "$HERE/lib.sh"

DIR="${1:-$RESULTS_DIR}"
[[ -d "$DIR" ]] || fail "no such results directory: $DIR"

# Histogram _sum/_count give the mean directly - these are counters over the
# whole run, not a distribution to compare.
hist_mean() {
  awk -v m="$2" -v f="${3:-}" '
    index($0, m "_sum") == 1 && (f == "" || index($0, f)) { s += $NF }
    index($0, m "_count") == 1 && (f == "" || index($0, f)) { c += $NF }
    END { if (c > 0) printf "%.4f  (n=%d)", s / c, c; else print "no samples" }
  ' "$1"
}

# Bare number, for the comparison table.
hist_mean_raw() {
  awk -v m="$2" -v f="${3:-}" '
    index($0, m "_sum") == 1 && (f == "" || index($0, f)) { s += $NF }
    index($0, m "_count") == 1 && (f == "" || index($0, f)) { c += $NF }
    END { if (c > 0) printf "%.4f", s / c; else printf "-" }
  ' "$1"
}

# Sum across label sets rather than taking the last line: nodes_count is split
# by {state=...}, so "last match wins" reports whichever state sorts last -
# usually 0 - for a cluster that plainly has nodes.
gauge() {
  awk -v m="$2" '
    index($0, m) == 1 && $0 !~ /^#/ {
      total += $NF
      if (match($0, /\{[^}]*\}/)) parts = parts sprintf(" %s=%s", substr($0, RSTART + 1, RLENGTH - 2), $NF)
      seen = 1
    }
    END { if (!seen) print "n/a"; else printf "%s%s\n", total, (parts == "" ? "" : "  (" substr(parts, 2) ")") }
  ' "$1"
}
gauge_raw() {
  awk -v m="$2" 'index($0, m) == 1 && $0 !~ /^#/ { t += $NF; seen = 1 } END { print (seen ? t : "-") }' "$1"
}

shape_field() { awk -v k="$2" '$1 == k { $1 = ""; sub(/^ +/, ""); print; exit }' "$1" 2>/dev/null; }

# Seconds from workload-applied to the first sample with nothing pending.
converged_at() {
  local d="$1" t0
  t0=$(cat "$d/t0.txt" 2>/dev/null || echo 0)
  [[ -f "$d/cluster-state.txt" ]] || { echo "-"; return; }
  awk -v t0="$t0" '
    /^ts=/      { ts = substr($0, 4) }
    /^pending=/ { if (substr($0, 9) + 0 == 0 && !done) { print ts - t0; done = 1; exit } }
    END { if (!done) print "-" }
  ' "$d/cluster-state.txt"
}

report_one() {
  local d="$1" label="${2:-}"
  local final="$d/metrics-final.txt"
  [[ -s "$final" ]] || final="$(ls -1t "$d"/metrics-*.txt 2>/dev/null | head -1 || true)"
  [[ -s "${final:-}" ]] || { log "no metrics in $d - skipping"; return 1; }

  echo "================ ${label:-workload} ================"
  cat "$d/workload-shape.txt" 2>/dev/null || echo "(no workload-shape.txt)"
  [[ -f "$DIR/ca-commit.txt" ]] && echo "CA commit        $(cat "$DIR/ca-commit.txt")"
  echo

  echo "---- pod equivalence groups ----"
  echo "Per binpacking simulation (per node group, after SchedulablePodGroups"
  echo "filtering) - not the global group count for the loop."
  echo "  mean groups/simulation:  $(hist_mean "$final" cluster_autoscaler_binpacking_heterogeneity)"
  echo "  cumulative distribution (le = groups):"
  awk '
    /^cluster_autoscaler_binpacking_heterogeneity_bucket/ {
      if (match($0, /le="[^"]+"/)) agg[substr($0, RSTART + 4, RLENGTH - 5)] += $NF
    }
    END { for (k in agg) printf "    le=%-6s %d\n", k, agg[k] }
  ' "$final" | sort -t= -k2 -g
  echo

  echo "---- loop timings (mean seconds) ----"
  local fn
  for fn in "scaleUp:buildPodEquivalenceGroups" "scaleUp:estimate" "scaleUp" "filterOutSchedulable" "main"; do
    printf "  %-34s %s\n" "$fn" \
      "$(hist_mean "$final" cluster_autoscaler_function_duration_seconds "function=\"$fn\"")"
  done
  echo

  echo "---- final cluster state ----"
  printf "  %-34s %s\n" "nodes_count"              "$(gauge "$final" cluster_autoscaler_nodes_count)"
  printf "  %-34s %s\n" "unschedulable_pods_count" "$(gauge "$final" cluster_autoscaler_unschedulable_pods_count)"
  printf "  %-34s %s\n" "converged after"          "$(converged_at "$d")s"
  echo
}

# A sweep is a results dir whose subdirectories hold the metrics.
# Newline-delimited string rather than `mapfile`: macOS ships bash 3.2, which
# has neither mapfile nor safe `${arr[@]}` expansion for empty arrays under -u.
CONFIGS="$(find "$DIR" -mindepth 2 -maxdepth 2 -name 'metrics-final.txt' 2>/dev/null \
  | while read -r f; do dirname "$f"; done | sort)"

SUMMARY="$DIR/summary.txt"
{
  if [[ -z "$CONFIGS" ]]; then
    report_one "$DIR" || fail "no metrics found in $DIR"
  else
    while IFS= read -r d; do [[ -n "$d" ]] && { report_one "$d" "$(basename "$d")" || true; }; done <<< "$CONFIGS"

    echo "================ comparison ================"
    printf "%-10s %6s %6s %8s %10s %12s %12s %10s %7s %9s\n" \
      label deploys reps pods "mean PEGs" "buildPEG ms" "estimate ms" "main ms" nodes "conv s"
    while IFS= read -r d; do
      [[ -n "$d" ]] || continue
      f="$d/metrics-final.txt"
      printf "%-10s %6s %6s %8s %10.1f %12.2f %12.2f %10.1f %7s %9s\n" \
        "$(basename "$d")" \
        "$(shape_field "$d/workload-shape.txt" deployments)" \
        "$(shape_field "$d/workload-shape.txt" replicas | awk '{print $2}')" \
        "$(shape_field "$d/workload-shape.txt" target | awk '{print $2}')" \
        "$(hist_mean_raw "$f" cluster_autoscaler_binpacking_heterogeneity)" \
        "$(awk -v v="$(hist_mean_raw "$f" cluster_autoscaler_function_duration_seconds 'function="scaleUp:buildPodEquivalenceGroups"')" 'BEGIN{print v*1000}')" \
        "$(awk -v v="$(hist_mean_raw "$f" cluster_autoscaler_function_duration_seconds 'function="scaleUp:estimate"')" 'BEGIN{print v*1000}')" \
        "$(awk -v v="$(hist_mean_raw "$f" cluster_autoscaler_function_duration_seconds 'function="main"')" 'BEGIN{print v*1000}')" \
        "$(gauge_raw "$f" cluster_autoscaler_nodes_count)" \
        "$(converged_at "$d")"
    done <<< "$CONFIGS"
    echo
  fi

  for lg in "$DIR"/ca.log "$DIR"/*/ca.log; do
    [[ -f "$lg" ]] || continue
    errs=$(grep -ciE '^[EF][0-9]' "$lg" || true)
    if [[ "$errs" -gt 0 ]]; then
      echo "CA errors in ${lg#$DIR/}: $errs"
      grep -E '^[EF][0-9]' "$lg" | head -3 | sed 's/^/    /'
    fi
  done
} | tee "$SUMMARY"

log "summary written to $SUMMARY"
