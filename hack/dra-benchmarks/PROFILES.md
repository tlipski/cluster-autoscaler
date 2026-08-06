# Where the benchmark profiles come from

`BenchmarkBinpackingEstimateDRAProfiles` varies the *shape* of a DRA cluster
rather than its size. The shapes are taken from how DRA is deployed in practice,
not from what is convenient to generate. This records the reasoning so the
profiles can be argued with.

## What Cluster Autoscaler's cost actually scales with

Five independent dimensions, each of which some real deployment pushes on:

| dimension | why it costs | what pushes on it |
|---|---|---|
| ResourceClaims in the snapshot | `GatherAllocatedState` walks every claim per PreFilter | any cluster with a lot of DRA workload |
| devices per node | the allocator evaluates selectors per candidate device | MIG, large NIC fleets |
| ResourceSlices per node | listed per PreFilter | device counts above `ResourceSliceMaxDevices` (128) |
| DeviceClasses | the resolver is rebuilt per PreFilter | multi-driver nodes |
| devices per claim | allocator search space | multi-GPU training |

Varying only the claim count - which the original benchmark did - leaves four of
these untested.

## The profiles

**`gpu8/exclusive`** - 8 whole GPUs per node, one per pod. The ordinary inference
shape, and the one most DRA tutorials describe: a `ResourceClaimTemplate` per pod
so each gets its own claim, deleted with the pod.

**`gpu8/wholeNode`** - one pod takes all 8 GPUs. Distributed training. Far fewer
claims, each much larger, so the allocator does more work per claim while
`GatherAllocatedState` does less.

**`mig56/fractional`** - 8 GPUs partitioned 7 ways each, 56 devices per node. The
NVIDIA DRA driver exposes MIG partitions as first-class devices, and the whole
point of MIG is that many small workloads share a card, so both device count and
claim count go up together.

**`devices256/multiSlice`** - 256 devices per node, which no longer fits in one
ResourceSlice. `ResourceSliceMaxDevices` is 128, and drivers split at that
boundary - the GKE predictor does exactly this - so a node publishes several
slices and anything iterating slices per attempt pays more.

**`multiDriver/gpu+net+computeDomain`** - three drivers on one node, each with its
own pool and DeviceClass. This is not hypothetical: a GKE GPU node can publish
slices for `gpu.nvidia.com`, `dra.net` (NICs) and `compute-domain.nvidia.com`
(which itself contributes two device classes, daemon and channel) at the same
time. Single-driver benchmarks miss the DeviceClass dimension entirely.

**`gpu8/sharedClaims`** - four pods referencing one ResourceClaim. Claims can be
shared, and are, for inference servers with sidecars and for anything
coordinating over one device. This goes through a different snapshot path than
pod-owned claims: `ReservePodClaims` on an existing claim rather than
`AddClaims` of a new one.

## Where the maintained state costs more

Every profile above is a case the incremental tracker is good at. Two further
groups exist so the suite can also say where it is not, because a benchmark set
that only contains favourable cases is not evidence.

`BenchmarkBinpackingEstimateDRAProfiles/noGain/*` are large clusters where the
change should still buy little:

- **`manyClaimsFewPods`** - 12k claims but three pending pods. The tracker is
  built once and read about as many times as recomputing would have walked.
- **`existingUnallocated`** - claims present but unallocated. Deriving from them
  returns immediately on a nil allocation, so the walk being removed was nearly
  free to begin with.
- **`draFleetNonDraPods`** - a DRA fleet whose pending pods use no DRA. The
  scheduler's DRA PreFilter short-circuits for pods without claims, so the
  allocated state is never requested at all.

`BenchmarkAllocatedState*` in the snapshot package go further and target the
cases where the tracker is outright slower. It trades work on writes for work on
reads, so it loses whenever writes outnumber reads, whenever the claim set is
small enough that recomputing was already trivial, or whenever the state is read
once and discarded:

- **`SingleRead`** - build, read once, throw away. The tracker does the same walk
  as recomputing *plus* records what every claim contributed, for a second read
  that never comes.
- **`WriteHeavy`** - many claim writes against one read, inverting the ratio the
  design assumes.
- **`ForkRevertChurn`** - Fork/Revert in a loop with no reads. Reverting makes the
  tracker re-read the claims the dropped layer touched, where recomputing would
  simply have invalidated and rebuilt lazily - and with no read, never rebuilt.
- **`SnapshotForkRevertNoDRA`** - a snapshot that never touches DRA, guarding
  against the machinery leaking cost into clusters that do not use the feature.

Measured results for these are in RESULTS.md. They are genuinely negative, and
the point of keeping them is that the trade is then explicit rather than
implied.

## Deliberately not covered

- **Time-slicing.** Configured through `.spec.devices.config` rather than the
  device model, so it does not change what the snapshot holds.
- **Prioritized lists** (KEP-4816, a claim listing acceptable device types in
  priority order). Would exercise the allocator, but the allocator is upstream
  code and not what these benchmarks are measuring.
- **Consumable capacity and device taints.** Both are feature-gated and change
  what `foreachAllocatedDevice` returns; the allocated-state unit tests cover
  both settings of `DRAConsumableCapacity` directly, which is cheaper than a
  benchmark.
- **Non-node-local ResourceSlices.** Network drivers can publish devices not tied
  to a node, which land under a single key in the slices PatchSet. Worth adding
  if that key ever becomes hot.

## Sources

- [Understanding dynamic resource allocation in Kubernetes (CNCF)](https://www.cncf.io/blog/2026/07/01/understanding-dynamic-resource-allocation-in-kubernetes/)
- [DRA goes GA in OpenShift 4.21 (Red Hat)](https://developers.redhat.com/articles/2026/03/25/dynamic-resource-allocation-goes-ga-red-hat-openshift-421-smarter-gpu)
- [Multi-instance GPU (MIG) with DRA on AKS](https://blog.aks.azure.com/2026/03/03/multi-instance-gpu-with-dra-on-aks)
- [DRA support for NIM (NVIDIA)](https://docs.nvidia.com/nim-operator/latest/dra.html)
- [Kubernetes GPU scheduling: DRA, KAI, MIG](https://www.techplained.com/kubernetes-gpu-scheduling)
- GKE's own DRA predictors in `GoogleCloudPlatform/cluster-autoscaler`,
  `pkg/cloudprovider/gke/dynamicresources` - `gpu.go`, `network.go`,
  `computedomain.go`, and the slice splitting in `predictor.go`
- `ResourceSliceMaxDevices` in `k8s.io/api/resource/v1`
