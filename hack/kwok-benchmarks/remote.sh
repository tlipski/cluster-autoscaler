#!/usr/bin/env bash
#
# The benchmark itself. run.sh prepends a block of `export` lines and hands the
# result to GCE as a startup-script, so this executes as root at boot with no
# inbound connection of any kind - see lib.sh for why ssh is not usable here.
#
# Still self-contained: with the variables set by hand it runs on any Ubuntu box.
#
#   REPO=https://github.com/<owner>/cluster-autoscaler.git REF=main \
#   PROFILE=medium ./remote.sh
#
# If GCS_DEST is set, the log and results are mirrored there and a DONE or
# FAILED marker is written on exit. Without it everything just stays on disk.
set -euo pipefail

# A startup-script gets almost no environment: no HOME, no USER, and a PATH that
# does not include /usr/local/go/bin. Everything downstream assumes these.
export HOME="${HOME:-/root}"
export USER="${USER:-$(id -un)}"

REPO="${REPO:?REPO must be set}"
REF="${REF:-main}"
PROFILE="${PROFILE:-medium}"
DURATION="${DURATION:-600}"
SCRAPE_INTERVAL="${SCRAPE_INTERVAL:-10}"
CA_SCAN_INTERVAL="${CA_SCAN_INTERVAL:-10s}"
KIND_VERSION="${KIND_VERSION:-v0.30.0}"
KIND_IMAGE="${KIND_IMAGE:-kindest/node:v1.34.0}"
KWOK_VERSION="${KWOK_VERSION:-v0.7.0}"
GO_VERSION="${GO_VERSION:-1.26.0}"
WORKDIR="${WORKDIR:-/opt/kwok-bench}"

# --- DRA ------------------------------------------------------------------
# DRA=1 turns the workload into a GPU-style DRA fleet: template nodes advertise
# devices through ResourceSlices, a pre-existing fleet already holds allocated
# claims, and the pending pods each want a device.
#
# The allocated fleet is the point. The change under test replaced a recompute
# of the DRA allocated state on every scheduling attempt with state maintained
# incrementally, so the cost it removes is proportional to how many allocated
# claims the cluster already carries. A DRA benchmark with an empty fleet
# measures almost none of it.
#
# Requires the provider to attach ResourceSlices to node templates. Stock kwok
# passes nil (kwok_nodegroups.go), so a node it creates has no devices and no
# DRA pod can ever be placed on one - see the kwok-dra-templates commit.
DRA="${DRA:-0}"
DRA_DEVICES_PER_NODE="${DRA_DEVICES_PER_NODE:-8}"
DRA_FLEET_NODES="${DRA_FLEET_NODES:-200}"
# Default is a FULLY allocated fleet (claims == devices). Leaving devices free
# would let the pending pods land on the existing fleet, so CA would never scale
# up and the binpacking path under test would never run. Free capacity in the
# fleet is not a more realistic setup here - it is a broken measurement.
DRA_FLEET_CLAIMS_PER_NODE="${DRA_FLEET_CLAIMS_PER_NODE:-8}"
DRA_DEVICE_CLASS="gpu.kwok-bench"
DRA_DRIVER="gpu.example.com"
# The pre-allocated fleet is deliberately underutilised, which is exactly what
# scale-down removes. Keeping it means the allocated state stays put for the
# whole run instead of evaporating between configurations.
DRA_CA_FLAGS=""
[[ "$DRA" == "1" ]] && DRA_CA_FLAGS="--scale-down-enabled=false"

RESULTS_ROOT="$WORKDIR/results"
OUT="$RESULTS_ROOT"
export PATH="/usr/local/go/bin:$WORKDIR/bin:$PATH"
export KUBECONFIG="$WORKDIR/kubeconfig"

log() { echo "[remote $(date +%H:%M:%S)] $*" >&2; }

# --- workload shape --------------------------------------------------------
# Two ways to describe the work.
#
# PROFILE gives the realistic clusterloader2 mix: half the pods in 5-replica
# Deployments, a quarter in 30-replica, a quarter in 250-replica, 3000 pods per
# namespace. Skewed hard towards small workloads, which is what makes the
# Deployment count - and so the equivalence-group count - large.
#
# SWEEP instead runs an explicit list of
# "<label>;<deployments>;<replicas>[;<ref>]" configurations back to back on this
# one machine. The optional fourth field builds and measures a different commit,
# which is how an A/B comparison is done: both refs run on the same VM, against
# the same cluster, so the numbers are comparable. Two separate runs on two
# separately created VMs would not be. That exists because the
# profiles vary Deployment count and pod count together, so they cannot separate
# the two costs: SchedulablePodGroups is per equivalence group, while grouping
# and binpacking are per pod. Holding one axis fixed and moving the other is the
# only way to read off which term dominates.
#
# All configurations in a sweep share one VM, one kind cluster and one CA build,
# so their numbers are comparable; separate VMs would not be.
SWEEP="${SWEEP:-}"

case "$PROFILE" in
  small)  TARGET_PODS=1000  ;;
  medium) TARGET_PODS=5000  ;;
  large)  TARGET_PODS=15000 ;;
  xlarge) TARGET_PODS=50000 ;;
  *) [[ -n "$SWEEP" ]] || { echo "unknown profile: $PROFILE (small|medium|large|xlarge)" >&2; exit 1; }
     TARGET_PODS=0 ;;
esac

SMALL_SIZE=5;   SMALL_N=$(( TARGET_PODS / 2 / SMALL_SIZE ))
MEDIUM_SIZE=30; MEDIUM_N=$(( TARGET_PODS / 4 / MEDIUM_SIZE ))
BIG_SIZE=250;   BIG_N=$(( TARGET_PODS / 4 / BIG_SIZE ))
DEPLOY_N=$(( SMALL_N + MEDIUM_N + BIG_N ))
NAMESPACES=$(( (TARGET_PODS + 2999) / 3000 )); (( NAMESPACES > 0 )) || NAMESPACES=1

install_deps() {
  if [[ -f "$WORKDIR/.deps-done" ]]; then log "deps already installed"; return; fi
  log "installing dependencies"
  sudo mkdir -p "$WORKDIR/bin" "$RESULTS_ROOT"
  sudo chown -R "$(id -u):$(id -g)" "$WORKDIR"

  export DEBIAN_FRONTEND=noninteractive
  sudo apt-get update -qq
  sudo apt-get install -y -qq docker.io curl jq >/dev/null
  sudo usermod -aG docker "$USER" || true

  curl -sSLo /tmp/go.tgz "https://go.dev/dl/go${GO_VERSION}.linux-amd64.tar.gz"
  sudo rm -rf /usr/local/go && sudo tar -C /usr/local -xzf /tmp/go.tgz

  curl -sSLo "$WORKDIR/bin/kind" \
    "https://kind.sigs.k8s.io/dl/${KIND_VERSION}/kind-linux-amd64"
  curl -sSLo "$WORKDIR/bin/kubectl" \
    "https://dl.k8s.io/release/$(curl -sSL https://dl.k8s.io/release/stable.txt)/bin/linux/amd64/kubectl"
  chmod +x "$WORKDIR/bin/kind" "$WORKDIR/bin/kubectl"

  touch "$WORKDIR/.deps-done"
}

create_cluster() {
  # Absolute path, not bare `kind`: sudo replaces PATH with secure_path, so
  # $WORKDIR/bin is not searched even under `sudo -E`.
  local kind="$WORKDIR/bin/kind"
  if sudo -E "$kind" get clusters 2>/dev/null | grep -qx kwok-bench; then
    log "kind cluster already exists"
  else
    log "creating kind cluster"
    # The control plane has to absorb a burst of tens of thousands of object
    # writes. Stock kind limits are sized for a demo and will throttle long
    # before CA becomes the bottleneck, which would make this benchmark measure
    # the apiserver instead of the autoscaler.
    #
    # extraArgs is a map, matching kubeadm v1beta3 - which is what kind v0.30
    # still emits, whatever the node image version. The v1beta4 list-of-
    # name/value form fails here with
    #   cannot unmarshal array into Go struct field APIServer.apiServer.extraArgs
    # If you move to a kind that emits v1beta4, these three blocks need
    # converting to lists.
    cat > "$WORKDIR/kind.yaml" <<YAML
kind: Cluster
apiVersion: kind.x-k8s.io/v1alpha4
nodes:
- role: control-plane
  image: ${KIND_IMAGE}
  kubeadmConfigPatches:
  - |
    kind: ClusterConfiguration
    apiServer:
      extraArgs:
        max-requests-inflight: "3000"
        max-mutating-requests-inflight: "1000"
    controllerManager:
      extraArgs:
        kube-api-qps: "1000"
        kube-api-burst: "2000"
        concurrent-deployment-syncs: "50"
        concurrent-replicaset-syncs: "50"
    etcd:
      local:
        extraArgs:
          quota-backend-bytes: "8589934592"
YAML
    sudo -E "$kind" create cluster --name kwok-bench --config "$WORKDIR/kind.yaml" --wait 5m
  fi
  sudo -E "$kind" get kubeconfig --name kwok-bench > "$KUBECONFIG"
  chmod 600 "$KUBECONFIG"
}

install_kwok() {
  log "installing kwok $KWOK_VERSION"
  local base="https://github.com/kubernetes-sigs/kwok/releases/download/${KWOK_VERSION}"
  kubectl apply -f "${base}/kwok.yaml"
  kubectl apply -f "${base}/stage-fast.yaml"
  kubectl -n kube-system rollout status deployment/kwok-controller --timeout=5m
}

configure_provider() {
  log "configuring kwok provider"
  # No helm chart in this repo, so the two ConfigMaps the provider looks for are
  # written out by hand. Names and keys are the provider's defaults:
  # kwok_config.go ("kwok-provider-config", key "config") and kwok_helpers.go
  # ("kwok-provider-templates", key "templates").
  #
  # skipTaint: true matters. By default the provider taints the fake nodes so
  # they cannot catch production workload; here the fake nodes are the entire
  # point, and leaving the taint on means nothing ever schedules and CA scales
  # up forever.
  kubectl create configmap kwok-provider-config -n default --dry-run=client -o yaml \
    --from-literal=config="$(cat <<'YAML'
apiVersion: v1alpha1
readNodesFrom: configmap
nodegroups:
  fromNodeLabelKey: "node.kubernetes.io/instance-type"
nodes:
  skipTaint: true
configmap:
  name: kwok-provider-templates
  key: templates
YAML
)" | kubectl apply -f -

  # Three node groups, so SchedulablePodGroups runs its per-group predicate
  # check over every equivalence group - that O(groups x nodegroups) cost is
  # part of what this benchmark is here to expose.
  local templates="" ng
  for ng in 1 2 3; do
    templates+="$(cat <<YAML
---
apiVersion: v1
kind: Node
metadata:
  name: kwok-template-${ng}
  annotations:
    kwok.x-k8s.io/node: fake
    cluster-autoscaler.kwok.nodegroup/name: "ng-${ng}"
    cluster-autoscaler.kwok.nodegroup/min-count: "0"
    cluster-autoscaler.kwok.nodegroup/max-count: "5000"
  labels:
    node.kubernetes.io/instance-type: "kwok-${ng}"
    kubernetes.io/os: linux
    kwok-benchmark: "true"
    type: kwok
spec: {}
status:
  capacity:
    cpu: "16"
    memory: "64Gi"
    pods: "110"
  allocatable:
    cpu: "16"
    memory: "64Gi"
    pods: "110"
  conditions:
  - type: Ready
    status: "True"
YAML
)"$'\n'
  done

  if [[ "$DRA" != "1" ]]; then
    kubectl create configmap kwok-provider-templates -n default --dry-run=client -o yaml \
      --from-literal=templates="$templates" | kubectl apply -f -
    return
  fi

  # One ResourceSlice per template node, matched by spec.nodeName. The estimator
  # renames the pool per simulated node, so one template serves every node the
  # group creates.
  local slices=""
  for ng in 1 2 3; do
    slices+="$(cat <<YAML
---
apiVersion: resource.k8s.io/v1
kind: ResourceSlice
metadata:
  name: slice-kwok-template-${ng}
spec:
  nodeName: kwok-template-${ng}
  driver: ${DRA_DRIVER}
  pool:
    name: kwok-template-${ng}
    resourceSliceCount: 1
    generation: 1
  devices:
$(for (( d = 0; d < DRA_DEVICES_PER_NODE; d++ )); do echo "  - name: gpu-${d}"; done)
YAML
)"$'\n'
  done

  kubectl create configmap kwok-provider-templates -n default --dry-run=client -o yaml \
    --from-literal=templates="$templates" \
    --from-literal=resourceSlices="$slices" | kubectl apply -f -
}

# Stands up the DRA prerequisites and, crucially, an already-allocated fleet.
#
# The fleet deliberately carries no nodegroup annotation and no grouping label,
# so the kwok provider does not consider it its own. KwokCloudProvider.Cleanup()
# deletes every node in every node group when CA shuts down - with the fleet
# owned by a node group, the first configuration's exit wiped the allocated
# state and every later configuration measured an empty cluster.
#
# CA still sees the fleet's claims and slices: the DRA snapshot is built from
# the API cluster-wide, not per node group. Which is all this needs.
#
# The allocated claims are produced by scheduling real pods onto pre-created
# kwok nodes rather than by hand-writing allocation status: the scheduler's DRA
# plugin then owns the allocation, which is both realistic and avoids thousands
# of status subresource writes.
dra_prepare() {
  [[ "$DRA" == "1" ]] || return 0
  log "preparing DRA: device class, ${DRA_FLEET_NODES}-node fleet, $(( DRA_FLEET_NODES * DRA_FLEET_CLAIMS_PER_NODE )) allocated claims"

  kubectl apply -f - >/dev/null <<YAML
apiVersion: resource.k8s.io/v1
kind: DeviceClass
metadata:
  name: ${DRA_DEVICE_CLASS}
spec:
  selectors:
  - cel:
      expression: 'device.driver == "${DRA_DRIVER}"'
YAML

  # The fleet: real Node objects kwok will mark Ready, each advertising devices.
  # Deliberately no nodegroup annotation and no grouping label - see the note
  # above dra_prepare on why provider ownership would destroy them.
  local f="$WORKDIR/dra-fleet.yaml"; : > "$f"
  local i d
  for (( i = 0; i < DRA_FLEET_NODES; i++ )); do
    cat >> "$f" <<YAML
---
apiVersion: v1
kind: Node
metadata:
  name: dra-fleet-${i}
  annotations:
    kwok.x-k8s.io/node: fake
  labels:
    kubernetes.io/os: linux
    kwok-bench-fleet: "true"
    type: kwok
status:
  capacity: { cpu: "16", memory: "64Gi", pods: "110" }
  allocatable: { cpu: "16", memory: "64Gi", pods: "110" }
---
apiVersion: resource.k8s.io/v1
kind: ResourceSlice
metadata:
  name: slice-dra-fleet-${i}
spec:
  nodeName: dra-fleet-${i}
  driver: ${DRA_DRIVER}
  pool:
    name: dra-fleet-${i}
    resourceSliceCount: 1
    generation: 1
  devices:
$(for (( d = 0; d < DRA_DEVICES_PER_NODE; d++ )); do echo "  - name: gpu-${d}"; done)
YAML
  done
  kubectl apply -f "$f" >/dev/null
  kubectl wait --for=condition=Ready nodes -l kwok-benchmark=true --timeout=5m >/dev/null 2>&1 || \
    log "  WARNING: not all fleet nodes became Ready"

  kubectl create namespace dra-fleet --dry-run=client -o yaml | kubectl apply -f - >/dev/null
  kubectl apply -f - >/dev/null <<YAML
apiVersion: resource.k8s.io/v1
kind: ResourceClaimTemplate
metadata:
  name: gpu-claim
  namespace: dra-fleet
spec:
  spec:
    devices:
      requests:
      - name: req-0
        exactly:
          deviceClassName: ${DRA_DEVICE_CLASS}
          allocationMode: ExactCount
          count: 1
YAML

  # Occupy part of the fleet. These pods schedule normally, and the claims their
  # template produces end up allocated - which is the state under test.
  kubectl apply -f - >/dev/null <<YAML
apiVersion: apps/v1
kind: Deployment
metadata:
  name: dra-fleet-occupant
  namespace: dra-fleet
spec:
  replicas: $(( DRA_FLEET_NODES * DRA_FLEET_CLAIMS_PER_NODE ))
  selector:
    matchLabels: { app: dra-fleet-occupant }
  template:
    metadata:
      labels: { app: dra-fleet-occupant }
    spec:
      # Pinned to the fleet. Without this the occupants spread onto nodes CA
      # creates, and the reset between configurations then evicts them - the
      # standing allocated state evaporates halfway through the run.
      nodeSelector: { kwok-bench-fleet: "true" }
      tolerations:
      - key: kwok.x-k8s.io/node
        operator: Exists
        effect: NoSchedule
      resourceClaims:
      - name: gpu
        resourceClaimTemplateName: gpu-claim
      containers:
      - name: c
        image: registry.k8s.io/pause:3.10
        resources:
          requests: { cpu: "250m", memory: "512Mi" }
YAML

  # Up to 20 minutes. 400 claims allocate in ~25s, but this scales with the
  # fleet and a large one must finish before any measurement starts - a
  # half-populated fleet silently understates the whole result.
  local want=$(( DRA_FLEET_NODES * DRA_FLEET_CLAIMS_PER_NODE )) got=0
  for _ in $(seq 1 240); do
    got=$(kubectl get resourceclaims -n dra-fleet -o json 2>/dev/null \
      | jq '[.items[] | select(.status.allocation != null)] | length' 2>/dev/null || echo 0)
    (( got >= want )) && break
    sleep 5
  done
  log "  fleet ready: $got/$want claims allocated"
  echo "$got" > "$RESULTS_ROOT/dra-allocated-claims.txt"
}

# Builds the given ref and echoes the binary path. One binary per ref, kept
# around, so a sweep that alternates refs does not rebuild each time.
build_ca() {
  local ref="${1:-$REF}"
  local sha bin
  if [[ ! -d "$WORKDIR/src/.git" ]]; then
    git clone --quiet "$REPO" "$WORKDIR/src"
  fi
  git -C "$WORKDIR/src" fetch --quiet --all 2>/dev/null || true
  sha=$(git -C "$WORKDIR/src" rev-parse --short "${ref}^{commit}")
  bin="$WORKDIR/bin/cluster-autoscaler-$sha"

  if [[ ! -x "$bin" ]]; then
    log "building cluster-autoscaler from $REPO @ $ref ($sha)"
    git -C "$WORKDIR/src" checkout --quiet --detach "$ref"
    # ./pkg is the main package in this repo - see the build-arch-% target in the
    # Makefile. It is not ./cmd/... as in the upstream autoscaler layout.
    ( cd "$WORKDIR/src" && go build -o "$bin" ./pkg )
  fi
  echo "$ref $sha" >> "$RESULTS_ROOT/ca-commit.txt"
  CA_BINARY="$bin"
}

# The kwok provider creates Node objects and nothing else. Attaching slices to
# the template (the kwok-dra-templates change) only teaches CA's *simulation*
# that a new node would carry devices - in the real cluster that node advertises
# nothing, so the scheduler can never place a DRA pod on it. CA then scales up
# again, forever, and no pod ever runs.
#
# This publishes the missing ResourceSlice for every node CA creates. Upstream
# the provider itself would have to do this; here a watcher is enough.
start_slice_publisher() {
  [[ "$DRA" == "1" ]] || return 0
  log "starting ResourceSlice publisher for CA-created nodes"
  (
    # The loop must not inherit -e/pipefail. On the first pass the only labelled
    # nodes are the fleet, so the grep below matches nothing and exits 1;
    # pipefail turns that into a failed assignment and -e then kills the whole
    # publisher before it has ever run. Which is exactly what happened.
    set +e +o pipefail
    published=0
    while sleep 3; do
      # Nodes that are ours, are not the pre-built fleet, and have no slice yet.
      have=$(kubectl get resourceslices -o jsonpath='{range .items[*]}{.spec.nodeName}{"\n"}{end}' 2>/dev/null | sort -u)
      want=$(kubectl get nodes -l kwok-benchmark=true -o jsonpath='{range .items[*]}{.metadata.name}{"\n"}{end}' 2>/dev/null \
             | grep -v '^dra-fleet-' | sort -u)
      missing=$(comm -23 <(printf '%s\n' "$want") <(printf '%s\n' "$have") 2>/dev/null)
      [[ -z "${missing// }" ]] && continue
      published=$(( published + $(printf '%s\n' "$missing" | grep -c .) ))
      echo "$published" > "$RESULTS_ROOT/dra-published-slices.txt"
      {
        for n in $missing; do
          cat <<SLICE
---
apiVersion: resource.k8s.io/v1
kind: ResourceSlice
metadata:
  name: slice-${n}
spec:
  nodeName: ${n}
  driver: ${DRA_DRIVER}
  pool:
    name: ${n}
    resourceSliceCount: 1
    generation: 1
  devices:
$(for (( d = 0; d < DRA_DEVICES_PER_NODE; d++ )); do echo "  - name: gpu-${d}"; done)
SLICE
        done
      } | kubectl apply -f - >/dev/null 2>&1 || true
    done
  ) &
  SLICE_PUBLISHER_PID=$!
}

dra_claim_template() {
  [[ "$DRA" == "1" ]] || return 0
  kubectl apply -f - >/dev/null <<YAML
apiVersion: resource.k8s.io/v1
kind: ResourceClaimTemplate
metadata:
  name: gpu-claim
  namespace: $1
spec:
  spec:
    devices:
      requests:
      - name: req-0
        exactly:
          deviceClassName: ${DRA_DEVICE_CLASS}
          allocationMode: ExactCount
          count: 1
YAML
}

generate_workload() {
  # In sweep mode the profile numbers are meaningless - the uniform branch below
  # logs and creates its own namespaces.
  if [[ -z "${UNIFORM_DEPLOYMENTS:-}" ]]; then
    log "generating $DEPLOY_N Deployments ($SMALL_N x$SMALL_SIZE, $MEDIUM_N x$MEDIUM_SIZE, $BIG_N x$BIG_SIZE) across $NAMESPACES namespace(s) -> ~$TARGET_PODS pods"
    local i
    for (( i = 0; i < NAMESPACES; i++ )); do
      kubectl create namespace "bench-$i" --dry-run=client -o yaml | kubectl apply -f - >/dev/null
      dra_claim_template "bench-$i"
    done
  else
    log "generating $UNIFORM_DEPLOYMENTS Deployments x $UNIFORM_REPLICAS replicas -> $(( UNIFORM_DEPLOYMENTS * UNIFORM_REPLICAS )) pods"
  fi

  # Every Deployment is its own controller, so every one of them becomes its own
  # pod equivalence group regardless of whether the specs match. That is the
  # whole reason this harness exists: the Go benchmarks build pods with no
  # ownerReference at all, which makes each individual pod its own group.
  emit() {
    local name="$1" replicas="$2" cpu="$3" mem="$4" ns="$5"
    cat <<YAML
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: $name
  namespace: $ns
spec:
  replicas: $replicas
  selector:
    matchLabels: { app: $name }
  template:
    metadata:
      labels: { app: $name }
    spec:
      nodeSelector:
        kwok-benchmark: "true"
      tolerations:
      - key: kwok.x-k8s.io/node
        operator: Exists
        effect: NoSchedule
      containers:
      - name: c
        image: registry.k8s.io/pause:3.10
        resources:
          requests: { cpu: "$cpu", memory: "$mem" }
$(if [[ "$DRA" == "1" ]]; then cat <<'EXTRA'
      resourceClaims:
      - name: gpu
        resourceClaimTemplateName: gpu-claim
EXTRA
fi)
YAML
  }

  local f="$WORKDIR/workload.yaml"
  : > "$f"

  if [[ -n "${UNIFORM_DEPLOYMENTS:-}" ]]; then
    # Sweep mode: N identical Deployments of R replicas each. Identical specs are
    # fine - every Deployment is a distinct controller UID, so each still becomes
    # its own equivalence group. Spec diversity is not what creates groups.
    local nd="$UNIFORM_DEPLOYMENTS" nr="$UNIFORM_REPLICAS"
    local pods=$(( nd * nr ))
    NAMESPACES=$(( (pods + 2999) / 3000 )); (( NAMESPACES > 0 )) || NAMESPACES=1
    for (( i = 0; i < NAMESPACES; i++ )); do
      kubectl create namespace "bench-$i" --dry-run=client -o yaml | kubectl apply -f - >/dev/null
      dra_claim_template "bench-$i"
    done
    for (( i = 0; i < nd; i++ )); do emit "uni-$i" "$nr" 250m 512Mi "bench-$(( i % NAMESPACES ))" >> "$f"; done
    cat > "$OUT/workload-shape.txt" <<EOF
mode             uniform
deployments      $nd
replicas each    $nr
target pods      $pods
namespaces       $NAMESPACES
expected PEGs    ~$nd (one per ReplicaSet)
EOF
    return
  fi

  for (( i = 0; i < SMALL_N; i++ ));  do emit "small-$i"  "$SMALL_SIZE"  250m 512Mi "bench-$(( i % NAMESPACES ))" >> "$f"; done
  for (( i = 0; i < MEDIUM_N; i++ )); do emit "medium-$i" "$MEDIUM_SIZE" 500m 1Gi   "bench-$(( i % NAMESPACES ))" >> "$f"; done
  for (( i = 0; i < BIG_N; i++ ));    do emit "big-$i"    "$BIG_SIZE"    1    2Gi   "bench-$(( i % NAMESPACES ))" >> "$f"; done

  cat > "$OUT/workload-shape.txt" <<EOF
mode             profile
profile          $PROFILE
target pods      $TARGET_PODS
deployments      $DEPLOY_N
  small  x$SMALL_SIZE   $SMALL_N
  medium x$MEDIUM_SIZE  $MEDIUM_N
  big    x$BIG_SIZE     $BIG_N
namespaces       $NAMESPACES
expected PEGs    ~$DEPLOY_N (one per ReplicaSet)
EOF
}

# Put the cluster back to an empty state so the next configuration in a sweep is
# not measured against the previous one's leftover nodes and pods.
reset_cluster() {
  log "resetting cluster"
  kubectl delete namespaces -l kwok-bench-workload=true --wait=true >/dev/null 2>&1 || true
  kubectl get namespaces -o name 2>/dev/null | grep -E 'namespace/bench-' \
    | xargs -r kubectl delete --wait=true >/dev/null 2>&1 || true
  # Nodes CA created carry the template's label; the kind control plane does not.
  if [[ "$DRA" == "1" ]]; then
    # Keep the DRA fleet and its allocated claims: rebuilding them per
    # configuration would cost minutes, and their whole purpose is to be
    # standing allocated state that every configuration is measured against.
    kubectl get nodes -l kwok-benchmark=true -o name 2>/dev/null \
      | grep -v '/dra-fleet-' | xargs -r kubectl delete --wait=true >/dev/null 2>&1 || true
  else
    kubectl delete nodes -l kwok-benchmark=true --wait=true >/dev/null 2>&1 || true
  fi
  # Up to 8 minutes: draining ~10k pods and their nodes is not quick, and
  # starting the next configuration against leftovers would silently corrupt it.
  for _ in $(seq 1 240); do
    local n p
    if [[ "$DRA" == "1" ]]; then
      n=$(kubectl get nodes -l kwok-benchmark=true --no-headers 2>/dev/null | grep -vc '^dra-fleet-' || true)
    else
      n=$(kubectl get nodes -l kwok-benchmark=true --no-headers 2>/dev/null | wc -l)
    fi
    p=$(kubectl get pods -A --no-headers 2>/dev/null | grep -c '^bench-' || true)
    (( n == 0 && p == 0 )) && break
    sleep 2
  done
  log "  reset complete (nodes=$(kubectl get nodes -l kwok-benchmark=true --no-headers 2>/dev/null | wc -l | tr -d ' '))"
}

run_benchmark() {
  log "starting cluster-autoscaler"
  # Leader election off: there is one CA and no reason to wait for a lease.
  #
  # KWOK_PROVIDER_MODE=local is required and easy to miss. The kwok provider
  # builds its own kubeclient and ignores --kubeconfig entirely
  # (kwok_provider.go:187) - without this it calls rest.InClusterConfig() and
  # dies immediately with
  #   failed to get kubeclient config for cluster: unable to load in-cluster
  #   configuration, KUBERNETES_SERVICE_HOST and KUBERNETES_SERVICE_PORT must be defined
  # In "local" mode it loads KUBECONFIG instead, which is exported above.
  POD_NAMESPACE=default KWOK_PROVIDER_MODE=local "$CA_BINARY" \
    --cloud-provider=kwok \
    --kubeconfig="$KUBECONFIG" \
    --namespace=default \
    --address=:8085 \
    --leader-elect=false \
    --scan-interval="$CA_SCAN_INTERVAL" \
    --scale-up-from-zero=true \
    --max-nodes-total=15000 \
    --max-nodes-per-scaleup=1000 \
    ${DRA_CA_FLAGS} \
    --v=2 > "$OUT/ca.log" 2>&1 &
  # Deliberately not a `trap ... EXIT` here: that would replace the finish trap
  # installed below, and the run would complete without ever uploading its
  # results or writing DONE. finish() kills CA_PID instead.
  CA_PID=$!
  local ca_pid=$CA_PID

  log "waiting for CA metrics endpoint"
  local up=0 i
  for i in $(seq 1 60); do
    if curl -sf localhost:8085/metrics >/dev/null 2>&1; then up=1; break; fi
    if ! kill -0 "$ca_pid" 2>/dev/null; then
      log "CA exited during startup - last lines of ca.log:"; tail -30 "$OUT/ca.log" >&2; exit 1
    fi
    sleep 2
  done
  (( up == 1 )) || { log "CA never served metrics"; tail -30 "$OUT/ca.log" >&2; exit 1; }

  # Let it settle so informer sync and the first loop are not attributed to the
  # scale-up burst.
  sleep 30

  log "applying workload"
  date +%s > "$OUT/t0.txt"
  kubectl apply -f "$WORKDIR/workload.yaml" >/dev/null

  log "scraping for ${DURATION}s every ${SCRAPE_INTERVAL}s"
  local end=$(( $(date +%s) + DURATION ))
  while (( $(date +%s) < end )); do
    local ts; ts=$(date +%s)
    curl -sf localhost:8085/metrics > "$OUT/metrics-$ts.txt" 2>/dev/null || true
    {
      echo "ts=$ts"
      echo "nodes=$(kubectl get nodes --no-headers 2>/dev/null | wc -l)"
      echo "pending=$(kubectl get pods -A --field-selector=status.phase=Pending --no-headers 2>/dev/null | wc -l)"
      echo "running=$(kubectl get pods -A --field-selector=status.phase=Running --no-headers 2>/dev/null | wc -l)"
    } >> "$OUT/cluster-state.txt"
    if ! kill -0 "$ca_pid" 2>/dev/null; then log "CA exited early - stopping"; break; fi
    sleep "$SCRAPE_INTERVAL"
  done

  curl -sf localhost:8085/metrics > "$OUT/metrics-final.txt" 2>/dev/null || true
  kill "$ca_pid" 2>/dev/null || true
  log "configuration complete - results in $OUT"
}

main() {
  install_deps
  create_cluster
  install_kwok
  configure_provider
  dra_prepare
  start_slice_publisher

  if [[ -z "$SWEEP" ]]; then
    build_ca "$REF"
    generate_workload
    run_benchmark
    return
  fi

  # Every configuration runs against the same cluster and the same CA binary,
  # one after another, with the cluster reset in between.
  local label nd nr ref extra n=0
  while IFS=';' read -r label nd nr ref extra; do
    [[ -z "${label// }" ]] && continue
    [[ -n "${nr:-}" && -z "${extra:-}" ]] || { echo "[remote] malformed SWEEP entry: $label" >&2; exit 1; }
    ref="${ref:-$REF}"
    n=$(( n + 1 ))
    log "=== sweep [$n] $label: ${nd} deployments x ${nr} replicas @ ${ref} ==="
    (( n > 1 )) && reset_cluster
    OUT="$RESULTS_ROOT/$label"
    mkdir -p "$OUT"
    build_ca "$ref"
    UNIFORM_DEPLOYMENTS="$nd" UNIFORM_REPLICAS="$nr" generate_workload
    run_benchmark
  done <<< "$SWEEP"
}

# --- reporting -------------------------------------------------------------
# Everything below exists so that a run is observable and recoverable without a
# connection to the VM: the log is mirrored to GCS while it runs, results are
# uploaded at the end, and a DONE or FAILED marker tells the watcher which
# happened. Without the marker a watcher cannot distinguish "still working" from
# "died ten minutes ago".
GCS_DEST="${GCS_DEST:-}"
CA_PID=""
SLICE_PUBLISHER_PID=""
mkdir -p "$WORKDIR" "$RESULTS_ROOT" 2>/dev/null || { sudo mkdir -p "$WORKDIR" "$RESULTS_ROOT"; sudo chown -R "$(id -u):$(id -g)" "$WORKDIR"; }
LOGFILE="$WORKDIR/remote.log"
exec > >(tee -a "$LOGFILE") 2>&1

# The Ubuntu GCE image ships the guest environment but not the Cloud SDK, and
# every report out of this VM goes through `gcloud storage` - so it has to exist
# before the mirror loop starts, not as part of install_deps.
bootstrap_gcloud() {
  command -v gcloud >/dev/null 2>&1 && return 0
  log "installing google-cloud-cli (for gcloud storage)"
  export DEBIAN_FRONTEND=noninteractive
  curl -sSL https://packages.cloud.google.com/apt/doc/apt-key.gpg \
    | sudo gpg --dearmor -o /usr/share/keyrings/cloud.google.gpg
  echo "deb [signed-by=/usr/share/keyrings/cloud.google.gpg] https://packages.cloud.google.com/apt cloud-sdk main" \
    | sudo tee /etc/apt/sources.list.d/google-cloud-sdk.list >/dev/null
  sudo apt-get update -qq
  sudo apt-get install -y -qq google-cloud-cli >/dev/null
}
[[ -n "$GCS_DEST" ]] && bootstrap_gcloud

publish_log() { [[ -n "$GCS_DEST" ]] && gcloud storage cp "$LOGFILE" "$GCS_DEST/remote.log" >/dev/null 2>&1 || true; }

finish() {
  local rc=$?
  # The only EXIT trap in this script - see run_benchmark.
  [[ -n "${CA_PID:-}" ]] && kill "$CA_PID" 2>/dev/null || true
  [[ -n "${SLICE_PUBLISHER_PID:-}" ]] && kill "$SLICE_PUBLISHER_PID" 2>/dev/null || true
  if [[ -n "$GCS_DEST" ]]; then
    log "uploading results to $GCS_DEST"
    gcloud storage cp -r "$RESULTS_ROOT/*" "$GCS_DEST/results/" >/dev/null 2>&1 || true
    publish_log
    if (( rc == 0 )); then
      echo ok | gcloud storage cp - "$GCS_DEST/DONE" >/dev/null 2>&1 || true
    else
      echo "exit $rc" | gcloud storage cp - "$GCS_DEST/FAILED" >/dev/null 2>&1 || true
    fi
  fi
  return $rc
}
trap finish EXIT

if [[ -n "$GCS_DEST" ]]; then
  # Mirror the log every 15s so the watcher sees progress rather than silence.
  ( while sleep 15; do publish_log; done ) &
  disown
fi

main "$@"
