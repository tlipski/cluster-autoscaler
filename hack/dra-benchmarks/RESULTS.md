# Reference run

Produced by this harness on a dedicated GKE `c2d-standard-32` node (AMD EPYC
7B13, `GOMAXPROCS=8`), comparing `ae77a96` (benchmarks, no fix) against
`4123c81` (incremental `GatherAllocatedState`).

```
================ dra ================
pkg: sigs.k8s.io/cluster-autoscaler/pkg/estimator
cpu: AMD EPYC 7B13
                                                                  │   baseline   │             candidate              │
                                                                  │    sec/op    │   sec/op     vs base               │
BinpackingEstimateDRA/nodes=1000/claims=4000/pendingPods=200-8      520.33m ± 1%   97.03m ± 1%  -81.35% (p=0.002 n=6)
BinpackingEstimateDRA/nodes=5000/claims=20000/pendingPods=500-8       8.151 ± 2%    1.048 ± 1%  -87.14% (p=0.002 n=6)
BinpackingEstimateDRAProfiles/gpu8/exclusive-8                      390.82m ± 2%   83.32m ± 2%  -78.68% (p=0.002 n=6)
BinpackingEstimateDRAProfiles/gpu8/wholeNode-8                       288.9m ± 1%   121.3m ± 2%  -58.03% (p=0.002 n=6)
BinpackingEstimateDRAProfiles/mig56/fractional-8                    800.51m ± 1%   62.52m ± 5%  -92.19% (p=0.002 n=6)
BinpackingEstimateDRAProfiles/devices256/multiSlice-8               797.55m ± 2%   66.29m ± 9%  -91.69% (p=0.002 n=6)
BinpackingEstimateDRAProfiles/multiDriver/gpu+net+computeDomain-8   290.17m ± 1%   89.63m ± 2%  -69.11% (p=0.002 n=6)
BinpackingEstimateDRAProfiles/gpu8/sharedClaims-8                    62.61m ± 4%   20.95m ± 3%  -66.54% (p=0.002 n=6)
geomean                                                              522.5m        97.24m       -81.39%

                                                                  │   baseline    │              candidate              │
                                                                  │     B/op      │     B/op      vs base               │
BinpackingEstimateDRA/nodes=1000/claims=4000/pendingPods=200-8      312.08Mi ± 0%   81.66Mi ± 0%  -73.83% (p=0.002 n=6)
BinpackingEstimateDRA/nodes=5000/claims=20000/pendingPods=500-8     3132.5Mi ± 0%   792.1Mi ± 0%  -74.71% (p=0.002 n=6)
BinpackingEstimateDRAProfiles/gpu8/exclusive-8                      238.20Mi ± 0%   65.74Mi ± 0%  -72.40% (p=0.002 n=6)
BinpackingEstimateDRAProfiles/gpu8/wholeNode-8                      176.12Mi ± 0%   90.76Mi ± 0%  -48.47% (p=0.002 n=6)
BinpackingEstimateDRAProfiles/mig56/fractional-8                    358.01Mi ± 0%   39.23Mi ± 0%  -89.04% (p=0.002 n=6)
BinpackingEstimateDRAProfiles/devices256/multiSlice-8               446.21Mi ± 0%   34.09Mi ± 0%  -92.36% (p=0.002 n=6)
BinpackingEstimateDRAProfiles/multiDriver/gpu+net+computeDomain-8   162.52Mi ± 0%   53.33Mi ± 0%  -67.19% (p=0.002 n=6)
BinpackingEstimateDRAProfiles/gpu8/sharedClaims-8                    29.55Mi ± 0%   12.69Mi ± 0%  -57.07% (p=0.002 n=6)
geomean                                                              273.7Mi        65.75Mi       -75.98%

                                                                  │  baseline   │             candidate              │
                                                                  │  allocs/op  │  allocs/op   vs base               │
BinpackingEstimateDRA/nodes=1000/claims=4000/pendingPods=200-8      332.4k ± 1%   320.9k ± 1%   -3.46% (p=0.002 n=6)
BinpackingEstimateDRA/nodes=5000/claims=20000/pendingPods=500-8     3.582M ± 4%   3.171M ± 1%  -11.50% (p=0.002 n=6)
BinpackingEstimateDRAProfiles/gpu8/exclusive-8                      309.9k ± 0%   299.8k ± 0%   -3.27% (p=0.002 n=6)
BinpackingEstimateDRAProfiles/gpu8/wholeNode-8                      519.0k ± 0%   509.0k ± 0%   -1.92% (p=0.002 n=6)
BinpackingEstimateDRAProfiles/mig56/fractional-8                    244.4k ± 1%   236.5k ± 0%   -3.20% (p=0.002 n=6)
BinpackingEstimateDRAProfiles/devices256/multiSlice-8               208.7k ± 1%   200.7k ± 1%   -3.85% (p=0.002 n=6)
BinpackingEstimateDRAProfiles/multiDriver/gpu+net+computeDomain-8   545.0k ± 0%   540.2k ± 0%   -0.88% (p=0.002 n=6)
BinpackingEstimateDRAProfiles/gpu8/sharedClaims-8                   82.80k ± 2%   84.16k ± 1%   +1.64% (p=0.002 n=6)
geomean                                                             380.6k        367.8k        -3.37%

================ loop ================
pkg: sigs.k8s.io/cluster-autoscaler/pkg/core/bench
cpu: AMD EPYC 7B13
                      │  baseline   │             candidate              │
                      │   sec/op    │   sec/op     vs base               │
RunOnceScaleUp-8         10.41 ± 9%   10.39 ±  8%        ~ (p=0.699 n=6)
RunOnceScaleUpDRA-8     15.542 ± 2%   5.139 ± 11%  -66.93% (p=0.002 n=6)
RunOnceScaleDown-8       9.850 ± 3%   9.897 ±  5%        ~ (p=0.818 n=6)
RunOnceScaleDownDRA-8    6.385 ± 3%   1.710 ±  2%  -73.23% (p=0.002 n=6)
geomean                  10.04        5.483        -45.41%

                      │   baseline   │              candidate               │
                      │     B/op     │     B/op       vs base               │
RunOnceScaleUp-8        6.574Gi ± 0%   6.581Gi ±  0%        ~ (p=0.485 n=6)
RunOnceScaleUpDRA-8     9.924Gi ± 0%   3.551Gi ±  1%  -64.22% (p=0.002 n=6)
RunOnceScaleDown-8      7.127Gi ± 2%   7.130Gi ±  2%        ~ (p=0.699 n=6)
RunOnceScaleDownDRA-8   3.000Gi ± 8%   1.248Gi ± 21%  -58.41% (p=0.002 n=6)
geomean                 6.111Gi        3.797Gi        -37.86%

                      │  baseline   │              candidate              │
                      │  allocs/op  │  allocs/op    vs base               │
RunOnceScaleUp-8        51.25M ± 0%   51.32M ±  0%        ~ (p=0.485 n=6)
RunOnceScaleUpDRA-8     40.46M ± 1%   40.03M ±  1%   -1.07% (p=0.015 n=6)
RunOnceScaleDown-8      14.55M ± 5%   14.56M ±  5%        ~ (p=0.937 n=6)
RunOnceScaleDownDRA-8   11.72M ± 9%   10.48M ± 11%  -10.63% (p=0.002 n=6)
geomean                 24.39M        23.66M         -2.99%

================ nodra ================
pkg: sigs.k8s.io/cluster-autoscaler/pkg/estimator
cpu: AMD EPYC 7B13
                     │  baseline  │          candidate          │
                     │   sec/op   │   sec/op    vs base         │
BinpackingEstimate-8   3.883 ± 0%   3.877 ± 1%  ~ (p=0.240 n=6)

                     │   baseline   │           candidate           │
                     │     B/op     │     B/op      vs base         │
BinpackingEstimate-8   2.700Gi ± 0%   2.700Gi ± 0%  ~ (p=1.000 n=6)

                     │  baseline   │          candidate           │
                     │  allocs/op  │  allocs/op   vs base         │
BinpackingEstimate-8   4.418M ± 0%   4.418M ± 0%  ~ (p=0.818 n=6)

================ profiles ================
pkg: sigs.k8s.io/cluster-autoscaler/pkg/estimator
cpu: AMD EPYC 7B13
                                                                  │   baseline   │              candidate              │
                                                                  │    sec/op    │    sec/op     vs base               │
BinpackingEstimateDRAProfiles/gpu8/exclusive-8                      389.53m ± 1%   85.38m ±  2%  -78.08% (p=0.002 n=6)
BinpackingEstimateDRAProfiles/gpu8/wholeNode-8                       288.8m ± 1%   124.4m ±  1%  -56.92% (p=0.002 n=6)
BinpackingEstimateDRAProfiles/mig56/fractional-8                    805.02m ± 2%   63.81m ±  7%  -92.07% (p=0.002 n=6)
BinpackingEstimateDRAProfiles/devices256/multiSlice-8               795.96m ± 2%   55.34m ± 15%  -93.05% (p=0.002 n=6)
BinpackingEstimateDRAProfiles/multiDriver/gpu+net+computeDomain-8   290.69m ± 1%   89.59m ±  5%  -69.18% (p=0.002 n=6)
BinpackingEstimateDRAProfiles/gpu8/sharedClaims-8                    63.91m ± 2%   23.67m ±  9%  -62.96% (p=0.002 n=6)
geomean                                                              332.0m        65.59m        -80.25%

                                                                  │   baseline    │              candidate              │
                                                                  │     B/op      │     B/op      vs base               │
BinpackingEstimateDRAProfiles/gpu8/exclusive-8                      238.18Mi ± 0%   65.76Mi ± 0%  -72.39% (p=0.002 n=6)
BinpackingEstimateDRAProfiles/gpu8/wholeNode-8                      176.08Mi ± 0%   90.78Mi ± 0%  -48.44% (p=0.002 n=6)
BinpackingEstimateDRAProfiles/mig56/fractional-8                    358.02Mi ± 0%   39.26Mi ± 0%  -89.03% (p=0.002 n=6)
BinpackingEstimateDRAProfiles/devices256/multiSlice-8               446.22Mi ± 0%   34.15Mi ± 0%  -92.35% (p=0.002 n=6)
BinpackingEstimateDRAProfiles/multiDriver/gpu+net+computeDomain-8   162.70Mi ± 0%   53.35Mi ± 0%  -67.21% (p=0.002 n=6)
BinpackingEstimateDRAProfiles/gpu8/sharedClaims-8                    29.51Mi ± 0%   12.67Mi ± 0%  -57.07% (p=0.002 n=6)
geomean                                                              178.3Mi        41.90Mi       -76.51%

                                                                  │  baseline   │             candidate             │
                                                                  │  allocs/op  │  allocs/op   vs base              │
BinpackingEstimateDRAProfiles/gpu8/exclusive-8                      309.2k ± 1%   300.4k ± 1%  -2.84% (p=0.002 n=6)
BinpackingEstimateDRAProfiles/gpu8/wholeNode-8                      517.9k ± 0%   509.7k ± 0%  -1.58% (p=0.002 n=6)
BinpackingEstimateDRAProfiles/mig56/fractional-8                    244.4k ± 1%   237.4k ± 0%  -2.85% (p=0.002 n=6)
BinpackingEstimateDRAProfiles/devices256/multiSlice-8               209.0k ± 1%   202.9k ± 0%  -2.92% (p=0.002 n=6)
BinpackingEstimateDRAProfiles/multiDriver/gpu+net+computeDomain-8   545.5k ± 1%   540.0k ± 0%  -1.02% (p=0.002 n=6)
BinpackingEstimateDRAProfiles/gpu8/sharedClaims-8                   81.72k ± 0%   83.66k ± 0%  +2.37% (p=0.002 n=6)
geomean                                                             267.3k        263.3k       -1.49%

================ scaledowndra ================
pkg: sigs.k8s.io/cluster-autoscaler/pkg/core/bench
cpu: AMD EPYC 7B13
                      │  baseline  │             candidate              │
                      │   sec/op   │   sec/op    vs base                │
RunOnceScaleDownDRA-8   6.532 ± 3%   1.776 ± 9%  -72.81% (p=0.000 n=10)

                      │   baseline   │              candidate               │
                      │     B/op     │     B/op      vs base                │
RunOnceScaleDownDRA-8   2.985Gi ± 1%   1.247Gi ± 0%  -58.24% (p=0.000 n=10)

                      │  baseline   │              candidate              │
                      │  allocs/op  │  allocs/op   vs base                │
RunOnceScaleDownDRA-8   11.67M ± 1%   10.47M ± 0%  -10.33% (p=0.000 n=10)

================ store ================
pkg: sigs.k8s.io/cluster-autoscaler/pkg/simulator/clustersnapshot/store
cpu: AMD EPYC 7B13
                                            │   baseline   │             candidate              │
                                            │    sec/op    │    sec/op     vs base              │
BuildNodeInfoList/fork_add_1000_to_1000-8     44.42µ ± 29%   44.26µ ± 35%       ~ (p=0.818 n=6)
BuildNodeInfoList/fork_add_1000_to_5000-8     54.96µ ±  7%   54.23µ ±  2%       ~ (p=0.937 n=6)
BuildNodeInfoList/fork_add_1000_to_15000-8    233.4µ ± 50%   235.0µ ± 31%       ~ (p=0.699 n=6)
BuildNodeInfoList/fork_add_1000_to_100000-8   809.5µ ± 67%   806.7µ ± 91%       ~ (p=1.000 n=6)
BuildNodeInfoList/base_1000-8                 13.43µ ± 20%   13.18µ ± 25%       ~ (p=0.240 n=6)
BuildNodeInfoList/base_5000-8                 119.4µ ± 13%   113.9µ ± 37%       ~ (p=0.818 n=6)
BuildNodeInfoList/base_15000-8                281.3µ ± 24%   257.8µ ± 30%       ~ (p=0.699 n=6)
BuildNodeInfoList/base_100000-8               1.456m ± 37%   1.466m ± 37%       ~ (p=0.240 n=6)
geomean                                       153.2µ         150.1µ        -1.97%

                                            │   baseline   │              candidate               │
                                            │     B/op     │     B/op      vs base                │
BuildNodeInfoList/fork_add_1000_to_1000-8     32.33Ki ± 0%   32.33Ki ± 0%       ~ (p=1.000 n=6)
BuildNodeInfoList/fork_add_1000_to_5000-8     97.62Ki ± 0%   97.62Ki ± 0%       ~ (p=1.000 n=6) ¹
BuildNodeInfoList/fork_add_1000_to_15000-8    260.8Ki ± 0%   260.8Ki ± 0%       ~ (p=1.000 n=6) ¹
BuildNodeInfoList/fork_add_1000_to_100000-8   1.578Mi ± 0%   1.578Mi ± 0%       ~ (p=1.000 n=6)
BuildNodeInfoList/base_1000-8                 16.02Ki ± 0%   16.02Ki ± 0%       ~ (p=1.000 n=6) ¹
BuildNodeInfoList/base_5000-8                 80.02Ki ± 0%   80.02Ki ± 0%       ~ (p=1.000 n=6) ¹
BuildNodeInfoList/base_15000-8                240.0Ki ± 0%   240.0Ki ± 0%       ~ (p=1.000 n=6) ¹
BuildNodeInfoList/base_100000-8               1.531Mi ± 0%   1.531Mi ± 0%       ~ (p=1.000 n=6) ¹
geomean                                       168.2Ki        168.2Ki       +0.00%
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
