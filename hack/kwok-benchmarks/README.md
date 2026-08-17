# kwok benchmark harness

Runs cluster-autoscaler against a kwok-backed kind cluster on a throwaway GCE VM,
with a workload made of real Deployments in realistic proportions, and reports
how many pod equivalence groups CA actually builds and what the loop costs.

## Why this exists

The Go benchmarks in `pkg/core/bench` build pods with `BuildTestPod`, which sets
no `OwnerReferences`. `equivalence/groups.go:70` gives any pod without a
controller its own equivalence group, so those benchmarks run at **one group per
pod** - 50,000 groups for `BenchmarkRunOnceScaleUp(1000)`. Upstream's own
`binpacking_heterogeneity` metric buckets at 1..32, so the microbenchmarks sit
four orders of magnitude above what CA expects in production.

This harness measures the other end: pods owned by real Deployments, where the
group count tracks the *controller* count rather than the pod count.

It answers a different question from the Go benchmarks and does not replace
them. For regression-testing a specific code path use `hack/dra-benchmarks` - it
isolates the change and gives you significance testing. Use this one to find out
whether realistic workload diversity changes the shape of the problem.

## Requirements

- `gcloud`, authenticated against a project you are willing to bill
- a publicly cloneable repository holding the ref under test (the VM clones over
  the network; a local checkout is not enough)

No `gsutil`, no `kubectl`, no ssh access. Everything on the VM is installed by
the VM.

## Usage

`PROJECT`, `ZONE` and `REPO` have no defaults - every script that bills or
scales something fails immediately if they are unset.

```bash
export PROJECT=my-gcp-project
export ZONE=europe-west1-b
export REPO=https://github.com/<owner>/cluster-autoscaler.git
export REF=main

PROFILE=small ./run.sh
```

`run.sh` does the whole thing: creates the bucket if needed, creates a VM whose
startup-script *is* the benchmark, follows the mirrored log, downloads the
results, prints the summary, and deletes the VM on every exit path. `KEEP_VM=1`
leaves the VM up; `./teardown.sh` removes an orphan.

### Comparing two refs (A/B)

Put both refs in one `SWEEP` so they run on the same VM against the same
cluster. Do **not** run `run.sh` twice with different `REF` - that is two VMs,
and cross-machine variance is large enough to invent a result.

Entries are `<label>;<deployments>;<replicas>[;<ref>]`:

```bash
export PROJECT=... ZONE=... REPO=...
export SWEEP='before;1000;10;<base-sha>
after;1000;10;<candidate-sha>'
MACHINE_TYPE=n2-standard-16 DURATION=300 ./run.sh
```

Both refs must be reachable from a branch pushed to `$REPO` - the VM clones over
the network. The comparison table at the end of `results/summary.txt` is what to
paste into the PR; `results/<label>/` holds the raw metric snapshots behind it.

Interleave the labels (`before`, `after`, `before2`, `after2`) if you want a
read on run-to-run noise, since there is no significance testing here.

### Recovering an interrupted run

The VM does not care whether you are watching. If `run.sh` dies - a dropped
connection, an expired credential, a `gcloud` crash - the benchmark carries on
and still uploads. Pick the results up afterwards:

```bash
DEST=gs://$PROJECT-kwok-bench/<run-id>
gcloud storage cat  $DEST/remote.log            # progress, or the failure
gcloud storage cp -r "$DEST/results/*" results/
./collect.sh
```

`run.sh` prints those two commands on any non-zero exit. An interrupted `run.sh`
does **not** delete the VM - check for orphans with `./teardown.sh`.

### DRA scenarios

`DRA=1` turns the workload into a GPU-style DRA fleet: node-group templates
advertise devices through ResourceSlices, a pre-existing fleet already holds
allocated claims, and every pending pod wants a device.

The allocated fleet is the point. The change under test replaced a recompute of
the DRA allocated state on every scheduling attempt with state maintained
incrementally, so what it saves is proportional to how many allocated claims the
cluster already carries. A DRA benchmark against an empty fleet measures almost
none of it.

**Requires a patched provider.** Stock kwok passes `nil` ResourceSlices in
`TemplateNodeInfo` (`kwok_nodegroups.go`), so a node it creates advertises no
devices and no pod with a ResourceClaim can ever be placed on one. `$REF` must
contain the `kwok-dra-templates` commit, which reads slices from an optional
`resourceSlices` key in the templates ConfigMap. Both refs of an A/B need it.

```bash
export PROJECT=... ZONE=... REPO=...
export DRA=1 DRA_FLEET_NODES=200 DRA_FLEET_CLAIMS_PER_NODE=4
export SWEEP='before;500;10;<base-sha-with-kwok-dra>
after;500;10;<candidate-sha-with-kwok-dra>'
MACHINE_TYPE=n2-standard-16 DURATION=300 ./run.sh
```

The fleet is built once and deliberately survives the reset between
configurations, so every configuration is measured against the same standing
allocated state. Scale-down is disabled for DRA runs for the same reason -
an underutilised fleet is exactly what scale-down would remove.

**Size the fleet deliberately.** The allocated claim count sets how big the
improvement looks, because the old code rescanned that state on every scheduling
attempt - the Go benchmarks measured -80% at 10,000 allocated claims rising to
-95% at 40,000. The 200x8 default is only 1,600 claims and will understate the
change substantially. For a result worth putting in a PR, use something like
`DRA_FLEET_NODES=1000 DRA_FLEET_CLAIMS_PER_NODE=8` (8,000 claims) on an
`n2-standard-32`.

Keep the fleet **fully** allocated (`CLAIMS_PER_NODE` == `DEVICES_PER_NODE`).
With free devices left over, the pending pods just land on the existing fleet,
CA never scales up, and the binpacking path under test never runs.

`results/dra-allocated-claims.txt` records how many claims actually reached
`status.allocation`. Check it: if it is far below
`DRA_FLEET_NODES x DRA_FLEET_CLAIMS_PER_NODE`, the fleet did not populate and
the run measures much less than intended.

| variable | default | meaning |
|---|---|---|
| `DRA` | `0` | `1` enables the DRA scenario |
| `DRA_DEVICES_PER_NODE` | `8` | devices each node advertises |
| `DRA_FLEET_NODES` | `200` | pre-existing nodes holding allocated claims |
| `DRA_FLEET_CLAIMS_PER_NODE` | `8` | allocated claims per fleet node (= devices, i.e. fully allocated) |

## Configuration

| variable | default | meaning |
|---|---|---|
| `PROJECT` / `ZONE` | **required** | where to create the VM |
| `REPO` | **required** | publicly cloneable repo holding `$REF` |
| `REF` | `main` | commit/branch to build CA from |
| `VM_NAME` | `kwok-bench` | instance name |
| `MACHINE_TYPE` | `n2-standard-16` | see the profile table |
| `PROFILE` | `medium` | workload size |
| `DURATION` | `600` | seconds to run CA and scrape |
| `SCRAPE_INTERVAL` | `10` | seconds between metric snapshots |
| `CA_SCAN_INTERVAL` | `10s` | CA `--scan-interval` |
| `KIND_IMAGE` | `kindest/node:v1.34.0` | pinned for comparability |
| `BUCKET` | `gs://$PROJECT-kwok-bench` | results bucket |
| `KEEP_VM` | `0` | `1` keeps the VM after the run |

## Profiles

The mix is clusterloader2's `load` test: half the pods in 5-replica Deployments,
a quarter in 30-replica, a quarter in 250-replica, 3000 pods per namespace. That
skew is what makes the Deployment count large relative to the pod count - about
one Deployment per nine pods - and the Deployment count is what drives the
equivalence-group count.

| profile | pods | Deployments | namespaces | suggested machine |
|---|---|---|---|---|
| `small` | 1,000 | 109 | 1 | `n2-standard-8` |
| `medium` | 5,000 | 546 | 2 | `n2-standard-16` |
| `large` | 15,000 | 1,640 | 5 | `n2-standard-16` |
| `xlarge` | 50,000 | 5,466 | 17 | `n2-standard-32` |

The ceiling is the VM's control plane, not CA: every pod is a real API object in
one etcd on one box. A 5000-node production cluster at 30 pods/node is 150,000
pods, roughly ten times what fits here - so `large` models a burst on a cluster
that size, not the whole cluster.

Of the named profiles only `small` has been run end to end (see below); the
larger machine suggestions are estimates. Uniform sweeps have been run up to
10,900 pods on `n2-standard-16`, which did not converge inside a 240s window -
budget `DURATION` accordingly above a few thousand pods.

## Reading the output

`collect.sh` prints, and writes to `results/summary.txt`:

- **pod equivalence groups** - mean and cumulative distribution from
  `cluster_autoscaler_binpacking_heterogeneity`. This is the *per-node-group*
  count after `SchedulablePodGroups` filtering, not the global count for the
  loop. Weight in `le="+Inf"` is past what upstream's buckets were sized for.
- **loop timings** - mean seconds for `scaleUp:buildPodEquivalenceGroups`,
  `scaleUp:estimate`, `scaleUp`, `filterOutSchedulable` and `main`.
- **final cluster state** - gauges summed across label sets, with the
  per-label breakdown.
- **scale-up timeline** - nodes/pending/running against `t+0` = workload applied.

For a sweep, `collect.sh` adds a comparison table across configurations - that
table is the artefact to put in a PR.

Know what it is worth: these are single-run gauges from a live cluster, not
repeated samples, and there is no significance testing. Treat small differences
as noise, and interleave labels if you need a feel for run-to-run spread. For
statistical confidence about a code change, `hack/dra-benchmarks` is the right
instrument - it runs benchstat over repeated samples.

## Validated run

`PROFILE=small` on `n2-standard-8`, CA at `3f19984` (main), 2026-08-17. 109
Deployments / ~1000 pods, three node groups:

```
mean groups/simulation:  15.4  (n=57)
  le=1 3   le=2 3   le=4 9   le=8 21   le=16 33   le=32 51   le=+Inf 57

buildPodEquivalenceGroups  0.0008s (n=19)
scaleUp:estimate           0.0021s (n=57)
main (whole loop)          0.4746s (n=71)

t+2s  nodes=5  pending=811   t+44s nodes=33 pending=0 running=1064
```

Scale-up converged in ~44s. Note 6 of 57 simulations exceeded upstream's top
bucket of 32 groups, at only 109 Deployments.

## Notes

Four things bit during the first real run and are now handled - worth knowing if
you change the setup:

- **No ssh, by design.** A corp workstation forces every GCP IP through
  `corp-ssh-helper` (`/etc/ssh/ssh_config.d/google_ssh_config`), which cannot
  reach a non-corp VPC. IAP avoids that but needs a firewall rule and its
  handshake intermittently 502s. Fatally, installing Docker on Ubuntu 24.04
  rewrites nftables and drops inbound port 22 permanently - and installing
  Docker is step one. Hence the startup-script.
- **`gcloud storage`, not `gsutil`.** `gsutil` uses its own legacy credential
  store and fails with "Your credentials are invalid" while `gcloud` is fine.
- **`KWOK_PROVIDER_MODE=local`.** The kwok provider builds its own kubeclient
  and ignores `--kubeconfig` (`kwok_provider.go:187`). Without it CA dies at
  startup demanding `KUBERNETES_SERVICE_HOST`.
- **kubeadm `extraArgs` is a map.** kind v0.30 emits `v1beta3` whatever the node
  image; the v1beta4 list form fails to unmarshal.

Two that remain:

- **`skipTaint: true`** is deliberate. With the provider's default taint nothing
  schedules onto the fake nodes and CA scales up until it hits max.
- **`gcloud` ECP proxy errors** can kill `run.sh` mid-poll. The run survives;
  see "Recovering an interrupted run".
