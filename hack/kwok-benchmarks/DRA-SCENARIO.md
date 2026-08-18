# The DRA scenario: what it simulates, and how to rerun it

Companion to [`README.md`](./README.md), for reviewing the DRA allocated-state
change. Every claim below links to the code that implements it, so none of it
has to be taken on trust.

<!-- toc -->
- [What it simulates](#what-it-simulates)
- [Why each parameter is what it is](#why-each-parameter-is-what-it-is)
- [Resources the scenario creates](#resources-the-scenario-creates)
- [Rerunning it](#rerunning-it)
- [Checking the run was valid](#checking-the-run-was-valid)
- [Reading the numbers](#reading-the-numbers)
- [Measured result](#measured-result)
- [What it does not simulate](#what-it-does-not-simulate)
- [Prerequisite: the kwok provider cannot do DRA unpatched](#prerequisite-the-kwok-provider-cannot-do-dra-unpatched)
<!-- /toc -->

## What it simulates

**A saturated GPU fleet that has to grow.**

The cluster starts with 1,000 nodes, each advertising 8 devices through a
`ResourceSlice`, and every one of those 8,000 devices already allocated to a
running pod through a `ResourceClaim`. That standing allocated state is the
thing under test.

Then 500 pods arrive, each wanting one device, as 50 Deployments of 10 replicas.
Nothing fits, so Cluster Autoscaler binpacks, decides it needs 63 more nodes
(500 pods over 8 devices each), and provisions them. The cluster settles at 1,063
nodes with every pod placed.

That scale-up is measured on two builds that differ only by the allocated-state
change.

| stage | code |
|---|---|
| fleet nodes + their ResourceSlices | [`remote.sh` `dra_prepare()`](./remote.sh#L301) |
| fleet occupants that allocate the claims | [`remote.sh:379`](./remote.sh#L379) |
| ResourceSlices on the node-group templates | [`remote.sh:283`](./remote.sh#L283) |
| the pending burst | [`remote.sh` `emit()`](./remote.sh#L536) |
| slices for nodes CA creates | [`remote.sh` `start_slice_publisher()`](./remote.sh#L454) |
| CA invocation (note `KWOK_PROVIDER_MODE=local`) | [`remote.sh:660`](./remote.sh#L660) |
| cluster reset between the two refs | [`remote.sh` `reset_cluster()`](./remote.sh#L618) |

## Why each parameter is what it is

**The fleet is fully allocated** — 8 claims against 8 devices per node
([`DRA_FLEET_CLAIMS_PER_NODE`](./remote.sh#L54),
[`DRA_DEVICES_PER_NODE`](./remote.sh#L48)). Two reasons. With devices free the
incoming pods would just schedule onto existing nodes, CA would never run, and
the path under test would never execute. And the allocated claims *are* the
variable: the old code recomputed the DRA allocated state on every scheduling
attempt, so the work removed is roughly *attempts x size of allocated state*.
This shape makes the second term large while keeping the first modest.

**The workload is Deployments, not bare pods** ([`emit()`](./remote.sh#L536)).
Replicas of a Deployment share a pod spec and collapse into a single pod
equivalence group, so 50 Deployments give 50 groups rather than 500. A pod with
no `ownerReference` gets a group to itself
([`equivalence/groups.go`](../../pkg/core/scaleup/equivalence/groups.go#L70)),
which is the shape `BuildTestPod` produces and is not what production looks
like.

**Claims come from `resourceClaimTemplateName`, not a per-pod
`resourceClaimName`** ([`remote.sh:401`](./remote.sh#L401), and the fleet
occupants are pinned to fleet nodes at [`remote.sh:394`](./remote.sh#L394)). A unique claim name
per pod makes every replica's spec distinct and destroys that grouping.

**Three node groups** ([`configure_provider()`](./remote.sh#L188)) so
`SchedulablePodGroups` runs its per-group predicate check across all of them, as
it would in a cluster with several machine shapes.

**Devices are the binding constraint.** Pods request 250m CPU against 16-CPU
nodes, so CPU would allow ~64 pods per node while devices allow 8. Scale-up is
driven by DRA, not by CPU.

**The 500-pod burst is deliberately small.** The cost under test scales with the
standing fleet, not with the burst. A larger burst lengthens the run without
sharpening the result.

**Scale-down is disabled for DRA runs** ([`remote.sh:61`](./remote.sh#L61)). The
fleet is underutilised by design, which is exactly what scale-down would remove.

## Resources the scenario creates

Object counts are for the full-scale run (`DRA_FLEET_NODES=1000`,
`DRA_FLEET_CLAIMS_PER_NODE=8`, 50 Deployments x 10 replicas). Snippets are the
YAML the harness applies, with shell variables resolved.

| object | count | created by |
|---|---|---|
| `DeviceClass` | 1 | [`dra_prepare()`](./remote.sh#L301) |
| fleet `Node` | 1,000 | [`dra_prepare()`](./remote.sh#L301) |
| fleet `ResourceSlice` | 1,000 | [`dra_prepare()`](./remote.sh#L301) |
| `ResourceClaimTemplate` | 1 per namespace | [`dra_claim_template()`](./remote.sh#L498) |
| fleet occupant `Deployment` | 1 (8,000 replicas) | [`dra_prepare()`](./remote.sh#L301) |
| **allocated `ResourceClaim`** | **8,000** | the scheduler, from the template |
| workload `Deployment` | 50 (500 replicas) | [`emit()`](./remote.sh#L536) |
| node-group template `Node` | 3 (in a ConfigMap) | [`configure_provider()`](./remote.sh#L188) |
| node-group template `ResourceSlice` | 3 (in a ConfigMap) | [`configure_provider()`](./remote.sh#L188) |
| `ResourceSlice` for created nodes | ~63 | [`start_slice_publisher()`](./remote.sh#L454) |

### 1. DeviceClass

One class, matching the single fake driver.

```yaml
apiVersion: resource.k8s.io/v1
kind: DeviceClass
metadata:
  name: gpu.kwok-bench
spec:
  selectors:
  - cel:
      expression: 'device.driver == "gpu.example.com"'
```

### 2. The fleet: 1,000 nodes, each with 8 devices

`kwok.x-k8s.io/node: fake` is what makes the kwok controller manage the node and
mark it Ready. Note there is **no** `cluster-autoscaler.kwok.nodegroup/*`
annotation and no `node.kubernetes.io/instance-type` label: that keeps the fleet
out of any node group, because
[`KwokCloudProvider.Cleanup()`](../../pkg/cloudprovider/kwok/kwok_provider.go)
deletes every node of every node group when CA exits, which would destroy the
allocated state between the two measured configurations.

```yaml
apiVersion: v1
kind: Node
metadata:
  name: dra-fleet-0            # ... through dra-fleet-999
  annotations:
    kwok.x-k8s.io/node: fake
  labels:
    kubernetes.io/os: linux
    kwok-bench-fleet: "true"   # occupants select on this
    type: kwok
status:
  capacity:    { cpu: "16", memory: "64Gi", pods: "110" }
  allocatable: { cpu: "16", memory: "64Gi", pods: "110" }
---
apiVersion: resource.k8s.io/v1
kind: ResourceSlice
metadata:
  name: slice-dra-fleet-0
spec:
  nodeName: dra-fleet-0
  driver: gpu.example.com
  pool:
    name: dra-fleet-0
    resourceSliceCount: 1
    generation: 1
  devices:
  - name: gpu-0                # ... through gpu-7
```

### 3. What fills the fleet: 8,000 allocated claims

One `ResourceClaimTemplate` per namespace, then a Deployment sized to consume
every device. The claims are allocated **by the scheduler** as these pods land -
not by hand-written `status.allocation` - which is both realistic and avoids
8,000 status subresource writes.

```yaml
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
          deviceClassName: gpu.kwok-bench
          allocationMode: ExactCount
          count: 1
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: dra-fleet-occupant
  namespace: dra-fleet
spec:
  replicas: 8000               # FLEET_NODES x CLAIMS_PER_NODE
  selector:
    matchLabels: { app: dra-fleet-occupant }
  template:
    metadata:
      labels: { app: dra-fleet-occupant }
    spec:
      nodeSelector: { kwok-bench-fleet: "true" }   # stays off CA-created nodes
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
```

Each replica produces one `ResourceClaim` that reaches
`status.allocation` - those 8,000 objects are the state the change is about.

### 4. The measured burst: 50 Deployments x 10 replicas

`resourceClaimTemplateName` rather than a per-pod `resourceClaimName` keeps every
replica's spec identical, so a Deployment collapses to one equivalence group.
`nodeSelector: kwok-benchmark: "true"` matches only node-group nodes, never the
fleet - so these pods cannot land on the (full) fleet and force a scale-up.

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: uni-0                  # ... through uni-49
  namespace: bench-0
spec:
  replicas: 10
  selector:
    matchLabels: { app: uni-0 }
  template:
    metadata:
      labels: { app: uni-0 }
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
          requests: { cpu: "250m", memory: "512Mi" }
      resourceClaims:          # DRA=1 only
      - name: gpu
        resourceClaimTemplateName: gpu-claim
```

### 5. What CA scales: the node-group templates

Two ConfigMaps in `default`. The provider config points at the templates, and
`fromNodeLabelKey` is how nodes are bucketed into groups - get it wrong and CA
silently never scales up.

```yaml
# ConfigMap kwok-provider-config, key "config"
apiVersion: v1alpha1
readNodesFrom: configmap
nodegroups:
  fromNodeLabelKey: "node.kubernetes.io/instance-type"
nodes:
  skipTaint: true              # or nothing ever schedules on the fake nodes
configmap:
  name: kwok-provider-templates
  key: templates
```

```yaml
# ConfigMap kwok-provider-templates, key "templates" - three of these
apiVersion: v1
kind: Node
metadata:
  name: kwok-template-1        # ... -2, -3
  annotations:
    kwok.x-k8s.io/node: fake
    cluster-autoscaler.kwok.nodegroup/name: "ng-1"
    cluster-autoscaler.kwok.nodegroup/min-count: "0"
    cluster-autoscaler.kwok.nodegroup/max-count: "5000"
  labels:
    node.kubernetes.io/instance-type: "kwok-1"   # the grouping key
    kubernetes.io/os: linux
    kwok-benchmark: "true"
    type: kwok
spec: {}
status:
  capacity:    { cpu: "16", memory: "64Gi", pods: "110" }
  allocatable: { cpu: "16", memory: "64Gi", pods: "110" }
  conditions:
  - type: Ready
    status: "True"
```

```yaml
# ConfigMap kwok-provider-templates, key "resourceSlices" - three of these.
# Read only by the patched provider; joined to a template by spec.nodeName.
apiVersion: resource.k8s.io/v1
kind: ResourceSlice
metadata:
  name: slice-kwok-template-1
spec:
  nodeName: kwok-template-1
  driver: gpu.example.com
  pool:
    name: kwok-template-1
    resourceSliceCount: 1
    generation: 1
  devices:
  - name: gpu-0                # ... through gpu-7
```

### 6. Slices for the nodes CA creates

The provider creates a `Node` and nothing else, so a node it adds advertises no
devices and no DRA pod can ever be placed on it - CA would scale up forever. A
watcher publishes the missing slice for each new node; in a real cluster the DRA
driver on the node does this.

```yaml
apiVersion: resource.k8s.io/v1
kind: ResourceSlice
metadata:
  name: slice-ng-1-abc12       # one per node CA creates
spec:
  nodeName: ng-1-abc12
  driver: gpu.example.com
  pool:
    name: ng-1-abc12
    resourceSliceCount: 1
    generation: 1
  devices:
  - name: gpu-0                # ... through gpu-7
```

## Rerunning it

Needs `gcloud` authenticated, with permission to create a GCE instance and a GCS
bucket **in your own project**. Nothing else locally - no kubectl, Go or Docker;
the VM installs everything.

```bash
git clone -b kwok-benchmarks https://github.com/tlipski/cluster-autoscaler.git kwok-harness
cd kwok-harness/hack/kwok-benchmarks

export PROJECT=<your-gcp-project>
export ZONE=europe-west1-b

# Both refs carry the kwok provider fix; they differ only by the change itself.
export REPO=https://github.com/tlipski/cluster-autoscaler.git
export SWEEP='before;50;10;53becdb
after;50;10;2675936'

export DRA=1 DRA_FLEET_NODES=1000 DRA_FLEET_CLAIMS_PER_NODE=8

MACHINE_TYPE=n2-standard-32 DURATION=300 WAIT_TIMEOUT=9000 ./run.sh
```

About 35 minutes and roughly $0.70 of `n2-standard-32`.
[`run.sh`](./run.sh) creates the VM, runs **both** refs on it, downloads the
results, prints the comparison and deletes the VM on every exit path. Both refs
share one VM and one cluster on purpose - two separately created VMs are not
comparable, which is the whole reason this harness exists.

A cheaper smoke test first:

```bash
DRA_FLEET_NODES=50 MACHINE_TYPE=n2-standard-16 DURATION=240 ./run.sh
```

~15 minutes, ~$0.15, and should show around -75% on `scaleUp:estimate` - smaller
than the full run because the effect scales with allocated-claim count.

If `run.sh` dies, the VM carries on and still uploads. It prints the recovery
commands on any non-zero exit; note it does **not** delete the VM in that case,
so run [`teardown.sh`](./teardown.sh) to clear orphans.

## Checking the run was valid

Three checks, in order. Skipping them is how a broken run gets mistaken for a
result - see the history in [`README.md`](./README.md).

```bash
cat results/dra-allocated-claims.txt   # must be 8000
cat results/summary.txt
```

1. **`dra-allocated-claims.txt` equals `FLEET_NODES x CLAIMS_PER_NODE`.** A
   half-populated fleet does not fail loudly, it just understates the result.
   Written at [`remote.sh:301`](./remote.sh#L301).
2. **Both rows report the same `nodes`.** Different node counts mean the two
   sides did different work and cannot be compared.
3. **Both `conv s` are numeric, not `-`.** A `-` means that side never converged
   and its numbers come from a stuck scale-up
   ([`converged_at()`](./collect.sh#L56)).

## Reading the numbers

[`collect.sh`](./collect.sh) writes `results/summary.txt`; the raw metric
snapshots, CA logs and timelines stay under `results/<label>/`.

| column | meaning |
|---|---|
| `estimate ms` | binpacking simulation, where the allocated state is consulted - the most direct measure of the change ([`collect.sh:95`](./collect.sh#L95)) |
| `main ms` | the whole CA loop, most of which the change does not touch, so expect a smaller relative move |
| `conv s` | wall clock from burst to every pod placed - the user-visible number |
| `nodes` | final node count; must match across both rows |
| `mean PEGs` | **ignore.** Averaged across the scale-up trajectory, so it depends on how many loops each side ran, not on the workload ([`collect.sh:81`](./collect.sh#L81)) |

## Measured result

1,000-node fleet, 8,000 allocated claims, 500-pod burst, `n2-standard-32`:

| | before (`53becdb`) | after (`2675936`) | delta |
|---|---|---|---|
| `scaleUp:estimate` | 436.5 ms | 26.0 ms | **-94.0%** |
| `main` (whole loop) | 837.8 ms | 544.6 ms | **-35.0%** |
| convergence | 69 s | 42 s | **-39.1%** |
| final nodes | 1,063 | 1,063 | identical |
| unschedulable at end | 0 | 0 | identical |

The effect tracks allocated-claim count, which is the mechanism the change
targets:

| allocated claims | `scaleUp:estimate` improvement |
|---|---|
| 400 | -75% |
| 8,000 | **-94%** |

That reproduces, from an independent path, what `hack/dra-benchmarks` measures
with benchstat: -80% at 10k claims rising to -95.4% at 40k.

**This is a single run with no significance testing.** The statistically
defensible numbers are the benchstat ones (p=0.002, n=6). Treat this as
end-to-end corroboration - it shows two things the microbenchmarks structurally
cannot, namely the whole loop improving and time-to-converge dropping on a real
1,063-node scale-up.

## What it does not simulate

kwok is a simulator, so:

- **No kubelet, containers, image pulls or VM boot.** A node goes from created to
  Ready almost instantly. Real scale-up latency is dominated by provisioning,
  removed here deliberately so the autoscaler's own cost is visible.
- **One kind cluster on one VM.** apiserver, etcd and kube-controller-manager
  share CPU with the thing being measured.
- **Homogeneous devices** - one driver, one device class, one device per claim,
  trivial CEL selectors. Real fleets vary, and selector evaluation carries cost
  this does not exercise.
- **`ResourceSlice`s for nodes CA creates are published by a loop in this
  harness** ([`start_slice_publisher()`](./remote.sh#L454)), not by a driver. The
  kwok provider creates a `Node` and nothing else, so without this the scheduler
  can never place a DRA pod on a new node and CA scales up forever.
- **Single run.** Small differences are noise.

## Prerequisite: the kwok provider cannot do DRA unpatched

On `main`, `TemplateNodeInfo` passes `nil` ResourceSlices
([`kwok_nodegroups.go`](../../pkg/cloudprovider/kwok/kwok_nodegroups.go)), and
there is no DRA handling anywhere in the provider. A node it would create
appears to have no devices, so scale-up simulation can never place a pod
carrying a `ResourceClaim`.

Both refs above therefore include a provider fix that reads slices from an
optional `resourceSlices` key in the templates ConfigMap. It is identical on both
sides, so it cannot influence the comparison - verify with:

> Run `git fetch origin` first if any SHA below reports *"unknown revision"*. A
> clone only carries what existed when it was made, and these branches are still
> moving.

Use the same SHAs the `SWEEP` above runs, so the check covers exactly the refs
being benchmarked. `git clone -b kwok-benchmarks` creates only that one *local*
branch, so a bare `kwok-dra-base` will not resolve - the other branches arrive as
`origin/kwok-dra-base`. The SHAs work either way:

```bash
git diff 53becdb 2675936 -- pkg/cloudprovider/kwok/   # empty: provider identical
git diff 53becdb 2675936 --stat                       # only the DRA change
```

Expected output of the second command - four files, all under
`pkg/simulator/dynamicresources/`:

```
 .../dynamicresources/snapshot/snapshot.go               |  82 ++++-
 .../snapshot/snapshot_allocated_state.go                | 273 +++++++++++++++
 .../snapshot/snapshot_allocated_state_test.go           | 386 +++++++++++++++++++++
 .../snapshot/snapshot_claim_tracker.go                  |  91 ++---
 4 files changed, 787 insertions(+), 45 deletions(-)
```

### Reviewing the provider fix itself

It is one commit, `27ac97c`, also on its own branch (`kwok-dra-templates`) off
`main` so it can be read without the DRA change in the way:

```bash
git fetch origin                   # a clone older than the branch will not have it
git log -1 27ac97c                 # the commit and its reasoning
git show 27ac97c                   # the full patch
git diff 3f19984 27ac97c --stat    # against main
```

The same change as it appears inside the benchmarked refs - `de948bf` is the
PR-55 base before the fix, `53becdb` is that plus the fix and nothing else:

```bash
git diff de948bf 53becdb --stat
```

```
 pkg/cloudprovider/kwok/kwok_helpers.go         | 58 +++++++++++++++++++++++++-
 pkg/cloudprovider/kwok/kwok_nodegroups.go      |  9 +++-
 pkg/cloudprovider/kwok/kwok_nodegroups_test.go | 42 +++++++++++++++++++
 pkg/cloudprovider/kwok/kwok_provider.go        | 11 ++++-
 pkg/cloudprovider/kwok/kwok_types.go           | 13 ++++--
 5 files changed, 127 insertions(+), 6 deletions(-)
```

What it does, in four parts:

| file | change |
|---|---|
| [`kwok_types.go`](../../pkg/cloudprovider/kwok/kwok_types.go) | `NodeGroup` gains a `resourceSlices` field |
| [`kwok_helpers.go`](../../pkg/cloudprovider/kwok/kwok_helpers.go) | loads an optional `resourceSlices` key from the templates ConfigMap, joining each slice to a node template by `spec.nodeName` |
| [`kwok_nodegroups.go`](../../pkg/cloudprovider/kwok/kwok_nodegroups.go) | `TemplateNodeInfo` returns deep copies of them instead of `nil` |
| [`kwok_provider.go`](../../pkg/cloudprovider/kwok/kwok_provider.go) | wires the loader into `BuildKwokProvider` |

The key is optional, so a provider config without it behaves exactly as before -
which is what keeps this from affecting any non-DRA use. The added test
(`TestTemplateNodeInfoResourceSlices`) covers the attach path, that the returned
slices are copies (the estimator renames pools per simulated node, so a shared
template would be corrupted), and the empty case.

Note the paths above resolve against the `kwok-benchmarks` branch, which is based
on `main` - so following them shows the **unpatched** code, with the `nil` this
fix replaces.

That fix is not upstream, which is why this cannot yet be run against plain
upstream refs. "The kwok provider silently cannot do DRA" is arguably a gap
worth addressing separately.
