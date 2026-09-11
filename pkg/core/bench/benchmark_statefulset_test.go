/*
Copyright The Kubernetes Authors.

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
	"fmt"
	"testing"
	"time"

	apiv1 "k8s.io/api/core/v1"
	storagev1 "k8s.io/api/storage/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"

	"sigs.k8s.io/cluster-autoscaler/pkg/cloudprovider"
	testprovider "sigs.k8s.io/cluster-autoscaler/pkg/cloudprovider/test"
	"sigs.k8s.io/cluster-autoscaler/pkg/config"
	"sigs.k8s.io/cluster-autoscaler/pkg/simulator/framework"
	"sigs.k8s.io/cluster-autoscaler/pkg/test/integration"
	. "sigs.k8s.io/cluster-autoscaler/pkg/utils/test"
)

// StatefulSet pods are the shape that defeats pod equivalence grouping entirely.
// Groups are keyed on the controller and then on (labels, spec), and
// sanitizePodSpec drops projected volumes, hostname and env but not
// PersistentVolumeClaim volumes - so the per-ordinal claim a volumeClaimTemplate
// produces ("data-<set>-<ordinal>") leaves no two replicas semantically equal.
// Every replica therefore becomes its own equivalence group, and the
// SchedulablePodGroups sweep, which runs one predicate check per
// (equivalence group, node group) pair, becomes O(pending pods x node groups)
// rather than O(workloads x node groups).
//
// This is not a synthetic worst case. Databases, Kafka and Elasticsearch are all
// StatefulSets with volumeClaimTemplates.
const (
	stsStorageClassName = "bench-sc"
	stsFleetNGName      = "sts-fleet"
	// Independent of maxNGSize: a 10k fleet would otherwise sit exactly at its
	// own group's ceiling.
	stsMaxNGSize = 200000
)

// statefulSetPod builds one replica of its own StatefulSet, carrying the PVC that
// set's volumeClaimTemplate would have produced.
func statefulSetPod(setName string, ordinal int, cpu, mem int64, opts ...func(*apiv1.Pod)) *apiv1.Pod {
	name := fmt.Sprintf("%s-%d", setName, ordinal)
	pod := BuildTestPod(name, cpu, mem, opts...)
	pod.OwnerReferences = []metav1.OwnerReference{{
		APIVersion: "apps/v1",
		Kind:       "StatefulSet",
		Name:       setName,
		UID:        types.UID("uid-" + setName),
		Controller: ptr.To(true),
	}}
	pod.Labels = map[string]string{"app": setName}
	pod.Spec.Hostname = name
	pod.Spec.Volumes = []apiv1.Volume{{
		Name: "data",
		VolumeSource: apiv1.VolumeSource{
			PersistentVolumeClaim: &apiv1.PersistentVolumeClaimVolumeSource{
				ClaimName: fmt.Sprintf("data-%s", name),
			},
		},
	}}
	return pod
}

// stsClaim is the PVC the volumeClaimTemplate produces for a replica. Bound ones
// belong to pods already placed on the fleet; unbound ones use
// WaitForFirstConsumer, which is what makes the scheduler's VolumeBinding plugin
// actually run for a pending StatefulSet replica.
func stsClaim(podName string, bound bool) *apiv1.PersistentVolumeClaim {
	pvc := &apiv1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("data-%s", podName),
			Namespace: "default",
		},
		Spec: apiv1.PersistentVolumeClaimSpec{
			StorageClassName: ptr.To(stsStorageClassName),
			AccessModes:      []apiv1.PersistentVolumeAccessMode{apiv1.ReadWriteOnce},
		},
		Status: apiv1.PersistentVolumeClaimStatus{Phase: apiv1.ClaimPending},
	}
	if bound {
		pvc.Spec.VolumeName = fmt.Sprintf("pv-%s", podName)
		pvc.Status.Phase = apiv1.ClaimBound
	}
	return pvc
}

// setupScaleUpStatefulSets builds a fleet of fleetNodes nodes each already running
// podsPerNode StatefulSet replicas, offers nodeGroups groups to grow into, and
// leaves pendingPods further replicas unschedulable so the scale-up path runs.
//
// Because every replica is its own equivalence group (see the comment above), the
// sweep this produces is pendingPods x nodeGroups predicate checks per scale-up.
func setupScaleUpStatefulSets(fleetNodes, podsPerNode, pendingPods, nodeGroups int) func(*integration.FakeSet) error {
	return func(clusterFakes *integration.FakeSet) error {
		ctx := context.Background()

		// newClusterFakes caps the cluster at nodeCPU*maxNGSize cores, which a
		// 10k-node fleet reaches exactly. Left alone, every expansion option is
		// rejected on the resource limit and scale-up returns before the sweep.
		headroom := int64(fleetNodes + pendingPods + 1000)
		clusterFakes.CloudProvider.SetResourceLimit(cloudprovider.ResourceNameCores, 0, headroom*nodeCPU)
		clusterFakes.CloudProvider.SetResourceLimit(cloudprovider.ResourceNameMemory, 0, headroom*nodeMem)

		// WaitForFirstConsumer is what StatefulSets on a dynamically provisioned
		// cluster use, and it is what keeps VolumeBinding in the predicate path
		// instead of short-circuiting on an already-bound claim.
		sc := &storagev1.StorageClass{
			ObjectMeta:        metav1.ObjectMeta{Name: stsStorageClassName},
			Provisioner:       "bench.csi.example.com",
			VolumeBindingMode: ptr.To(storagev1.VolumeBindingWaitForFirstConsumer),
		}
		if _, err := clusterFakes.KubeClient.StorageV1().StorageClasses().Create(ctx, sc, metav1.CreateOptions{}); err != nil {
			return err
		}

		nTemplate := BuildTestNode("n-template", nodeCPU, nodeMem)
		SetNodeReadyState(nTemplate, true, time.Now())

		// One group holds the existing fleet, the rest are empty targets. Every
		// group is a candidate the sweep has to evaluate against every pending
		// replica, which is the cost under test.
		fleetNG := clusterFakes.CloudProvider.AddNodeGroup(stsFleetNGName,
			testprovider.WithNodes(nTemplate, fleetNodes),
			testprovider.WithNGSize(0, stsMaxNGSize),
		)
		for g := range nodeGroups {
			clusterFakes.CloudProvider.AddNodeGroup(fmt.Sprintf("sts-ng-%d", g),
				testprovider.WithTemplate(framework.NewNodeInfo(nTemplate, nil)),
				testprovider.WithNGSize(0, stsMaxNGSize),
			)
		}

		// A CSINode per fleet node, so the CSI snapshot carries the whole fleet
		// rather than sitting empty - that snapshot is a PatchSet too, and an
		// empty one exercises none of it.
		for i := range fleetNodes {
			nodeName := fmt.Sprintf("%s-node-%d", fleetNG.Id(), i)
			csiNode := &storagev1.CSINode{
				ObjectMeta: metav1.ObjectMeta{Name: nodeName},
				Spec: storagev1.CSINodeSpec{Drivers: []storagev1.CSINodeDriver{{
					Name:   "bench.csi.example.com",
					NodeID: nodeName,
					// Absent limit means unlimited, so volume-count limits never
					// become the reason a pod does not fit.
					Allocatable: &storagev1.VolumeNodeResources{Count: ptr.To(int32(256))},
				}}},
			}
			if _, err := clusterFakes.KubeClient.StorageV1().CSINodes().Create(ctx, csiNode, metav1.CreateOptions{}); err != nil {
				return err
			}
		}

		// The fleet's standing load. Each pod is its own StatefulSet, pinned to a
		// node, with its claim already bound.
		//
		// The fleet is filled to within 1% of capacity on purpose. Leave real room
		// on it and filterOutSchedulable places the pending replicas on existing
		// nodes, scale-up never runs, and the sweep this benchmark exists to
		// measure never happens.
		fleetCPU := int64(nodeCPU/podsPerNode - nodeCPU/100)
		fleetMem := int64(nodeMem/podsPerNode - nodeMem/100)
		for i := range fleetNodes * podsPerNode {
			nodeName := fmt.Sprintf("%s-node-%d", fleetNG.Id(), i%fleetNodes)
			setName := fmt.Sprintf("fleet-sts-%d", i)
			pod := statefulSetPod(setName, 0, fleetCPU, fleetMem, WithNodeName(nodeName))
			clusterFakes.K8s.AddPod(pod)
			if _, err := clusterFakes.KubeClient.CoreV1().PersistentVolumeClaims("default").Create(ctx, stsClaim(pod.Name, true), metav1.CreateOptions{}); err != nil {
				return err
			}
		}

		// The pending burst. Sized to need a whole node each so the estimator has
		// real work to do rather than packing them all onto one simulated node.
		pendingCPU := int64(nodeCPU / 2)
		pendingMem := int64(nodeMem / 2)
		for i := range pendingPods {
			setName := fmt.Sprintf("pending-sts-%d", i)
			pod := statefulSetPod(setName, 0, pendingCPU, pendingMem, MarkUnschedulable())
			clusterFakes.K8s.AddPod(pod)
			if _, err := clusterFakes.KubeClient.CoreV1().PersistentVolumeClaims("default").Create(ctx, stsClaim(pod.Name, false), metav1.CreateOptions{}); err != nil {
				return err
			}
		}

		return nil
	}
}

// verifyScaledUpBeyond checks that the loop actually decided to add nodes, summed
// over every group. A scenario where the pending replicas turn out to fit on the
// existing fleet still runs to completion and still reports a time - it just
// measures the wrong thing, silently, because scale-up never ran.
func verifyScaledUpBeyond(fleetNodes int) func(*integration.FakeSet) error {
	return func(clusterFakes *integration.FakeSet) error {
		total := 0
		for _, ng := range clusterFakes.CloudProvider.NodeGroups(context.Background()) {
			size, err := ng.TargetSize(context.Background())
			if err != nil {
				return err
			}
			total += size
		}
		if total <= fleetNodes {
			return fmt.Errorf("no scale-up happened: total target size %d, fleet was %d - the pending pods most likely fit on the existing fleet", total, fleetNodes)
		}
		return nil
	}
}

// BenchmarkRunOnceStatefulSets measures a RunOnce over a cluster whose workload is
// StatefulSets, at the scale where the SchedulablePodGroups sweep dominates.
//
// The configurations vary the two factors the sweep is the product of - pending
// replicas (each its own equivalence group) and node groups - against a fleet
// large enough that the snapshot itself is not free.
func BenchmarkRunOnceStatefulSets(b *testing.B) {
	configurations := map[string]struct {
		fleetNodes  int
		podsPerNode int
		pendingPods int
		nodeGroups  int
	}{
		// The shape asked about: 10k nodes, two StatefulSet replicas each.
		"fleet10000x2/pending500/ng10":  {fleetNodes: 10000, podsPerNode: 2, pendingPods: 500, nodeGroups: 10},
		"fleet10000x2/pending2000/ng10": {fleetNodes: 10000, podsPerNode: 2, pendingPods: 2000, nodeGroups: 10},
		"fleet10000x2/pending500/ng50":  {fleetNodes: 10000, podsPerNode: 2, pendingPods: 500, nodeGroups: 50},
		// Smaller fleet, to separate snapshot size from sweep size.
		"fleet1000x2/pending500/ng10": {fleetNodes: 1000, podsPerNode: 2, pendingPods: 500, nodeGroups: 10},
		"fleet1000x2/pending500/ng50": {fleetNodes: 1000, podsPerNode: 2, pendingPods: 500, nodeGroups: 50},
	}

	for name, cfg := range configurations {
		b.Run(name, func(b *testing.B) {
			s := scenario{
				setup: setupScaleUpStatefulSets(cfg.fleetNodes, cfg.podsPerNode, cfg.pendingPods, cfg.nodeGroups),
				// Without this the benchmark cannot tell "the sweep ran and was
				// slow" from "nothing was unschedulable so nothing happened",
				// which is exactly the way this scenario fails silently.
				verify: verifyScaledUpBeyond(cfg.fleetNodes),
				config: func(opts *config.AutoscalingOptions) {
					opts.MaxNodesPerScaleUp = stsMaxNGSize
					opts.ScaleUpFromZero = true
					// Room above the fleet for the burst. Leaving this at
					// maxNGSize means a 10k fleet is already at the cluster-wide
					// cap, GetCappedNewNodeCount refuses every option, and the
					// scale-up path returns before the sweep runs.
					opts.MaxNodesTotal = cfg.fleetNodes + cfg.pendingPods + 1000
				},
			}
			s.run(b)
		})
	}
}
