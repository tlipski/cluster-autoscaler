#!/usr/bin/env bash
#
# Runs the benchmark suite on the cluster and writes the results locally.
#
# bench.sh is shipped to the pod in a ConfigMap rather than baked into an image,
# so the suite can be edited and rerun without a build step.
set -euo pipefail
HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "$HERE/lib.sh"

require kubectl

kubectl --context "$KCTX" get nodes >/dev/null 2>&1 || \
  fail "cannot reach $CLUSTER - run provision.sh first"

READY_NODES=$(kubectl --context "$KCTX" get nodes --no-headers 2>/dev/null | grep -c ' Ready ' || true)
[[ "$READY_NODES" -ge 1 ]] || fail "no Ready nodes - run provision.sh first"

kubectl --context "$KCTX" get namespace "$NAMESPACE" >/dev/null 2>&1 || {
  log "creating namespace $NAMESPACE"
  kubectl --context "$KCTX" create namespace "$NAMESPACE"
}

log "cleaning up any previous run"
kube delete job "$JOB_NAME" --ignore-not-found --wait=true >/dev/null 2>&1 || true
kube delete configmap "$JOB_NAME-script" --ignore-not-found >/dev/null 2>&1 || true

log "uploading bench.sh"
kube create configmap "$JOB_NAME-script" --from-file=bench.sh="$HERE/bench.sh"

log "submitting job (baseline=$BASELINE_REF candidate=$CANDIDATE_REF)"
kube apply -f - <<YAML
apiVersion: batch/v1
kind: Job
metadata:
  name: $JOB_NAME
spec:
  backoffLimit: 0
  activeDeadlineSeconds: $JOB_DEADLINE_SECONDS
  template:
    spec:
      restartPolicy: Never
      containers:
      - name: bench
        image: $GO_IMAGE
        command: ["/bin/bash", "/scripts/bench.sh"]
        env:
        - name: REPO
          value: "$REPO"
        - name: BASELINE_REF
          value: "$BASELINE_REF"
        - name: CANDIDATE_REF
          value: "$CANDIDATE_REF"
        - name: GOMAXPROCS
          value: "$BENCH_GOMAXPROCS"
        - name: WORKDIR
          value: /workspace
        - name: SUITE
          value: |-
$(printf '%s\n' "${SUITE:-}" | sed 's/^/            /')
        resources:
          # requests == limits -> Guaranteed QoS, so the kubelet will not let
          # anything else share the cores this is measured on.
          requests:
            cpu: "$BENCH_CPU"
            memory: "$BENCH_MEMORY"
          limits:
            cpu: "$BENCH_CPU"
            memory: "$BENCH_MEMORY"
        volumeMounts:
        - { name: scripts,   mountPath: /scripts }
        - { name: workspace, mountPath: /workspace }
      volumes:
      - name: scripts
        configMap: { name: $JOB_NAME-script }
      - name: workspace
        emptyDir: {}
YAML

log "waiting for pod to start"
for _ in $(seq 1 60); do
  POD=$(kube get pods -l "job-name=$JOB_NAME" -o name 2>/dev/null | head -1 || true)
  [[ -n "$POD" ]] && break
  sleep 2
done
[[ -n "${POD:-}" ]] || fail "pod never appeared"

log "waiting for the container to start (pulling $GO_IMAGE)"
for _ in $(seq 1 150); do
  PHASE=$(kube get "$POD" -o jsonpath='{.status.phase}' 2>/dev/null || true)
  if [[ "$PHASE" == "Running" || "$PHASE" == "Succeeded" || "$PHASE" == "Failed" ]]; then
    break
  fi
  sleep 4
done
if [[ "${PHASE:-Pending}" == "Pending" ]]; then
  fail "pod stuck Pending: $(kube get "$POD" -o jsonpath='{.status.conditions[*].message}' 2>/dev/null)"
fi

mkdir -p "$RESULTS_DIR"
RAW="$RESULTS_DIR/raw.log"

# Follow along so there is something to watch, but never depend on it: a long
# benchmark run regularly outlives a single `kubectl logs -f` connection.
log "running - following progress (this takes a while, the baseline side is the slow one)"
kube logs -f "$POD" 2>/dev/null | sed 's/^/  | /' || true

log "waiting for the job to finish"
if ! kube wait --for=condition=complete "job/$JOB_NAME" --timeout="${JOB_DEADLINE_SECONDS}s" 2>/dev/null; then
  kube wait --for=condition=failed "job/$JOB_NAME" --timeout=30s >/dev/null 2>&1 &&
    fail "job failed - inspect with: kubectl --context $KCTX -n $NAMESPACE logs $POD"
  fail "job neither completed nor failed within ${JOB_DEADLINE_SECONDS}s"
fi

# Re-read the whole log from the API rather than keeping whatever the stream
# happened to catch, so a dropped connection cannot silently truncate results.
log "collecting complete log"
kube logs "$POD" > "$RAW"

log "splitting results"
"$HERE/collect.sh" "$RAW"
