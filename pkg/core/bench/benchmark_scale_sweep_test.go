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
	"fmt"
	"testing"
	"time"

	"sigs.k8s.io/cluster-autoscaler/pkg/config"
)

// sweepNodeCounts is the cluster sizes the sweeps below run at. Costs that scale with the
// number of nodes in the snapshot show up as a widening gap across these points; costs
// that scale only with the number of pods stay flat.
var sweepNodeCounts = []int{250, 500, 1000, 2000}

// BenchmarkScaleUpSweep is BenchmarkRunOnceScaleUp parameterised by target cluster size.
// The node group starts empty and binpacking grows it to nodes, with 50 pending pods per
// node, so both the snapshot and the pending pod count scale together.
func BenchmarkScaleUpSweep(b *testing.B) {
	for _, nodes := range sweepNodeCounts {
		b.Run(fmt.Sprintf("nodes=%d", nodes), func(b *testing.B) {
			s := scenario{
				setup:  setupScaleUp(nodes),
				verify: verifyTargetSize(nodes),
				config: func(opts *config.AutoscalingOptions) {
					opts.MaxNodesPerScaleUp = maxNGSize
					opts.ScaleUpFromZero = true
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
	for _, nodes := range sweepNodeCounts {
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
					opts.ScaleDownSimulationTimeout = 60 * time.Second
				},
			}
			s.run(b)
		})
	}
}
