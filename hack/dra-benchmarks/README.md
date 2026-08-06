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

```bash
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
| `PROJECT` | `my-gcp-project` | GCP project |
| `CLUSTER` / `LOCATION` | `my-bench-cluster` / `<zone-or-region>` | cluster to run on |
| `NODE_POOL` | `default-pool` | pool to scale |
| `REPO` | the public fork | repository to benchmark |
| `BASELINE_REF` | `61fec2d` | "before" commit |
| `CANDIDATE_REF` | `3a3d0b0` | "after" commit |
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
REPO=https://github.com/someone/cluster-autoscaler.git \
  BASELINE_REF=... CANDIDATE_REF=... ./run.sh
```

### Choosing the two commits

Both sides must run **identical benchmark code**, otherwise the comparison
measures the benchmark rather than the change. The defaults satisfy this: the
branch is ordered so `61fec2d` adds the benchmarks and `3a3d0b0` adds the fix,
and `git diff 61fec2d 3a3d0b0` touches no benchmark file. If your branch is not
arranged that way, cherry-pick the benchmarks onto the baseline first.

### Choosing the suite

`SUITE` is one `label|package|regex|benchtime|count` entry per line:

```bash
SUITE="dra|./pkg/estimator/|BenchmarkBinpackingEstimateDRA|3x|6" ./run.sh
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
