#!/usr/bin/env bash
#
# Runs the benchmark suite twice - once at BASELINE_REF and once at CANDIDATE_REF -
# and prints both sets of results with markers the collector splits on.
#
# This is the script that executes inside the benchmark pod. It is deliberately
# self-contained so it can also be run directly on any machine:
#
#   REPO=https://github.com/<owner>/cluster-autoscaler.git \
#   BASELINE_REF=61fec2d CANDIDATE_REF=3a3d0b0 ./bench.sh
#
set -euo pipefail

REPO="${REPO:-https://github.com/<owner>/cluster-autoscaler.git}"
BASELINE_REF="${BASELINE_REF:?BASELINE_REF must be set}"
CANDIDATE_REF="${CANDIDATE_REF:?CANDIDATE_REF must be set}"
WORKDIR="${WORKDIR:-/workspace}"

# Benchmarks are run at a fixed GOMAXPROCS so results do not depend on how many
# cores the machine happens to have, and so there is always spare capacity for
# the runtime and the system - the point of running this on a dedicated node is
# that nothing competes with the measurement.
export GOMAXPROCS="${GOMAXPROCS:-8}"
export GOFLAGS="${GOFLAGS:--mod=mod}"

# Each entry is: <label>|<package>|<bench regex>|<benchtime>|<count>
# RunOnceScaleDownDRA gets more samples because it is bimodal - at 6 samples it
# does not reach significance even with a large mean difference.
DEFAULT_SUITE="\
loop|./pkg/core/bench/|BenchmarkRunOnce|1x|6
scaledowndra|./pkg/core/bench/|BenchmarkRunOnceScaleDownDRA|1x|10
dra|./pkg/estimator/|BenchmarkBinpackingEstimateDRA|3x|6
nodra|./pkg/estimator/|BenchmarkBinpackingEstimate\$|2x|6
store|./pkg/simulator/clustersnapshot/store/|.|50x|6"

SUITE="${SUITE:-$DEFAULT_SUITE}"

log() { echo "[bench] $*" >&2; }

clone_once() {
  if [[ ! -d "$WORKDIR/src/.git" ]]; then
    log "cloning $REPO"
    mkdir -p "$WORKDIR"
    git clone --quiet "$REPO" "$WORKDIR/src"
  fi
}

run_side() {
  local side="$1" ref="$2"
  cd "$WORKDIR/src"
  git checkout --quiet --detach "$ref"
  log "=== $side @ $(git rev-parse --short HEAD) ==="

  # Warm the build cache so compilation is not attributed to the first benchmark.
  go build ./pkg/... >/dev/null 2>&1 || true

  while IFS='|' read -r label pkg regex benchtime count; do
    [[ -z "${label:-}" ]] && continue
    log "  $label: $pkg $regex (benchtime=$benchtime count=$count)"
    echo "###BEGIN ${side} ${label}###"
    # -run '^$' skips tests; only benchmarks execute.
    go test "$pkg" -run '^$' -bench "$regex" \
        -benchtime "$benchtime" -count "$count" -benchmem 2>&1 |
      grep -E '^(goos|goarch|pkg|cpu|Benchmark)' || true
    echo "###END ${side} ${label}###"
  done <<< "$SUITE"
}

main() {
  log "go: $(go version)"
  log "GOMAXPROCS=$GOMAXPROCS"
  log "cpus available: $(nproc 2>/dev/null || echo '?')"
  clone_once
  run_side baseline  "$BASELINE_REF"
  run_side candidate "$CANDIDATE_REF"
  log "done"
}

main "$@"
