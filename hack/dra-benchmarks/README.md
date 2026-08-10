# DRA benchmark harness

Runs the Cluster Autoscaler benchmark suite twice - once at a baseline commit
and once at a candidate commit - on a dedicated cloud node, and reports the
difference with `benchstat`.

The point of running on a dedicated node rather than a laptop is variance. On a
laptop a long benchmark session heats up and drifts, and the drift is large
enough to fake a result: an early run of this suite showed a "statistically
significant" +5% on one benchmark and -24% on another, both in code the change
under test does not touch. On an idle cloud node with nothing else scheduled,
those same benchmarks come out flat.

## Requirements

- `gcloud` authenticated against a project with a GKE cluster to use
- `kubectl`
- `benchstat` (`go install golang.org/x/perf/cmd/benchstat@latest`) - only
  needed locally, for the comparison step

The benchmarked code is cloned inside the pod from a public repository, so
nothing needs to be built or pushed locally.

## Usage

`PROJECT`, `CLUSTER`, `LOCATION` and `REPO` have no defaults - every script
fails immediately if they are unset, rather than billing or benchmarking
somebody else's. All three scripts need them, so keep them in the environment:

```bash
export PROJECT=my-gcp-project
export CLUSTER=my-bench-cluster
export LOCATION=<zone-or-region>          # where $CLUSTER lives
export REPO=https://github.com/<owner>/cluster-autoscaler.git

./provision.sh    # scale the node pool up (this starts billing)
./run.sh          # run both sides, write results/ and print the comparison
./teardown.sh     # scale the node pool back to zero
```

`run.sh` is safe to repeat without reprovisioning. `collect.sh results/raw.log`
re-does just the analysis on an already-collected log, without touching the
cluster.

## Configuration

Everything is an environment variable with a default in `lib.sh`:

| variable | default | meaning |
|---|---|---|
| `PROJECT` | **required** | GCP project |
| `CLUSTER` / `LOCATION` | **required** | cluster to run on, and its zone or region |
| `NODE_POOL` | `default-pool` | pool to scale |
| `REPO` | **required** | publicly cloneable repository holding both refs |
| `BASELINE_REF` | `4064b26` | "before" commit |
| `CANDIDATE_REF` | `2e5e38a` | "after" commit |
| `BENCH_CPU` / `BENCH_MEMORY` | `16` / `64Gi` | pod size, requests == limits |
| `BENCH_GOMAXPROCS` | `8` | cores the benchmark itself may use |
| `NAMESPACE` | `dra-bench` | created if absent |
| `SUITE` | see `bench.sh` | what to run |

So benchmarking a different change is:

```bash
BASELINE_REF=abc1234 CANDIDATE_REF=def5678 ./run.sh
```

and a different fork:

```bash
REPO=https://github.com/<owner>/cluster-autoscaler.git \
  BASELINE_REF=... CANDIDATE_REF=... ./run.sh
```

### Choosing the two commits

Both sides must run **identical benchmark code**, otherwise the comparison
measures the benchmark rather than the change. The defaults satisfy this: the
branch is ordered so `4064b26` adds the benchmarks and `2e5e38a` adds the fix,
and `git diff 4064b26 2e5e38a` touches no benchmark file. If your branch is not
arranged that way, cherry-pick the benchmarks onto the baseline first.

### Choosing the suite

`SUITE` is one `label;package;regex;benchtime;count` entry per line (semicolon
separated, so a regex may contain `|`):

```bash
SUITE="dra;./pkg/estimator/;BenchmarkBinpackingEstimateDRA;3x;6" ./run.sh
```

Each label becomes its own pair of result files and its own `benchstat` table.

`count` matters. `benchstat` needs at least 6 samples to report a confidence
interval at all, and some benchmarks need more: `RunOnceScaleDownDRA` is bimodal
and does not reach significance at 6 samples even with a large mean difference,
which is why the default suite gives it 10.

## Why the pod is shaped the way it is

- **`requests == limits`** puts the pod in Guaranteed QoS, so the kubelet will
  not let anything else share the cores being measured.
- **`GOMAXPROCS` well below the CPU limit** (8 against 16) leaves the Go runtime
  headroom for GC workers without risking CFS throttling, which would show up as
  random slow samples.
- **A fixed `GOMAXPROCS`** rather than "whatever the node has" keeps results
  comparable across machine types.
- **The node pool sits at zero when idle.** A `c2d-standard-32` costs real money
  per hour; `teardown.sh` exists so it is one command to stop paying for it.

- **Interrupting `run.sh` does not stop the benchmark, and does not stop the
  billing.** The suite runs as a Kubernetes Job, so killing the script only
  drops the log follower - the Job carries on and its output stays retrievable
  from the pod. `run.sh` prints the recovery and teardown commands on every
  non-zero exit, but it will not scale the pool down on its own, because doing
  so would destroy a run that was going to finish. For unattended runs set
  `AUTO_TEARDOWN=1` and `run.sh` tears down for you, whatever the outcome:

  ```bash
  AUTO_TEARDOWN=1 BASELINE_REF=... CANDIDATE_REF=... ./run.sh
  ```

  To pick up an interrupted run, take the log from the pod and analyse it
  offline - `collect.sh` needs no cluster configuration at all:

  ```bash
  kubectl -n dra-bench logs <pod> > results/raw.log
  ./collect.sh results/raw.log
  ```

## Output

```
results/
  raw.log                  complete pod log
  <label>-baseline.txt     per-suite, benchstat-formatted
  <label>-candidate.txt
  summary.txt              all the benchstat tables
```

`raw.log` is re-read from the API after the job finishes rather than saved from
the streamed output, because a `kubectl logs -f` connection routinely does not
survive a long run and would silently truncate the results.
