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

package store

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	schedulerinterface "k8s.io/kube-scheduler/framework"
	"sigs.k8s.io/cluster-autoscaler/pkg/simulator/framework"
	. "sigs.k8s.io/cluster-autoscaler/pkg/utils/test"
)

func schedNodeNames(nodeInfos []schedulerinterface.NodeInfo) []string {
	names := make([]string, 0, len(nodeInfos))
	for _, nodeInfo := range nodeInfos {
		names = append(names, nodeInfo.Node().Name)
	}
	return names
}

// antiAffinityPod returns a pod with a required hostname-scoped anti-affinity term.
func antiAffinityPod(name, nodeName string) *framework.PodInfo {
	pod := BuildTestPod(name, 100, 100,
		WithLabels(map[string]string{"app": "spread"}),
		WithPodHostnameAntiAffinity(map[string]string{"app": "spread"}),
	)
	pod.Spec.NodeName = nodeName
	return framework.NewPodInfo(pod, nil)
}

// plainPod returns a pod with no affinity constraints at all.
func plainPod(name, nodeName string) *framework.PodInfo {
	pod := BuildTestPod(name, 100, 100)
	pod.Spec.NodeName = nodeName
	return framework.NewPodInfo(pod, nil)
}

// TestHavePodsWithRequiredAntiAffinityListEmpty covers the common case, where no pod in
// the cluster uses anti-affinity. The empty answer has to be cached, not recomputed.
func TestHavePodsWithRequiredAntiAffinityListEmpty(t *testing.T) {
	deltaStore, names := storeWithNodes(t, 6)
	assert.NoError(t, deltaStore.StorePodInfo(plainPod("p-0", names[0]), names[0]))

	list, err := deltaStore.NodeInfos().HavePodsWithRequiredAntiAffinityList()
	assert.NoError(t, err)
	assert.Empty(t, list)
	assert.True(t, deltaStore.data.havePodsWithRequiredAntiAffinityBuilt,
		"empty result must be recorded as built, otherwise every call rescans")

	// A second call must be served from the cache rather than rescanning.
	again, err := deltaStore.NodeInfos().HavePodsWithRequiredAntiAffinityList()
	assert.NoError(t, err)
	assert.Empty(t, again)
}

// TestHavePodsWithRequiredAntiAffinityListNonEmpty checks the filtered result itself, and
// that adding and removing anti-affinity pods invalidates it.
func TestHavePodsWithRequiredAntiAffinityListNonEmpty(t *testing.T) {
	deltaStore, names := storeWithNodes(t, 6)

	assert.NoError(t, deltaStore.StorePodInfo(antiAffinityPod("aa-1", names[1]), names[1]))
	assert.NoError(t, deltaStore.StorePodInfo(antiAffinityPod("aa-4", names[4]), names[4]))
	assert.NoError(t, deltaStore.StorePodInfo(plainPod("p-2", names[2]), names[2]))

	list, err := deltaStore.NodeInfos().HavePodsWithRequiredAntiAffinityList()
	assert.NoError(t, err)
	assert.ElementsMatch(t, []string{names[1], names[4]}, schedNodeNames(list))

	// Adding another anti-affinity pod has to show up.
	assert.NoError(t, deltaStore.StorePodInfo(antiAffinityPod("aa-3", names[3]), names[3]))
	list, err = deltaStore.NodeInfos().HavePodsWithRequiredAntiAffinityList()
	assert.NoError(t, err)
	assert.ElementsMatch(t, []string{names[1], names[3], names[4]}, schedNodeNames(list))

	// Removing one has to drop it back out.
	assert.NoError(t, deltaStore.RemovePodInfo("default", "aa-3", names[3]))
	list, err = deltaStore.NodeInfos().HavePodsWithRequiredAntiAffinityList()
	assert.NoError(t, err)
	assert.ElementsMatch(t, []string{names[1], names[4]}, schedNodeNames(list))
}

// TestHavePodsWithAffinityListTracksPods is the same check for the affinity list, which
// shares the implementation shape.
func TestHavePodsWithAffinityListTracksPods(t *testing.T) {
	deltaStore, names := storeWithNodes(t, 6)

	list, err := deltaStore.NodeInfos().HavePodsWithAffinityList()
	assert.NoError(t, err)
	assert.Empty(t, list)

	// A hostname anti-affinity term also counts as "has affinity" for the scheduler.
	assert.NoError(t, deltaStore.StorePodInfo(antiAffinityPod("aa-2", names[2]), names[2]))
	list, err = deltaStore.NodeInfos().HavePodsWithAffinityList()
	assert.NoError(t, err)
	assert.ElementsMatch(t, []string{names[2]}, schedNodeNames(list))
}

// TestHavePodsWithRequiredAntiAffinityListAcrossFork checks that the lists are per-layer:
// a pod added inside a fork must not be visible in the list after reverting.
func TestHavePodsWithRequiredAntiAffinityListAcrossFork(t *testing.T) {
	deltaStore, names := storeWithNodes(t, 6)
	assert.NoError(t, deltaStore.StorePodInfo(antiAffinityPod("aa-1", names[1]), names[1]))

	before, err := deltaStore.NodeInfos().HavePodsWithRequiredAntiAffinityList()
	assert.NoError(t, err)
	assert.ElementsMatch(t, []string{names[1]}, schedNodeNames(before))

	deltaStore.Fork()
	assert.NoError(t, deltaStore.StorePodInfo(antiAffinityPod("aa-5", names[5]), names[5]))
	forked, err := deltaStore.NodeInfos().HavePodsWithRequiredAntiAffinityList()
	assert.NoError(t, err)
	assert.ElementsMatch(t, []string{names[1], names[5]}, schedNodeNames(forked))

	deltaStore.Revert()
	reverted, err := deltaStore.NodeInfos().HavePodsWithRequiredAntiAffinityList()
	assert.NoError(t, err)
	assert.ElementsMatch(t, []string{names[1]}, schedNodeNames(reverted),
		"pod added in the reverted fork leaked into the anti-affinity list")
}

// TestHavePodsWithRequiredAntiAffinityListAllNodesMatch covers the opposite extreme from
// the empty case: every node matches, so the build has to size itself to the full node
// count without losing or duplicating entries.
func TestHavePodsWithRequiredAntiAffinityListAllNodesMatch(t *testing.T) {
	const nodeCount = 200
	deltaStore, names := storeWithNodes(t, nodeCount)
	for i, name := range names {
		assert.NoError(t, deltaStore.StorePodInfo(antiAffinityPod(fmt.Sprintf("aa-%d", i), name), name))
	}

	list, err := deltaStore.NodeInfos().HavePodsWithRequiredAntiAffinityList()
	assert.NoError(t, err)
	assert.ElementsMatch(t, names, schedNodeNames(list))
}

// TestHavePodsWithRequiredAntiAffinityListAfterNodeRemoval checks that dropping a node
// drops it from the list too.
func TestHavePodsWithRequiredAntiAffinityListAfterNodeRemoval(t *testing.T) {
	deltaStore, names := storeWithNodes(t, 6)
	assert.NoError(t, deltaStore.StorePodInfo(antiAffinityPod("aa-1", names[1]), names[1]))
	assert.NoError(t, deltaStore.StorePodInfo(antiAffinityPod("aa-2", names[2]), names[2]))

	list, err := deltaStore.NodeInfos().HavePodsWithRequiredAntiAffinityList()
	assert.NoError(t, err)
	assert.Len(t, list, 2)

	assert.NoError(t, deltaStore.RemoveNodeInfo(context.Background(), names[1]))
	list, err = deltaStore.NodeInfos().HavePodsWithRequiredAntiAffinityList()
	assert.NoError(t, err)
	assert.ElementsMatch(t, []string{names[2]}, schedNodeNames(list))
}
