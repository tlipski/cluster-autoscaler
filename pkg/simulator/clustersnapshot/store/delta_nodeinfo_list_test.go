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
	"testing"

	"github.com/stretchr/testify/assert"
	"sigs.k8s.io/cluster-autoscaler/pkg/simulator/clustersnapshot"
	"sigs.k8s.io/cluster-autoscaler/pkg/simulator/framework"
	. "sigs.k8s.io/cluster-autoscaler/pkg/utils/test"
)

// storeWithNodes returns a DeltaSnapshotStore holding nodeCount nodes, with the node list
// cache already built.
func storeWithNodes(t *testing.T, nodeCount int) (*DeltaSnapshotStore, []string) {
	t.Helper()
	deltaStore := NewDeltaSnapshotStore()
	var names []string
	for _, node := range clustersnapshot.CreateTestNodes(nodeCount) {
		if err := deltaStore.StoreNodeInfo(framework.NewNodeInfo(node, nil)); err != nil {
			t.Fatalf("StoreNodeInfo(%s): %v", node.Name, err)
		}
		names = append(names, node.Name)
	}
	// Force the base layer to build and cache its list, so that a fork can alias it.
	if _, err := deltaStore.ListNodeInfos(); err != nil {
		t.Fatalf("ListNodeInfos: %v", err)
	}
	return deltaStore, names
}

func nodeNames(nodeInfos []*framework.NodeInfo) []string {
	names := make([]string, 0, len(nodeInfos))
	for _, nodeInfo := range nodeInfos {
		names = append(names, nodeInfo.Node().Name)
	}
	return names
}

// TestDeltaAddNodeInBranchDoesNotCorruptBaseList checks that adding a node inside a fork
// doesn't write into the base layer's cached list, which the fork's list may alias.
func TestDeltaAddNodeInBranchDoesNotCorruptBaseList(t *testing.T) {
	deltaStore, baseNames := storeWithNodes(t, 8)

	deltaStore.Fork()
	// Build the fork's list before mutating it - this is what aliases the base list.
	forkedBefore, err := deltaStore.ListNodeInfos()
	assert.NoError(t, err)
	assert.ElementsMatch(t, baseNames, nodeNames(forkedBefore))

	extraNode := BuildTestNode("extra-node", 1000, 1000)
	assert.NoError(t, deltaStore.StoreNodeInfo(framework.NewNodeInfo(extraNode, nil)))

	forkedAfter, err := deltaStore.ListNodeInfos()
	assert.NoError(t, err)
	assert.ElementsMatch(t, append(append([]string{}, baseNames...), "extra-node"), nodeNames(forkedAfter))

	deltaStore.Revert()
	reverted, err := deltaStore.ListNodeInfos()
	assert.NoError(t, err)
	assert.ElementsMatch(t, baseNames, nodeNames(reverted), "base list was modified by the reverted fork")
}

// TestDeltaAddPodInBranchDoesNotCorruptBaseList checks that scheduling a pod onto a base
// node inside a fork replaces the entry only in the fork's list, not the base's.
func TestDeltaAddPodInBranchDoesNotCorruptBaseList(t *testing.T) {
	deltaStore, baseNames := storeWithNodes(t, 8)
	targetNode := baseNames[3]

	baseNodeInfo, err := deltaStore.NodeInfos().Get(targetNode)
	assert.NoError(t, err)
	assert.Empty(t, baseNodeInfo.GetPods())

	deltaStore.Fork()
	// Build the fork's list before mutating it, so that it aliases the base list.
	_, err = deltaStore.ListNodeInfos()
	assert.NoError(t, err)

	pod := BuildTestPod("p-1", 100, 100)
	pod.Spec.NodeName = targetNode
	assert.NoError(t, deltaStore.StorePodInfo(framework.NewPodInfo(pod, nil), targetNode))

	// Inside the fork, the list must show the node carrying the pod.
	forked, err := deltaStore.ListNodeInfos()
	assert.NoError(t, err)
	assert.ElementsMatch(t, baseNames, nodeNames(forked))
	assert.Equal(t, 1, len(nodeInfoByName(t, forked, targetNode).GetPods()))

	deltaStore.Revert()

	reverted, err := deltaStore.ListNodeInfos()
	assert.NoError(t, err)
	assert.ElementsMatch(t, baseNames, nodeNames(reverted))
	assert.Empty(t, nodeInfoByName(t, reverted, targetNode).GetPods(),
		"pod added in the reverted fork leaked into the base list")
}

// TestDeltaRemoveNodeInBranchDoesNotCorruptBaseList is the same check for node removal,
// which drops the cached list rather than patching it.
func TestDeltaRemoveNodeInBranchDoesNotCorruptBaseList(t *testing.T) {
	deltaStore, baseNames := storeWithNodes(t, 8)

	deltaStore.Fork()
	_, err := deltaStore.ListNodeInfos()
	assert.NoError(t, err)

	assert.NoError(t, deltaStore.RemoveNodeInfo(context.Background(), baseNames[2]))

	forked, err := deltaStore.ListNodeInfos()
	assert.NoError(t, err)
	assert.NotContains(t, nodeNames(forked), baseNames[2])

	deltaStore.Revert()
	reverted, err := deltaStore.ListNodeInfos()
	assert.NoError(t, err)
	assert.ElementsMatch(t, baseNames, nodeNames(reverted))
}

// TestDeltaListNodeInfosMatchesSchedulerList checks that the CA-typed list and the
// scheduler-typed view stay in agreement as the snapshot is mutated.
func TestDeltaListNodeInfosMatchesSchedulerList(t *testing.T) {
	deltaStore, baseNames := storeWithNodes(t, 8)

	assertListsAgree := func(step string) {
		t.Helper()
		typed, err := deltaStore.ListNodeInfos()
		assert.NoError(t, err, step)
		sched, err := deltaStore.NodeInfos().List()
		assert.NoError(t, err, step)
		assert.Equal(t, len(typed), len(sched), step)
		for i := range typed {
			assert.Same(t, typed[i], sched[i], step)
		}
	}

	assertListsAgree("base")

	deltaStore.Fork()
	assertListsAgree("after fork")

	extraNode := BuildTestNode("extra-node", 1000, 1000)
	assert.NoError(t, deltaStore.StoreNodeInfo(framework.NewNodeInfo(extraNode, nil)))
	assertListsAgree("after add")

	pod := BuildTestPod("p-1", 100, 100)
	pod.Spec.NodeName = baseNames[1]
	assert.NoError(t, deltaStore.StorePodInfo(framework.NewPodInfo(pod, nil), baseNames[1]))
	assertListsAgree("after pod add")

	assert.NoError(t, deltaStore.RemoveNodeInfo(context.Background(), baseNames[0]))
	assertListsAgree("after remove")

	deltaStore.Revert()
	assertListsAgree("after revert")
}

func nodeInfoByName(t *testing.T, nodeInfos []*framework.NodeInfo, name string) *framework.NodeInfo {
	t.Helper()
	for _, nodeInfo := range nodeInfos {
		if nodeInfo.Node().Name == name {
			return nodeInfo
		}
	}
	t.Fatalf("node %s not found in list", name)
	return nil
}
