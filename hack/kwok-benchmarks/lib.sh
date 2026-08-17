#!/usr/bin/env bash
#
# Shared configuration and helpers for the kwok benchmark harness.
#
# Every value can be overridden from the environment. The ones that decide what
# gets billed have no defaults, on purpose - which project and zone to spend
# money in is not something to guess at.
set -euo pipefail

# --- the VM ----------------------------------------------------------------
PROJECT="${PROJECT:-}"
ZONE="${ZONE:-}"
VM_NAME="${VM_NAME:-kwok-bench}"
# A whole cluster's control plane runs on this one box: apiserver, etcd and
# kube-controller-manager all have to keep up with tens of thousands of pods.
# Memory is the binding constraint - etcd holds every object.
MACHINE_TYPE="${MACHINE_TYPE:-n2-standard-16}"
DISK_SIZE="${DISK_SIZE:-200GB}"
IMAGE_FAMILY="${IMAGE_FAMILY:-ubuntu-2404-lts-amd64}"
IMAGE_PROJECT="${IMAGE_PROJECT:-ubuntu-os-cloud}"

# --- what to benchmark -----------------------------------------------------
# Cloned over the network from the VM, so it has to be a repository GitHub can
# serve anonymously - a local checkout is not enough.
REPO="${REPO:-}"
REF="${REF:-main}"

# --- the workload ----------------------------------------------------------
# Pending pods to create. The Deployment count follows from the size mix in
# remote.sh, which is clusterloader2's: half the pods in 5-replica Deployments,
# a quarter in 30-replica, a quarter in 250-replica. That works out to roughly
# one Deployment per nine pods, which is the ratio that decides how many pod
# equivalence groups CA ends up building.
PROFILE="${PROFILE:-medium}"

# How long to let CA run, and how often to snapshot its metrics.
DURATION="${DURATION:-600}"
SCRAPE_INTERVAL="${SCRAPE_INTERVAL:-10}"
CA_SCAN_INTERVAL="${CA_SCAN_INTERVAL:-10s}"

# Pinned rather than "latest" so a run six months from now is comparable to
# this one. Bump deliberately, not by accident.
KIND_VERSION="${KIND_VERSION:-v0.30.0}"
KIND_IMAGE="${KIND_IMAGE:-kindest/node:v1.34.0}"
KWOK_VERSION="${KWOK_VERSION:-v0.7.0}"
GO_VERSION="${GO_VERSION:-1.26.0}"

REMOTE_DIR="${REMOTE_DIR:-/opt/kwok-bench}"
RESULTS_DIR="${RESULTS_DIR:-$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/results}"

log()  { echo "[$(date +%H:%M:%S)] $*" >&2; }
fail() { echo "ERROR: $*" >&2; exit 1; }

gc() { gcloud --project "$PROJECT" "$@"; }

# --- why there is no ssh in this harness -----------------------------------
# The benchmark runs as a GCE startup-script and reports through GCS. Nothing
# here opens an ssh connection, because on this setup ssh cannot be relied on:
#
#   1. /etc/ssh/ssh_config.d/google_ssh_config matches *any* GCP IP and forces a
#      ProxyCommand through the corp relay, which cannot reach a VPC that is not
#      corp-connected. Direct ssh fails with an opaque
#      "websocket: close 4003: failed to connect to backend".
#   2. IAP avoids that (it terminates on localhost) but needs a firewall rule,
#      and its handshake intermittently 502s.
#   3. Fatally: installing Docker on Ubuntu 24.04 rewrites nftables and drops
#      inbound port 22 for the rest of the VM's life. Installing Docker is the
#      first thing the benchmark does, so ssh dies ~40s into every run and never
#      comes back. Observed, not theorised.
#
# A startup-script has none of these problems, survives credential expiry, and
# needs no inbound connectivity at all - the VM only makes outbound calls to GCS.
BUCKET="${BUCKET:-gs://${PROJECT}-kwok-bench}"
KEEP_VM="${KEEP_VM:-0}"

# --- DRA ------------------------------------------------------------------
# DRA=1 needs a cluster-autoscaler whose kwok provider attaches ResourceSlices
# to node templates. Stock kwok does not (it passes nil), so $REF must include
# the kwok-dra-templates change or nothing will ever scale up.
DRA="${DRA:-0}"
DRA_DEVICES_PER_NODE="${DRA_DEVICES_PER_NODE:-8}"
# The allocated claim count is what sets the size of the improvement: the Go
# benchmarks measured -80% at 10k allocated claims rising to -95% at 40k, because
# the old code rescanned that state on every scheduling attempt. 200 nodes x 8 is
# only 1600 claims, which will show a much weaker effect than the change really
# has. Raise this to make the win visible; it costs pods on the VM.
DRA_FLEET_NODES="${DRA_FLEET_NODES:-200}"
# Default is a FULLY allocated fleet (claims == devices). Leaving devices free
# would let the pending pods land on the existing fleet, so CA would never scale
# up and the binpacking path under test would never run. Free capacity in the
# fleet is not a more realistic setup here - it is a broken measurement.
DRA_FLEET_CLAIMS_PER_NODE="${DRA_FLEET_CLAIMS_PER_NODE:-8}"

# Only the scripts that actually touch a VM call this. collect.sh works on an
# already-collected results directory and must stay usable with no config.
require_vm_config() {
  local missing=()
  [[ -n "$PROJECT" ]] || missing+=("PROJECT - the GCP project to create the VM in")
  [[ -n "$ZONE"    ]] || missing+=("ZONE - the zone to create the VM in")
  [[ -n "$REPO"    ]] || missing+=("REPO - a publicly cloneable repository holding \$REF")
  if [[ ${#missing[@]} -gt 0 ]]; then
    echo "ERROR: unset required configuration:" >&2
    printf '  %s\n' "${missing[@]}" >&2
    exit 1
  fi
}

require() {
  for bin in "$@"; do
    command -v "$bin" >/dev/null 2>&1 || fail "$bin is required but not on PATH"
  done
}
