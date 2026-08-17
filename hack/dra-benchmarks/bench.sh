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

REPO="${REPO:?REPO must be set}"
WORKDIR="${WORKDIR:-/workspace}"

# Two refs stays spelled the old way. A stack of more than two is spelled with
# REFS, one '<side>:<ref>' per line. Every side runs in this one process on this
# one machine, which is the point: results from separately provisioned nodes are
# not comparable, so an N-way comparison cannot be N/2 separate two-way runs.
# Side names build the block markers collect.sh greps for - lowercase only.
if [[ -z "${REFS:-}" ]]; then
  REFS="baseline:${BASELINE_REF:?BASELINE_REF or REFS must be set}
candidate:${CANDIDATE_REF:?CANDIDATE_REF or REFS must be set}"
fi

# Benchmarks are run at a fixed GOMAXPROCS so results do not depend on how many
# cores the machine happens to have, and so there is always spare capacity for
# the runtime and the system - the point of running this on a dedicated node is
# that nothing competes with the measurement.
export GOMAXPROCS="${GOMAXPROCS:-8}"
export GOFLAGS="${GOFLAGS:--mod=mod}"

# Each entry is: <label>;<package>;<bench regex>;<benchtime>;<count>
#
# Semicolon rather than pipe, because a bench regex may itself contain '|' for
# alternation - which silently shifted every field when '|' was the separator.
# RunOnceScaleDownDRA gets more samples because it is bimodal - at 6 samples it
# does not reach significance even with a large mean difference.
DEFAULT_SUITE="\
loop;./pkg/core/bench/;BenchmarkRunOnce;1x;6
scaledowndra;./pkg/core/bench/;BenchmarkRunOnceScaleDownDRA;1x;10
dra;./pkg/estimator/;BenchmarkBinpackingEstimateDRA;3x;6
profiles;./pkg/estimator/;BenchmarkBinpackingEstimateDRAProfiles;2x;6
nodra;./pkg/estimator/;BenchmarkBinpackingEstimate\$;2x;6
adverse;./pkg/simulator/dynamicresources/snapshot/;BenchmarkAllocatedState|BenchmarkSnapshotForkRevertNoDRA;200x;6
store;./pkg/simulator/clustersnapshot/store/;.;50x;6"

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

  while IFS=';' read -r label pkg regex benchtime count extra; do
    [[ -z "${label:-}" ]] && continue
    if [[ -z "${count:-}" || -n "${extra:-}" ]]; then
      echo "[bench] malformed SUITE entry: $label" >&2
      exit 1
    fi
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

  # Validate every ref up front: discovering a bad one an hour in, after the
  # earlier sides are already measured, wastes the whole run.
  local side ref
  while IFS=':' read -r side ref; do
    [[ -z "${side// }" ]] && continue
    [[ "$side" =~ ^[a-z]+$ ]] || { echo "[bench] side '$side' must be lowercase letters only" >&2; exit 1; }
    git -C "$WORKDIR/src" rev-parse --verify --quiet "${ref}^{commit}" >/dev/null ||
      { echo "[bench] side '$side': no such commit '$ref'" >&2; exit 1; }
  done <<< "$REFS"

  while IFS=':' read -r side ref; do
    [[ -z "${side// }" ]] && continue
    run_side "$side" "$ref"
  done <<< "$REFS"
  log "done"
}

main "$@"
