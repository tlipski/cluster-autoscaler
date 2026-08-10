#!/usr/bin/env bash
#
# Scales the benchmark node pool back to zero. Run this when you are done -
# a c2d-standard-32 is not cheap to leave idle.
set -euo pipefail
source "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/lib.sh"
require_cluster_config

require gcloud

if kubectl --context "$KCTX" get ns >/dev/null 2>&1; then
  log "removing job and configmap"
  kube delete job "$JOB_NAME" --ignore-not-found >/dev/null 2>&1 || true
  kube delete configmap "$JOB_NAME-script" --ignore-not-found >/dev/null 2>&1 || true
fi

log "scaling $CLUSTER/$NODE_POOL to 0"
gcloud container clusters resize "$CLUSTER" \
  --node-pool "$NODE_POOL" \
  --num-nodes 0 \
  --location "$LOCATION" \
  --project "$PROJECT" \
  --quiet

log "done - pool is at zero nodes"
