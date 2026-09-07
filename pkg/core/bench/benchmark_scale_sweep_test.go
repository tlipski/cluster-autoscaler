/*
Copyright 2026 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package bench

import (
	"flag"
	"fmt"
	"strconv"
	"strings"
	"testing"
	"time"

	"sigs.k8s.io/cluster-autoscaler/pkg/config"
)

// sweepNodes selects the cluster sizes the sweeps below run at. Costs that scale with the
// number of nodes in the snapshot show up as a widening gap across these points; costs
// that scale only with the number of pods stay flat.
//
// The default stops at 2000 to keep a full run to a few minutes per side. Larger sizes are
// useful for confirming how a cost scales, but a single scale-up iteration at 5000 nodes
// places 250000 pods and runs for minutes, so ask for those explicitly.
var sweepNodes = flag.String("sweep-nodes", "250,500,1000,2000", "Comma-separated cluster sizes for BenchmarkScaleUpSweep and BenchmarkScaleDownSweep.")

func sweepNodeCounts(b *testing.B) []int {
	b.Helper()
	var counts []int
	for _, field := range strings.Split(*sweepNodes, ",") {
		field = strings.TrimSpace(field)
		if field == "" {
			continue
		}
		n, err := strconv.Atoi(field)
		if err != nil || n <= 0 {
			b.Fatalf("invalid --sweep-nodes entry %q: want a positive integer", field)
		}
		counts = append(counts, n)
	}
	if len(counts) == 0 {
		b.Fatalf("--sweep-nodes is empty")
	}
	return counts
}

// BenchmarkScaleUpSweep is BenchmarkRunOnceScaleUp parameterised by target cluster size.
// The node group starts empty and binpacking grows it to nodes, with 50 pending pods per
// node, so both the snapshot and the pending pod count scale together.
func BenchmarkScaleUpSweep(b *testing.B) {
	for _, nodes := range sweepNodeCounts(b) {
		b.Run(fmt.Sprintf("nodes=%d", nodes), func(b *testing.B) {
			s := scenario{
				setup:  setupScaleUp(nodes),
				verify: verifyTargetSize(nodes),
				config: func(opts *config.AutoscalingOptions) {
					opts.MaxNodesPerScaleUp = maxNGSize
					opts.ScaleUpFromZero = true
					// The sweep has to measure the time to do a fixed amount of work. Left at
					// the harness default of 60s, binpacking past ~2000 nodes hits the deadline
					// and stops early, which turns the benchmark into a measure of how much work
					// fits in 60s and lets the two sides being compared stop at different cluster
					// sizes. Sizes at or below 2000 never reach the default, so lifting it leaves
					// their results unchanged.
					opts.MaxNodeGroupBinpackingDuration = time.Hour
				},
			}
			s.run(b)
		})
	}
}

// BenchmarkScaleDownSweep is BenchmarkRunOnceScaleDown parameterised by cluster size.
// Each node starts with 40 pods at 40% utilisation, and 60% of the nodes drain away, so
// the snapshot, the pod count and the number of removal simulations all scale together.
func BenchmarkScaleDownSweep(b *testing.B) {
	for _, nodes := range sweepNodeCounts(b) {
		b.Run(fmt.Sprintf("nodes=%d", nodes), func(b *testing.B) {
			s := scenario{
				setup:  setupScaleDown60Percent(nodes),
				verify: verifyToBeDeleted(nodes * 60 / 100),
				config: func(opts *config.AutoscalingOptions) {
					opts.NodeGroupDefaults.ScaleDownUnneededTime = 0
					opts.MaxScaleDownParallelism = 2 * nodes
					opts.MaxDrainParallelism = 2 * nodes
					opts.ScaleDownDelayAfterAdd = 0
					opts.ScaleDownEnabled = true
					opts.ScaleDownNonEmptyCandidatesCount = 2 * nodes
					opts.ScaleDownUnreadyEnabled = true
					// As above - the removal simulations have to run to completion for the sizes
					// being compared to mean the same thing.
					opts.ScaleDownSimulationTimeout = time.Hour
				},
			}
			s.run(b)
		})
	}
}
