#!/usr/bin/env bash
#
# Shared configuration and helpers. Every value can be overridden from the
# environment, so the harness is not tied to one cluster or one fork.
set -euo pipefail

# --- cluster ---------------------------------------------------------------
# No defaults: which project and cluster to bill and scale is not something to
# guess at. Export these, or put them in a local env file the harness sources.
PROJECT="${PROJECT:?PROJECT must be set - the GCP project owning the benchmark cluster}"
CLUSTER="${CLUSTER:?CLUSTER must be set - the GKE cluster to run the benchmark on}"
LOCATION="${LOCATION:?LOCATION must be set - the zone or region of \$CLUSTER}"
NODE_POOL="${NODE_POOL:-default-pool}"

# --- what to benchmark -----------------------------------------------------
# The pod clones this over the network, so it must be a repository GitHub can
# serve anonymously - a local checkout is not enough.
REPO="${REPO:?REPO must be set - a publicly cloneable repository holding both refs}"
# Baseline is the commit that adds the benchmarks but not the fix, so both
# sides run byte-identical measurement code.
BASELINE_REF="${BASELINE_REF:-4064b26}"
CANDIDATE_REF="${CANDIDATE_REF:-2e5e38a}"

# --- pod sizing ------------------------------------------------------------
# Requests equal limits so the pod is Guaranteed QoS. CPU is set well above
# GOMAXPROCS so the runtime never contends with itself or gets CFS-throttled.
GO_IMAGE="${GO_IMAGE:-golang:1.26}"
BENCH_CPU="${BENCH_CPU:-16}"
BENCH_MEMORY="${BENCH_MEMORY:-64Gi}"
BENCH_GOMAXPROCS="${BENCH_GOMAXPROCS:-8}"
JOB_NAME="${JOB_NAME:-dra-bench}"
# A namespace of our own: `default` on shared clusters often carries a
# ResourceQuota far below what a benchmark pod needs.
NAMESPACE="${NAMESPACE:-dra-bench}"
# Generous: the baseline side of the loop benchmarks alone takes several minutes.
JOB_DEADLINE_SECONDS="${JOB_DEADLINE_SECONDS:-7200}"

RESULTS_DIR="${RESULTS_DIR:-$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/results}"

KCTX="gke_${PROJECT}_${LOCATION}_${CLUSTER}"

log()  { echo "[$(date +%H:%M:%S)] $*" >&2; }
fail() { echo "ERROR: $*" >&2; exit 1; }

kube() { kubectl --context "$KCTX" -n "$NAMESPACE" "$@"; }

require() {
  for bin in "$@"; do
    command -v "$bin" >/dev/null 2>&1 || fail "$bin is required but not on PATH"
  done
}
