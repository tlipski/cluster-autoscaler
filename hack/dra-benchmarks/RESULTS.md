# Reference run

Produced by this harness on a dedicated GKE `c2d-standard-32` node (AMD EPYC
7B13, `GOMAXPROCS=8`), comparing `4064b26` (benchmarks, no fix) against
`2e5e38a` (incremental `GatherAllocatedState`).

## Main suites

```
================ adverse ================

================ dra ================
pkg: sigs.k8s.io/cluster-autoscaler/pkg/estimator
cpu: AMD EPYC 7B13
                                                                  │   baseline    │              candidate              │
                                                                  │    sec/op     │    sec/op     vs base               │
BinpackingEstimateDRA/nodes=1000/claims=4000/pendingPods=200-8      526.96m ±  1%   97.62m ±  1%  -81.47% (p=0.002 n=6)
BinpackingEstimateDRA/nodes=5000/claims=20000/pendingPods=500-8       8.222 ±  2%    1.060 ±  2%  -87.11% (p=0.002 n=6)
BinpackingEstimateDRAProfiles/gpu8/exclusive-8                      390.94m ±  0%   84.15m ±  2%  -78.48% (p=0.002 n=6)
BinpackingEstimateDRAProfiles/gpu8/wholeNode-8                       289.6m ±  1%   122.7m ±  0%  -57.62% (p=0.002 n=6)
BinpackingEstimateDRAProfiles/mig56/fractional-8                    812.11m ±  1%   62.62m ±  3%  -92.29% (p=0.002 n=6)
BinpackingEstimateDRAProfiles/devices256/multiSlice-8               796.75m ±  1%   64.93m ±  6%  -91.85% (p=0.002 n=6)
BinpackingEstimateDRAProfiles/multiDriver/gpu+net+computeDomain-8   292.26m ±  1%   88.53m ±  3%  -69.71% (p=0.002 n=6)
BinpackingEstimateDRAProfiles/gpu8/sharedClaims-8                    63.02m ±  5%   21.32m ±  5%  -66.16% (p=0.002 n=6)
BinpackingEstimateDRAProfiles/noGain/manyClaimsFewPods-8             28.45m ±  5%   18.34m ±  4%  -35.54% (p=0.002 n=6)
BinpackingEstimateDRAProfiles/noGain/existingUnallocated-8          344.20m ±  1%   43.30m ±  2%  -87.42% (p=0.002 n=6)
BinpackingEstimateDRAProfiles/noGain/draFleetNonDraPods-8            9.792m ± 11%   9.289m ± 15%        ~ (p=0.310 n=6)
geomean                                                              270.2m         62.85m        -76.74%

                                                                  │   baseline    │              candidate              │
                                                                  │     B/op      │     B/op      vs base               │
BinpackingEstimateDRA/nodes=1000/claims=4000/pendingPods=200-8      312.05Mi ± 0%   81.66Mi ± 0%  -73.83% (p=0.002 n=6)
BinpackingEstimateDRA/nodes=5000/claims=20000/pendingPods=500-8     3132.4Mi ± 0%   792.0Mi ± 0%  -74.72% (p=0.002 n=6)
BinpackingEstimateDRAProfiles/gpu8/exclusive-8                      238.20Mi ± 0%   65.74Mi ± 0%  -72.40% (p=0.002 n=6)
BinpackingEstimateDRAProfiles/gpu8/wholeNode-8                      176.15Mi ± 0%   90.76Mi ± 0%  -48.48% (p=0.002 n=6)
BinpackingEstimateDRAProfiles/mig56/fractional-8                    358.00Mi ± 0%   39.23Mi ± 0%  -89.04% (p=0.002 n=6)
BinpackingEstimateDRAProfiles/devices256/multiSlice-8               446.17Mi ± 0%   34.09Mi ± 0%  -92.36% (p=0.002 n=6)
BinpackingEstimateDRAProfiles/multiDriver/gpu+net+computeDomain-8   162.59Mi ± 0%   53.32Mi ± 0%  -67.21% (p=0.002 n=6)
BinpackingEstimateDRAProfiles/gpu8/sharedClaims-8                    29.56Mi ± 0%   12.68Mi ± 0%  -57.08% (p=0.002 n=6)
BinpackingEstimateDRAProfiles/noGain/manyClaimsFewPods-8             5.525Mi ± 1%   5.862Mi ± 1%   +6.09% (p=0.002 n=6)
BinpackingEstimateDRAProfiles/noGain/existingUnallocated-8          217.46Mi ± 0%   27.01Mi ± 0%  -87.58% (p=0.002 n=6)
BinpackingEstimateDRAProfiles/noGain/draFleetNonDraPods-8            6.681Mi ± 0%   6.681Mi ± 0%        ~ (p=0.394 n=6)
geomean                                                              134.1Mi        39.54Mi       -70.52%

                                                                  │  baseline   │              candidate              │
                                                                  │  allocs/op  │  allocs/op   vs base                │
BinpackingEstimateDRA/nodes=1000/claims=4000/pendingPods=200-8      330.5k ± 2%   320.8k ± 1%    -2.93% (p=0.002 n=6)
BinpackingEstimateDRA/nodes=5000/claims=20000/pendingPods=500-8     3.583M ± 3%   3.167M ± 1%   -11.59% (p=0.002 n=6)
BinpackingEstimateDRAProfiles/gpu8/exclusive-8                      309.8k ± 1%   299.8k ± 0%    -3.25% (p=0.002 n=6)
BinpackingEstimateDRAProfiles/gpu8/wholeNode-8                      519.4k ± 0%   509.0k ± 0%    -2.00% (p=0.002 n=6)
BinpackingEstimateDRAProfiles/mig56/fractional-8                    244.0k ± 1%   236.5k ± 0%    -3.04% (p=0.002 n=6)
BinpackingEstimateDRAProfiles/devices256/multiSlice-8               207.3k ± 1%   200.7k ± 0%    -3.21% (p=0.002 n=6)
BinpackingEstimateDRAProfiles/multiDriver/gpu+net+computeDomain-8   546.0k ± 0%   541.5k ± 0%    -0.82% (p=0.002 n=6)
BinpackingEstimateDRAProfiles/gpu8/sharedClaims-8                   83.18k ± 2%   84.15k ± 1%    +1.17% (p=0.002 n=6)
BinpackingEstimateDRAProfiles/noGain/manyClaimsFewPods-8            22.34k ± 5%   46.32k ± 2%  +107.38% (p=0.002 n=6)
BinpackingEstimateDRAProfiles/noGain/existingUnallocated-8          193.4k ± 0%   195.1k ± 0%    +0.90% (p=0.002 n=6)
BinpackingEstimateDRAProfiles/noGain/draFleetNonDraPods-8           30.61k ± 0%   30.61k ± 0%         ~ (p=0.554 n=6)
geomean                                                             219.8k        229.5k         +4.38%

================ loop ================
pkg: sigs.k8s.io/cluster-autoscaler/pkg/core/bench
cpu: AMD EPYC 7B13
                      │  baseline   │              candidate              │
                      │   sec/op    │    sec/op     vs base               │
RunOnceScaleUp-8         10.38 ± 8%    10.39 ±  8%        ~ (p=1.000 n=6)
RunOnceScaleUpDRA-8     15.550 ± 1%    5.131 ± 10%  -67.01% (p=0.002 n=6)
RunOnceScaleDown-8       9.823 ± 3%   10.031 ±  4%        ~ (p=0.240 n=6)
RunOnceScaleDownDRA-8    6.366 ± 4%    1.739 ±  2%  -72.68% (p=0.002 n=6)
geomean                  10.02         5.522        -44.91%

                      │   baseline   │              candidate               │
                      │     B/op     │     B/op       vs base               │
RunOnceScaleUp-8        6.579Gi ± 0%   6.576Gi ±  0%        ~ (p=1.000 n=6)
RunOnceScaleUpDRA-8     9.910Gi ± 1%   3.534Gi ±  1%  -64.34% (p=0.002 n=6)
RunOnceScaleDown-8      7.126Gi ± 2%   7.126Gi ±  2%        ~ (p=0.818 n=6)
RunOnceScaleDownDRA-8   2.989Gi ± 9%   1.251Gi ± 20%  -58.13% (p=0.002 n=6)
geomean                 6.104Gi        3.794Gi        -37.85%

                      │  baseline   │              candidate              │
                      │  allocs/op  │  allocs/op    vs base               │
RunOnceScaleUp-8        51.29M ± 0%   51.27M ±  0%        ~ (p=1.000 n=6)
RunOnceScaleUpDRA-8     40.30M ± 2%   39.82M ±  1%        ~ (p=0.132 n=6)
RunOnceScaleDown-8      14.55M ± 5%   14.55M ±  5%        ~ (p=0.937 n=6)
RunOnceScaleDownDRA-8   11.71M ± 9%   10.49M ± 10%  -10.44% (p=0.002 n=6)
geomean                 24.36M        23.63M         -3.03%

================ nodra ================
pkg: sigs.k8s.io/cluster-autoscaler/pkg/estimator
cpu: AMD EPYC 7B13
                     │  baseline  │            candidate             │
                     │   sec/op   │   sec/op    vs base              │
BinpackingEstimate-8   3.876 ± 0%   3.855 ± 0%  -0.52% (p=0.002 n=6)

                     │   baseline   │           candidate           │
                     │     B/op     │     B/op      vs base         │
BinpackingEstimate-8   2.700Gi ± 0%   2.700Gi ± 0%  ~ (p=0.589 n=6)

                     │  baseline   │          candidate           │
                     │  allocs/op  │  allocs/op   vs base         │
BinpackingEstimate-8   4.418M ± 0%   4.418M ± 0%  ~ (p=0.394 n=6)

================ profiles ================
pkg: sigs.k8s.io/cluster-autoscaler/pkg/estimator
cpu: AMD EPYC 7B13
                                                                  │   baseline    │              candidate              │
                                                                  │    sec/op     │    sec/op     vs base               │
BinpackingEstimateDRAProfiles/gpu8/exclusive-8                      387.83m ±  1%   85.06m ±  2%  -78.07% (p=0.002 n=6)
BinpackingEstimateDRAProfiles/gpu8/wholeNode-8                       289.7m ±  1%   123.1m ±  2%  -57.51% (p=0.002 n=6)
BinpackingEstimateDRAProfiles/mig56/fractional-8                    799.38m ±  2%   64.75m ±  4%  -91.90% (p=0.002 n=6)
BinpackingEstimateDRAProfiles/devices256/multiSlice-8               790.42m ±  2%   54.81m ±  1%  -93.07% (p=0.002 n=6)
BinpackingEstimateDRAProfiles/multiDriver/gpu+net+computeDomain-8   291.36m ±  1%   88.82m ±  4%  -69.51% (p=0.002 n=6)
BinpackingEstimateDRAProfiles/gpu8/sharedClaims-8                    63.47m ±  2%   23.70m ±  5%  -62.66% (p=0.002 n=6)
BinpackingEstimateDRAProfiles/noGain/manyClaimsFewPods-8             27.18m ± 11%   18.95m ±  8%  -30.28% (p=0.002 n=6)
BinpackingEstimateDRAProfiles/noGain/existingUnallocated-8          338.67m ±  1%   42.43m ±  5%  -87.47% (p=0.002 n=6)
BinpackingEstimateDRAProfiles/noGain/draFleetNonDraPods-8            8.406m ± 34%   8.337m ± 21%        ~ (p=0.394 n=6)
geomean                                                              167.1m         43.20m        -74.15%

                                                                  │   baseline    │              candidate              │
                                                                  │     B/op      │     B/op      vs base               │
BinpackingEstimateDRAProfiles/gpu8/exclusive-8                      238.21Mi ± 0%   65.76Mi ± 0%  -72.40% (p=0.002 n=6)
BinpackingEstimateDRAProfiles/gpu8/wholeNode-8                      176.12Mi ± 0%   90.78Mi ± 0%  -48.45% (p=0.002 n=6)
BinpackingEstimateDRAProfiles/mig56/fractional-8                    358.02Mi ± 0%   39.26Mi ± 0%  -89.04% (p=0.002 n=6)
BinpackingEstimateDRAProfiles/devices256/multiSlice-8               446.23Mi ± 0%   34.16Mi ± 0%  -92.35% (p=0.002 n=6)
BinpackingEstimateDRAProfiles/multiDriver/gpu+net+computeDomain-8   162.70Mi ± 0%   53.28Mi ± 0%  -67.25% (p=0.002 n=6)
BinpackingEstimateDRAProfiles/gpu8/sharedClaims-8                    29.51Mi ± 0%   12.67Mi ± 0%  -57.07% (p=0.002 n=6)
BinpackingEstimateDRAProfiles/noGain/manyClaimsFewPods-8             5.556Mi ± 1%   5.847Mi ± 1%   +5.25% (p=0.002 n=6)
BinpackingEstimateDRAProfiles/noGain/existingUnallocated-8          217.46Mi ± 0%   27.01Mi ± 0%  -87.58% (p=0.002 n=6)
BinpackingEstimateDRAProfiles/noGain/draFleetNonDraPods-8            6.680Mi ± 0%   6.681Mi ± 0%        ~ (p=0.589 n=6)
geomean                                                              86.09Mi        26.14Mi       -69.63%

                                                                  │  baseline   │             candidate              │
                                                                  │  allocs/op  │  allocs/op   vs base               │
BinpackingEstimateDRAProfiles/gpu8/exclusive-8                      310.2k ± 0%   300.4k ± 0%   -3.14% (p=0.002 n=6)
BinpackingEstimateDRAProfiles/gpu8/wholeNode-8                      517.9k ± 0%   509.8k ± 0%   -1.57% (p=0.002 n=6)
BinpackingEstimateDRAProfiles/mig56/fractional-8                    243.8k ± 1%   237.4k ± 0%   -2.60% (p=0.002 n=6)
BinpackingEstimateDRAProfiles/devices256/multiSlice-8               209.7k ± 1%   202.9k ± 0%   -3.23% (p=0.002 n=6)
BinpackingEstimateDRAProfiles/multiDriver/gpu+net+computeDomain-8   545.5k ± 0%   540.0k ± 0%   -1.02% (p=0.002 n=6)
BinpackingEstimateDRAProfiles/gpu8/sharedClaims-8                   81.73k ± 0%   83.65k ± 0%   +2.36% (p=0.002 n=6)
BinpackingEstimateDRAProfiles/noGain/manyClaimsFewPods-8            23.35k ± 6%   45.82k ± 3%  +96.28% (p=0.002 n=6)
BinpackingEstimateDRAProfiles/noGain/existingUnallocated-8          193.4k ± 0%   195.2k ± 0%   +0.92% (p=0.002 n=6)
BinpackingEstimateDRAProfiles/noGain/draFleetNonDraPods-8           30.61k ± 0%   30.61k ± 0%        ~ (p=0.859 n=6)
geomean                                                             154.6k        165.1k        +6.77%

================ scaledowndra ================
pkg: sigs.k8s.io/cluster-autoscaler/pkg/core/bench
cpu: AMD EPYC 7B13
                      │  baseline  │             candidate              │
                      │   sec/op   │   sec/op    vs base                │
RunOnceScaleDownDRA-8   6.470 ± 3%   1.770 ± 9%  -72.64% (p=0.000 n=10)

                      │   baseline   │              candidate               │
                      │     B/op     │     B/op      vs base                │
RunOnceScaleDownDRA-8   2.974Gi ± 1%   1.247Gi ± 0%  -58.05% (p=0.000 n=10)

                      │  baseline   │              candidate              │
                      │  allocs/op  │  allocs/op   vs base                │
RunOnceScaleDownDRA-8   11.67M ± 1%   10.47M ± 0%  -10.23% (p=0.000 n=10)

================ store ================
pkg: sigs.k8s.io/cluster-autoscaler/pkg/simulator/clustersnapshot/store
cpu: AMD EPYC 7B13
                                            │   baseline   │             candidate              │
                                            │    sec/op    │    sec/op     vs base              │
BuildNodeInfoList/fork_add_1000_to_1000-8     47.95µ ± 38%   48.49µ ±  8%       ~ (p=0.937 n=6)
BuildNodeInfoList/fork_add_1000_to_5000-8     55.38µ ±  7%   55.63µ ±  5%       ~ (p=0.937 n=6)
BuildNodeInfoList/fork_add_1000_to_15000-8    232.5µ ±  6%   248.0µ ± 19%       ~ (p=0.310 n=6)
BuildNodeInfoList/fork_add_1000_to_100000-8   1.198m ± 38%   1.191m ± 37%       ~ (p=0.699 n=6)
BuildNodeInfoList/base_1000-8                 13.15µ ±  3%   13.25µ ± 19%       ~ (p=0.589 n=6)
BuildNodeInfoList/base_5000-8                 110.6µ ± 14%   119.5µ ±  9%       ~ (p=0.310 n=6)
BuildNodeInfoList/base_15000-8                315.8µ ± 16%   342.7µ ± 24%       ~ (p=0.485 n=6)
BuildNodeInfoList/base_100000-8               1.463m ± 36%   1.457m ± 35%       ~ (p=0.589 n=6)
geomean                                       162.9µ         167.8µ        +3.00%

                                            │   baseline   │              candidate               │
                                            │     B/op     │     B/op      vs base                │
BuildNodeInfoList/fork_add_1000_to_1000-8     32.34Ki ± 0%   32.33Ki ± 0%       ~ (p=0.424 n=6)
BuildNodeInfoList/fork_add_1000_to_5000-8     97.62Ki ± 0%   97.62Ki ± 0%       ~ (p=1.000 n=6) ¹
BuildNodeInfoList/fork_add_1000_to_15000-8    260.8Ki ± 0%   260.8Ki ± 0%       ~ (p=1.000 n=6)
BuildNodeInfoList/fork_add_1000_to_100000-8   1.578Mi ± 0%   1.578Mi ± 0%       ~ (p=1.000 n=6)
BuildNodeInfoList/base_1000-8                 16.02Ki ± 0%   16.02Ki ± 0%       ~ (p=1.000 n=6) ¹
BuildNodeInfoList/base_5000-8                 80.02Ki ± 0%   80.02Ki ± 0%       ~ (p=1.000 n=6) ¹
BuildNodeInfoList/base_15000-8                240.0Ki ± 0%   240.0Ki ± 0%       ~ (p=1.000 n=6)
BuildNodeInfoList/base_100000-8               1.531Mi ± 0%   1.531Mi ± 0%       ~ (p=1.000 n=6) ¹
geomean                                       168.2Ki        168.2Ki       -0.00%
¹ all samples are equal

                                            │  baseline  │             candidate              │
                                            │ allocs/op  │ allocs/op   vs base                │
BuildNodeInfoList/fork_add_1000_to_1000-8     3.000 ± 0%   3.000 ± 0%       ~ (p=1.000 n=6) ¹
BuildNodeInfoList/fork_add_1000_to_5000-8     3.000 ± 0%   3.000 ± 0%       ~ (p=1.000 n=6) ¹
BuildNodeInfoList/fork_add_1000_to_15000-8    3.000 ± 0%   3.000 ± 0%       ~ (p=1.000 n=6) ¹
BuildNodeInfoList/fork_add_1000_to_100000-8   3.000 ± 0%   3.000 ± 0%       ~ (p=1.000 n=6) ¹
BuildNodeInfoList/base_1000-8                 3.000 ± 0%   3.000 ± 0%       ~ (p=1.000 n=6) ¹
BuildNodeInfoList/base_5000-8                 3.000 ± 0%   3.000 ± 0%       ~ (p=1.000 n=6) ¹
BuildNodeInfoList/base_15000-8                3.000 ± 0%   3.000 ± 0%       ~ (p=1.000 n=6) ¹
BuildNodeInfoList/base_100000-8               3.000 ± 0%   3.000 ± 0%       ~ (p=1.000 n=6) ¹
geomean                                       3.000        3.000       +0.00%
¹ all samples are equal

```

## Adversarial suite

Cases chosen to be unfavourable - see PROFILES.md. These are the trade being made.

```
================ adverse ================
pkg: sigs.k8s.io/cluster-autoscaler/pkg/simulator/dynamicresources/snapshot
cpu: AMD EPYC 7B13
                                               │   baseline   │               candidate               │
                                               │    sec/op    │    sec/op      vs base                │
AllocatedStateSingleRead/claims=100-8            63.27µ ± 11%   111.79µ ±  5%   +76.69% (p=0.002 n=6)
AllocatedStateSingleRead/claims=5000-8           17.80m ±  6%    22.60m ±  6%   +26.96% (p=0.002 n=6)
AllocatedStateSingleRead/claims=20000-8          96.91m ±  6%   114.84m ±  3%   +18.51% (p=0.002 n=6)
AllocatedStateWriteHeavy/writes=100-8            164.8µ ±  6%    329.7µ ±  3%   +99.99% (p=0.002 n=6)
AllocatedStateWriteHeavy/writes=1000-8           3.932m ±  3%    7.163m ±  2%   +82.15% (p=0.002 n=6)
AllocatedStateForkRevertChurn/claims=0-8         560.9n ± 37%    566.1n ± 36%         ~ (p=0.589 n=6)
AllocatedStateForkRevertChurn/claims=10-8        1.411µ ± 17%    2.788µ ± 18%   +97.62% (p=0.002 n=6)
AllocatedStateForkRevertChurn/claims=1000-8      1.420µ ± 37%    3.872µ ± 34%  +172.68% (p=0.002 n=6)
AllocatedStateUnallocatedClaims/claims=5000-8    2.007m ±  2%    1.883m ±  1%    -6.20% (p=0.002 n=6)
AllocatedStateUnallocatedClaims/claims=20000-8   8.927m ±  1%    8.042m ±  2%    -9.91% (p=0.002 n=6)
SnapshotForkRevertNoDRA-8                        42.51µ ± 31%    43.59µ ± 19%         ~ (p=0.937 n=6)
geomean                                          220.7µ          312.4µ         +41.53%

                                               │   baseline   │               candidate                │
                                               │     B/op     │     B/op      vs base                  │
AllocatedStateSingleRead/claims=100-8            26.87Ki ± 0%   58.51Ki ± 0%  +117.77% (p=0.002 n=6)
AllocatedStateSingleRead/claims=5000-8           2.838Mi ± 0%   4.779Mi ± 0%   +68.42% (p=0.002 n=6)
AllocatedStateSingleRead/claims=20000-8          11.33Mi ± 0%   19.11Mi ± 0%   +68.67% (p=0.002 n=6)
AllocatedStateWriteHeavy/writes=100-8            155.6Ki ± 0%   195.2Ki ± 0%   +25.46% (p=0.002 n=6)
AllocatedStateWriteHeavy/writes=1000-8           1.823Mi ± 0%   1.899Mi ± 0%    +4.16% (p=0.002 n=6)
AllocatedStateForkRevertChurn/claims=0-8           336.0 ± 0%     336.0 ± 0%         ~ (p=1.000 n=6) ¹
AllocatedStateForkRevertChurn/claims=10-8        1.945Ki ± 0%   2.188Ki ± 0%   +12.50% (p=0.002 n=6)
AllocatedStateForkRevertChurn/claims=1000-8      1.949Ki ± 0%   2.192Ki ± 0%   +12.47% (p=0.002 n=6)
AllocatedStateUnallocatedClaims/claims=5000-8    1.612Mi ± 0%   1.656Mi ± 0%    +2.70% (p=0.002 n=6)
AllocatedStateUnallocatedClaims/claims=20000-8   6.465Mi ± 0%   6.639Mi ± 0%    +2.69% (p=0.002 n=6)
SnapshotForkRevertNoDRA-8                        32.86Ki ± 0%   32.86Ki ± 0%         ~ (p=1.000 n=6)
geomean                                          125.1Ki        155.3Ki        +24.14%
¹ all samples are equal

                                               │  baseline   │               candidate                │
                                               │  allocs/op  │  allocs/op    vs base                  │
AllocatedStateSingleRead/claims=100-8             136.0 ± 3%     366.5 ± 2%  +169.49% (p=0.002 n=6)
AllocatedStateSingleRead/claims=5000-8           42.05k ± 0%    52.16k ± 0%   +24.05% (p=0.002 n=6)
AllocatedStateSingleRead/claims=20000-8          167.2k ± 0%    207.5k ± 0%   +24.07% (p=0.002 n=6)
AllocatedStateWriteHeavy/writes=100-8             957.0 ± 1%    1457.5 ± 1%   +52.30% (p=0.002 n=6)
AllocatedStateWriteHeavy/writes=1000-8           14.20k ± 2%    20.83k ± 1%   +46.70% (p=0.002 n=6)
AllocatedStateForkRevertChurn/claims=0-8          9.000 ± 0%     9.000 ± 0%         ~ (p=1.000 n=6) ¹
AllocatedStateForkRevertChurn/claims=10-8         18.00 ± 0%     23.00 ± 0%   +27.78% (p=0.002 n=6)
AllocatedStateForkRevertChurn/claims=1000-8       18.00 ± 0%     23.00 ± 0%   +27.78% (p=0.002 n=6)
AllocatedStateUnallocatedClaims/claims=5000-8    5.093k ± 0%   10.078k ± 0%   +97.88% (p=0.002 n=6)
AllocatedStateUnallocatedClaims/claims=20000-8   20.29k ± 0%    40.22k ± 0%   +98.27% (p=0.002 n=6)
SnapshotForkRevertNoDRA-8                         903.0 ± 0%     903.0 ± 0%         ~ (p=1.000 n=6) ¹
geomean                                          1.122k         1.626k        +44.95%
¹ all samples are equal

```
