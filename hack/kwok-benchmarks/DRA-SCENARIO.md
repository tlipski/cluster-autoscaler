# The DRA scenario: what it simulates, and how to rerun it

Companion to [`README.md`](./README.md), for reviewing the DRA allocated-state
change. Every claim below links to the code that implements it, so none of it
has to be taken on trust.

<!-- toc -->
- [What it simulates](#what-it-simulates)
- [Why each parameter is what it is](#why-each-parameter-is-what-it-is)
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

```bash
git diff kwok-dra-base kwok-dra-candidate -- pkg/cloudprovider/kwok/   # empty
git diff kwok-dra-base kwok-dra-candidate --stat                       # only the DRA change
```

That fix is not upstream, which is why this cannot yet be run against plain
upstream refs. "The kwok provider silently cannot do DRA" is arguably a gap
worth addressing separately.
