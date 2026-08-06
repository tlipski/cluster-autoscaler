# Reference run

Produced by this harness on a dedicated GKE `c2d-standard-32` node (AMD EPYC
7B13, `GOMAXPROCS=8`), comparing `61fec2d` (benchmarks, no fix) against
`3a3d0b0` (incremental `GatherAllocatedState`).

```
================ dra ================
pkg: sigs.k8s.io/cluster-autoscaler/pkg/estimator
cpu: AMD EPYC 7B13
                                                                │   baseline   │             candidate              │
                                                                │    sec/op    │   sec/op     vs base               │
BinpackingEstimateDRA/nodes=1000/claims=4000/pendingPods=200-8    533.96m ± 1%   98.21m ± 1%  -81.61% (p=0.002 n=6)
BinpackingEstimateDRA/nodes=5000/claims=20000/pendingPods=500-8     8.203 ± 2%    1.053 ± 2%  -87.17% (p=0.002 n=6)
geomean                                                             2.093        321.5m       -84.64%

                                                                │   baseline    │              candidate              │
                                                                │     B/op      │     B/op      vs base               │
BinpackingEstimateDRA/nodes=1000/claims=4000/pendingPods=200-8    312.10Mi ± 0%   81.66Mi ± 0%  -73.84% (p=0.002 n=6)
BinpackingEstimateDRA/nodes=5000/claims=20000/pendingPods=500-8   3132.3Mi ± 0%   791.9Mi ± 0%  -74.72% (p=0.002 n=6)
geomean                                                            988.7Mi        254.3Mi       -74.28%

                                                                │  baseline   │             candidate              │
                                                                │  allocs/op  │  allocs/op   vs base               │
BinpackingEstimateDRA/nodes=1000/claims=4000/pendingPods=200-8    332.2k ± 0%   320.8k ± 1%   -3.41% (p=0.002 n=6)
BinpackingEstimateDRA/nodes=5000/claims=20000/pendingPods=500-8   3.571M ± 4%   3.164M ± 1%  -11.39% (p=0.002 n=6)
geomean                                                           1.089M        1.008M        -7.49%

================ loop ================
pkg: sigs.k8s.io/cluster-autoscaler/pkg/core/bench
cpu: AMD EPYC 7B13
                      │  baseline   │             candidate             │
                      │   sec/op    │   sec/op    vs base               │
RunOnceScaleUp-8         10.39 ± 9%   10.41 ± 8%        ~ (p=0.589 n=6)
RunOnceScaleUpDRA-8     15.554 ± 2%   5.182 ± 9%  -66.69% (p=0.002 n=6)
RunOnceScaleDown-8       9.893 ± 2%   9.983 ± 4%        ~ (p=0.394 n=6)
RunOnceScaleDownDRA-8    6.453 ± 4%   1.749 ± 1%  -72.89% (p=0.002 n=6)
geomean                  10.08        5.540       -45.03%

                      │   baseline   │              candidate               │
                      │     B/op     │     B/op       vs base               │
RunOnceScaleUp-8        6.580Gi ± 0%   6.577Gi ±  1%        ~ (p=0.818 n=6)
RunOnceScaleUpDRA-8     9.915Gi ± 0%   3.557Gi ±  1%  -64.12% (p=0.002 n=6)
RunOnceScaleDown-8      7.126Gi ± 2%   7.126Gi ±  2%        ~ (p=0.818 n=6)
RunOnceScaleDownDRA-8   2.987Gi ± 8%   1.249Gi ± 20%  -58.20% (p=0.002 n=6)
geomean                 6.105Gi        3.798Gi        -37.78%

                      │  baseline   │              candidate              │
                      │  allocs/op  │  allocs/op    vs base               │
RunOnceScaleUp-8        51.30M ± 0%   51.27M ±  1%        ~ (p=0.818 n=6)
RunOnceScaleUpDRA-8     40.35M ± 1%   40.10M ±  1%        ~ (p=0.310 n=6)
RunOnceScaleDown-8      14.55M ± 5%   14.55M ±  5%        ~ (p=0.937 n=6)
RunOnceScaleDownDRA-8   11.70M ± 9%   10.48M ± 10%  -10.40% (p=0.002 n=6)
geomean                 24.36M        23.66M         -2.88%

================ nodra ================
pkg: sigs.k8s.io/cluster-autoscaler/pkg/estimator
cpu: AMD EPYC 7B13
                     │  baseline  │          candidate          │
                     │   sec/op   │   sec/op    vs base         │
BinpackingEstimate-8   3.885 ± 1%   3.878 ± 0%  ~ (p=0.310 n=6)

                     │   baseline   │           candidate           │
                     │     B/op     │     B/op      vs base         │
BinpackingEstimate-8   2.700Gi ± 0%   2.700Gi ± 0%  ~ (p=0.310 n=6)

                     │  baseline   │          candidate           │
                     │  allocs/op  │  allocs/op   vs base         │
BinpackingEstimate-8   4.418M ± 0%   4.418M ± 0%  ~ (p=0.180 n=6)

================ scaledowndra ================
pkg: sigs.k8s.io/cluster-autoscaler/pkg/core/bench
cpu: AMD EPYC 7B13
                      │  baseline  │             candidate              │
                      │   sec/op   │   sec/op    vs base                │
RunOnceScaleDownDRA-8   6.563 ± 2%   1.771 ± 9%  -73.02% (p=0.000 n=10)

                      │   baseline   │              candidate               │
                      │     B/op     │     B/op      vs base                │
RunOnceScaleDownDRA-8   2.983Gi ± 1%   1.248Gi ± 0%  -58.17% (p=0.000 n=10)

                      │  baseline   │              candidate              │
                      │  allocs/op  │  allocs/op   vs base                │
RunOnceScaleDownDRA-8   11.68M ± 1%   10.48M ± 0%  -10.25% (p=0.000 n=10)

================ store ================
pkg: sigs.k8s.io/cluster-autoscaler/pkg/simulator/clustersnapshot/store
cpu: AMD EPYC 7B13
                                            │   baseline   │              candidate              │
                                            │    sec/op    │    sec/op     vs base               │
BuildNodeInfoList/fork_add_1000_to_1000-8     48.85µ ± 17%   46.16µ ± 13%        ~ (p=0.310 n=6)
BuildNodeInfoList/fork_add_1000_to_5000-8     54.16µ ± 21%   54.95µ ±  7%        ~ (p=0.394 n=6)
BuildNodeInfoList/fork_add_1000_to_15000-8    237.7µ ± 46%   227.2µ ± 44%        ~ (p=0.240 n=6)
BuildNodeInfoList/fork_add_1000_to_100000-8   811.5µ ±  1%   811.3µ ± 96%        ~ (p=1.000 n=6)
BuildNodeInfoList/base_1000-8                 13.54µ ±  3%   13.44µ ±  3%        ~ (p=0.699 n=6)
BuildNodeInfoList/base_5000-8                 122.8µ ±  3%   103.9µ ± 31%  -15.37% (p=0.041 n=6)
BuildNodeInfoList/base_15000-8                336.5µ ± 24%   295.0µ ± 23%        ~ (p=0.589 n=6)
BuildNodeInfoList/base_100000-8               1.480m ± 24%   1.492m ± 29%        ~ (p=0.589 n=6)
geomean                                       159.7µ         152.2µ         -4.70%

                                            │   baseline   │              candidate               │
                                            │     B/op     │     B/op      vs base                │
BuildNodeInfoList/fork_add_1000_to_1000-8     32.33Ki ± 0%   32.33Ki ± 0%       ~ (p=0.848 n=6)
BuildNodeInfoList/fork_add_1000_to_5000-8     97.62Ki ± 0%   97.62Ki ± 0%       ~ (p=1.000 n=6) ¹
BuildNodeInfoList/fork_add_1000_to_15000-8    260.8Ki ± 0%   260.8Ki ± 0%       ~ (p=1.000 n=6) ¹
BuildNodeInfoList/fork_add_1000_to_100000-8   1.578Mi ± 0%   1.578Mi ± 0%       ~ (p=1.000 n=6) ¹
BuildNodeInfoList/base_1000-8                 16.02Ki ± 0%   16.02Ki ± 0%       ~ (p=1.000 n=6) ¹
BuildNodeInfoList/base_5000-8                 80.02Ki ± 0%   80.02Ki ± 0%       ~ (p=1.000 n=6)
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
