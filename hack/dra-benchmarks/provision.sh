#!/usr/bin/env bash
#
# Scales the benchmark node pool up and fetches credentials.
#
# The pool is expected to sit at zero nodes when idle - a c2d-standard-32 is
# not something to leave running. teardown.sh puts it back.
set -euo pipefail
source "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/lib.sh"
require_cluster_config

NODES="${NODES:-1}"

require gcloud kubectl

log "scaling $CLUSTER/$NODE_POOL to $NODES node(s) in $LOCATION"
gcloud container clusters resize "$CLUSTER" \
  --node-pool "$NODE_POOL" \
  --num-nodes "$NODES" \
  --location "$LOCATION" \
  --project "$PROJECT" \
  --quiet

log "fetching credentials"
gcloud container clusters get-credentials "$CLUSTER" \
  --location "$LOCATION" \
  --project "$PROJECT"

log "waiting for a Ready node"
kubectl --context "$KCTX" wait --for=condition=Ready nodes --all --timeout=10m

kubectl --context "$KCTX" get nodes \
  -o custom-columns='NAME:.metadata.name,CPU:.status.allocatable.cpu,MEM:.status.allocatable.memory,TYPE:.metadata.labels.node\.kubernetes\.io/instance-type'

log "ready - the pool is now billing, run teardown.sh when finished"
