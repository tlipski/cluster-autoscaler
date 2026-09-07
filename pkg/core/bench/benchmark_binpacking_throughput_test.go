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
	"context"
	"flag"
	"fmt"
	"testing"
	"time"

	"sigs.k8s.io/cluster-autoscaler/pkg/config"
	"sigs.k8s.io/cluster-autoscaler/pkg/test/integration"
)

// binpackingDeadline is the per-NodeGroup binpacking budget the throughput benchmark runs
// under. It defaults to the --max-nodegroup-binpacking-duration default, because that is
// what a cluster actually runs with.
var binpackingDeadline = flag.Duration("binpacking-deadline", 10*time.Second, "Per-NodeGroup binpacking budget for BenchmarkBinpackingThroughput.")

// BenchmarkBinpackingThroughput measures how far binpacking gets inside a fixed deadline,
// rather than how long it takes to finish.
//
// Above roughly 2000 nodes a scale-up does not finish within the default budget:
// thresholdBasedEstimationLimiter.PermissionToAddNode stops granting nodes once the
// deadline passes, and CA scales up by however much it managed. Wall-clock time is then
// pinned to the deadline and says nothing, while the useful output - how many nodes CA
// decided to add - is what changes. A faster binpacking loop does not return sooner, it
// gets further, and a cluster that needed 5000 nodes gets closer to 5000 in one pass
// instead of taking several loops to trickle there.
//
// target sets how many pending pods exist (50 per node), and needs to stay comfortably
// above what either side can place, or the run stops being deadline-bound and the
// comparison silently turns back into a timing one. nodes/op reports what was placed.
func BenchmarkBinpackingThroughput(b *testing.B) {
	for _, target := range sweepNodeCounts(b) {
		b.Run(fmt.Sprintf("target=%d", target), func(b *testing.B) {
			placed := 0
			s := scenario{
				setup: setupScaleUp(target),
				verify: func(clusterFakes *integration.FakeSet) error {
					ng := clusterFakes.CloudProvider.GetNodeGroup(ngName)
					if ng == nil {
						return fmt.Errorf("nodegroup %s not found", ngName)
					}
					targetSize, err := ng.TargetSize(context.Background())
					if err != nil {
						return err
					}
					// Deliberately not asserting the target was reached - being cut short is
					// the condition under test.
					placed += targetSize
					return nil
				},
				config: func(opts *config.AutoscalingOptions) {
					opts.MaxNodesPerScaleUp = maxNGSize
					opts.ScaleUpFromZero = true
					opts.MaxNodeGroupBinpackingDuration = *binpackingDeadline
				},
			}
			s.run(b)
			b.ReportMetric(float64(placed)/float64(b.N), "nodes/op")
		})
	}
}
