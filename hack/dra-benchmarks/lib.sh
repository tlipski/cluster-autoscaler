#!/usr/bin/env bash
#
# Shared configuration and helpers. Every value can be overridden from the
# environment, so the harness is not tied to one cluster or one fork.
set -euo pipefail

# --- cluster ---------------------------------------------------------------
PROJECT="${PROJECT:-my-gcp-project}"
CLUSTER="${CLUSTER:-my-bench-cluster}"
LOCATION="${LOCATION:-<zone-or-region>}"
NODE_POOL="${NODE_POOL:-default-pool}"

# --- what to benchmark -----------------------------------------------------
REPO="${REPO:-https://github.com/<owner>/cluster-autoscaler.git}"
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
