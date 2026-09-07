/*
Copyright 2024 The Kubernetes Authors.

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
	"errors"
	"fmt"
	"slices"

	schedulingv1alpha3 "k8s.io/api/scheduling/v1alpha3"
	schedulingv1beta1 "k8s.io/api/scheduling/v1beta1"
	"k8s.io/klog/v2"
	schedulerinterface "k8s.io/kube-scheduler/framework"
	"sigs.k8s.io/cluster-autoscaler/pkg/simulator/clustersnapshot"
	"sigs.k8s.io/cluster-autoscaler/pkg/simulator/framework"
)

var (
	errorGettingPodGroupState          = errors.New("PodGroupState is not integrated with CA simulator")
	errorGettingPodGroup               = errors.New("PodGroup is not integrated with CA simulator")
	errorGettingCompositePodGroupState = errors.New("CompositePodGroupState is not integrated with CA simulator")
	errorGettingCompositePodGroup      = errors.New("CompositePodGroup is not integrated with CA simulator")
)

// DeltaSnapshotStore is an implementation of ClusterSnapshotStore optimized for typical Cluster Autoscaler usage - (fork, add stuff, revert), repeated many times per loop.
//
// Complexity of some notable operations:
//
//	fork - O(1)
//	revert - O(1)
//	commit - O(n)
//	list all pods (no filtering) - O(n), cached
//	list all pods (with filtering) - O(n)
//	list node infos - O(n), cached
//
// Watch out for:
//
// * Node deletions, pod additions & deletions - invalidates cache of current snapshot
// (when forked affects delta, but not base.)
//
// * Pod affinity - causes scheduler framework to list pods with non-empty selector,
// so basic caching doesn't help.
//
// * DRA objects are tracked in the separate snapshot and while they don't exactly share
// memory and time complexities of DeltaSnapshotStore - they are optimized for
// cluster autoscaler operations
type DeltaSnapshotStore struct {
	data *internalDeltaSnapshotData
}

type deltaSnapshotStoreNodeLister DeltaSnapshotStore
type deltaSnapshotStoreStorageLister DeltaSnapshotStore
type deltaSnapshotPodGroupStateLister DeltaSnapshotStore
type deltaSnapshotPodGroupLister DeltaSnapshotStore
type deltaSnapshotCompositePodGroupStateLister DeltaSnapshotStore
type deltaSnapshotCompositePodGroupLister DeltaSnapshotStore

type internalDeltaSnapshotData struct {
	baseData *internalDeltaSnapshotData

	addedNodeInfoMap    map[string]*framework.NodeInfo
	modifiedNodeInfoMap map[string]*framework.NodeInfo
	deletedNodeInfos    map[string]bool

	nodeInfoList []*framework.NodeInfo
	// nodeInfoListBuilt tells nodeInfoList == nil (not built yet) apart from an empty
	// list that has been built.
	nodeInfoListBuilt bool
	// nodeInfoListAliased records that nodeInfoList shares its backing array with the
	// base layer's list. Its capacity is capped to its length, so appends reallocate;
	// overwriting an element needs an explicit ownNodeInfoList() first.
	nodeInfoListAliased bool
	// schedNodeInfoList is the scheduler-typed view of nodeInfoList. Converting between
	// the two slice types needs a fresh slice, so it's built only if a scheduler plugin
	// actually lists nodes, and invalidated whenever nodeInfoList changes.
	schedNodeInfoList []schedulerinterface.NodeInfo

	havePodsWithAffinity                          []schedulerinterface.NodeInfo
	havePodsWithRequiredAntiAffinity              []schedulerinterface.NodeInfo
	havePodsWithRequiredNonHostScopedAntiAffinity []schedulerinterface.NodeInfo
	pvcNamespaceMap                               map[string]int
}

func newInternalDeltaSnapshotData() *internalDeltaSnapshotData {
	return &internalDeltaSnapshotData{
		addedNodeInfoMap:    make(map[string]*framework.NodeInfo),
		modifiedNodeInfoMap: make(map[string]*framework.NodeInfo),
		deletedNodeInfos:    make(map[string]bool),
	}
}

func (data *internalDeltaSnapshotData) getNodeInfo(name string) (*framework.NodeInfo, bool) {
	if data == nil {
		return nil, false
	}
	if nodeInfo, found := data.getNodeInfoLocal(name); found {
		return nodeInfo, found
	}
	if data.deletedNodeInfos[name] {
		return nil, false
	}
	return data.baseData.getNodeInfo(name)
}

func (data *internalDeltaSnapshotData) getNodeInfoLocal(name string) (*framework.NodeInfo, bool) {
	if data == nil {
		return nil, false
	}
	if nodeInfo, found := data.addedNodeInfoMap[name]; found {
		return nodeInfo, true
	}
	if nodeInfo, found := data.modifiedNodeInfoMap[name]; found {
		return nodeInfo, true
	}
	return nil, false
}

// getNodeInfoList returns the effective node list for this layer. The slice is owned by
// the store and must be treated as read-only by callers.
func (data *internalDeltaSnapshotData) getNodeInfoList() []*framework.NodeInfo {
	if data == nil {
		return nil
	}
	if !data.nodeInfoListBuilt {
		data.nodeInfoList, data.nodeInfoListAliased = data.buildNodeInfoList()
		data.nodeInfoListBuilt = true
	}
	return data.nodeInfoList
}

// getSchedNodeInfoList returns the node list typed for the scheduler framework's
// NodeInfoLister.
func (data *internalDeltaSnapshotData) getSchedNodeInfoList() []schedulerinterface.NodeInfo {
	if data == nil {
		return nil
	}
	nodeInfos := data.getNodeInfoList()
	if data.schedNodeInfoList == nil {
		schedNodeInfos := make([]schedulerinterface.NodeInfo, len(nodeInfos))
		for i, nodeInfo := range nodeInfos {
			schedNodeInfos[i] = nodeInfo
		}
		data.schedNodeInfoList = schedNodeInfos
	}
	return data.schedNodeInfoList
}

// ownNodeInfoList gives this layer a backing array of its own, so that entries can be
// overwritten without corrupting the base layer's cached list.
func (data *internalDeltaSnapshotData) ownNodeInfoList() {
	if !data.nodeInfoListAliased {
		return
	}
	data.nodeInfoList = slices.Clone(data.nodeInfoList)
	data.nodeInfoListAliased = false
}

// replaceInNodeInfoList swaps the cached list entry for nodeName, leaving the rest of the
// list intact. No-op if the list hasn't been built yet.
func (data *internalDeltaSnapshotData) replaceInNodeInfoList(nodeName string, nodeInfo *framework.NodeInfo) {
	if !data.nodeInfoListBuilt {
		return
	}
	for i, ni := range data.nodeInfoList {
		if ni.Node().Name != nodeName {
			continue
		}
		data.ownNodeInfoList()
		data.nodeInfoList[i] = nodeInfo
		data.schedNodeInfoList = nil
		return
	}
}

// buildNodeInfoList computes the effective node list for this layer. The second return
// value reports that the result aliases the base layer's list rather than copying it,
// which is possible whenever this layer hasn't changed the set of nodes yet.
func (data *internalDeltaSnapshotData) buildNodeInfoList() ([]*framework.NodeInfo, bool) {
	baseList := data.baseData.getNodeInfoList()
	totalLen := len(baseList) + len(data.addedNodeInfoMap)
	var nodeInfoList []*framework.NodeInfo

	if len(data.deletedNodeInfos) > 0 || len(data.modifiedNodeInfoMap) > 0 {
		nodeInfoList = make([]*framework.NodeInfo, 0, totalLen)
		for _, bni := range baseList {
			if data.deletedNodeInfos[bni.Node().Name] {
				continue
			}
			if mni, found := data.modifiedNodeInfoMap[bni.Node().Name]; found {
				nodeInfoList = append(nodeInfoList, mni)
				continue
			}
			nodeInfoList = append(nodeInfoList, bni)
		}
	} else if len(data.addedNodeInfoMap) == 0 {
		// This layer contributes nothing, so the base list is already the answer. Hand it
		// out with its capacity capped to its length, so that a later append reallocates
		// instead of writing into the base layer's backing array.
		return baseList[:len(baseList):len(baseList)], true
	} else {
		nodeInfoList = make([]*framework.NodeInfo, len(baseList), totalLen)
		copy(nodeInfoList, baseList)
	}

	for _, ani := range data.addedNodeInfoMap {
		nodeInfoList = append(nodeInfoList, ani)
	}

	return nodeInfoList, false
}

func (data *internalDeltaSnapshotData) addNodeInfo(nodeInfo *framework.NodeInfo) error {
	if _, found := data.getNodeInfo(nodeInfo.Node().Name); found {
		return fmt.Errorf("node %s already in snapshot", nodeInfo.Node().Name)
	}

	if _, found := data.deletedNodeInfos[nodeInfo.Node().Name]; found {
		delete(data.deletedNodeInfos, nodeInfo.Node().Name)
		data.modifiedNodeInfoMap[nodeInfo.Node().Name] = nodeInfo
	} else {
		data.addedNodeInfoMap[nodeInfo.Node().Name] = nodeInfo
	}

	if data.nodeInfoListBuilt {
		// If the list is aliased its capacity equals its length, so this reallocates and
		// leaves the base layer's array untouched.
		data.nodeInfoList = append(data.nodeInfoList, nodeInfo)
		data.nodeInfoListAliased = false
		data.schedNodeInfoList = nil
	}

	if len(nodeInfo.GetPods()) > 0 {
		data.clearPodCaches()
	}

	return nil
}

func (data *internalDeltaSnapshotData) clearCaches() {
	data.nodeInfoList = nil
	data.nodeInfoListBuilt = false
	data.nodeInfoListAliased = false
	data.schedNodeInfoList = nil
	data.clearPodCaches()
}

func (data *internalDeltaSnapshotData) clearPodCaches() {
	data.havePodsWithAffinity = nil
	data.havePodsWithRequiredAntiAffinity = nil
	data.havePodsWithRequiredNonHostScopedAntiAffinity = nil
	// TODO: update the cache when adding/removing pods instead of invalidating the whole cache
	data.pvcNamespaceMap = nil
}

func (data *internalDeltaSnapshotData) removeNodeInfo(nodeName string) error {
	_, foundInDelta := data.addedNodeInfoMap[nodeName]
	if foundInDelta {
		// If node was added within this delta, delete this change.
		delete(data.addedNodeInfoMap, nodeName)
	}

	if _, modified := data.modifiedNodeInfoMap[nodeName]; modified {
		// If node was modified within this delta, delete this change.
		delete(data.modifiedNodeInfoMap, nodeName)
	}

	if _, deleted := data.deletedNodeInfos[nodeName]; deleted {
		// If node was deleted within this delta, fail with error.
		return clustersnapshot.ErrNodeNotFound
	}

	_, foundInBase := data.baseData.getNodeInfo(nodeName)
	if foundInBase {
		// If node was found in the underlying data, mark it as deleted in delta.
		data.deletedNodeInfos[nodeName] = true
	}

	if !foundInBase && !foundInDelta {
		// Node not found in the chain.
		return clustersnapshot.ErrNodeNotFound
	}

	// Maybe consider deleting from the lists instead. Maybe not.
	data.clearCaches()
	return nil
}

func (data *internalDeltaSnapshotData) nodeInfoToModify(nodeName string) (*framework.NodeInfo, bool) {
	dni, found := data.getNodeInfoLocal(nodeName)
	if !found {
		if _, found := data.deletedNodeInfos[nodeName]; found {
			return nil, false
		}
		bni, found := data.baseData.getNodeInfo(nodeName)
		if !found {
			return nil, false
		}
		dni = bni.SnapshotTyped()
		data.modifiedNodeInfoMap[nodeName] = dni
		data.clearCaches()
	}
	return dni, true
}

func (data *internalDeltaSnapshotData) addPodInfo(podInfo schedulerinterface.PodInfo, nodeName string) error {
	ni, found := data.nodeInfoToModify(nodeName)
	if !found {
		return clustersnapshot.ErrNodeNotFound
	}

	ni.AddPodInfo(podInfo)

	data.clearCaches()
	return nil
}

func (data *internalDeltaSnapshotData) removePod(namespace, name, nodeName string) error {
	// This always clones node info, even if the pod is actually missing.
	// Not sure if we mind, since removing non-existent pod
	// probably means things are very bad anyway.
	ni, found := data.nodeInfoToModify(nodeName)
	if !found {
		return clustersnapshot.ErrNodeNotFound
	}

	podFound := false
	logger := klog.Background()
	for _, podInfo := range ni.GetPods() {
		if podInfo.GetPod().Namespace == namespace && podInfo.GetPod().Name == name {
			if err := ni.RemovePod(logger, podInfo.GetPod()); err != nil {
				return fmt.Errorf("cannot remove pod; %v", err)
			}
			podFound = true
			break
		}
	}
	if !podFound {
		return fmt.Errorf("pod %s/%s not in snapshot", namespace, name)
	}

	data.clearCaches()
	return nil
}

func (data *internalDeltaSnapshotData) isPVCUsedByPods(key string) bool {
	if data.pvcNamespaceMap != nil {
		return data.pvcNamespaceMap[key] > 0
	}
	nodeInfos := data.getNodeInfoList()
	pvcNamespaceMap := make(map[string]int)
	for _, v := range nodeInfos {
		for k, i := range v.GetPVCRefCounts() {
			pvcNamespaceMap[k] += i
		}
	}
	data.pvcNamespaceMap = pvcNamespaceMap
	return data.pvcNamespaceMap[key] > 0
}

func (data *internalDeltaSnapshotData) fork() *internalDeltaSnapshotData {
	forkedData := newInternalDeltaSnapshotData()
	forkedData.baseData = data
	return forkedData
}

func (data *internalDeltaSnapshotData) commit() (*internalDeltaSnapshotData, error) {
	if data.baseData == nil {
		// do nothing... as in basic snapshot.
		return data, nil
	}
	for node := range data.deletedNodeInfos {
		if err := data.baseData.removeNodeInfo(node); err != nil {
			return nil, err
		}
	}
	for _, node := range data.modifiedNodeInfoMap {
		if err := data.baseData.removeNodeInfo(node.Node().Name); err != nil {
			return nil, err
		}
		if err := data.baseData.addNodeInfo(node); err != nil {
			return nil, err
		}
	}
	for _, node := range data.addedNodeInfoMap {
		if err := data.baseData.addNodeInfo(node); err != nil {
			return nil, err
		}
	}

	return data.baseData, nil
}

// List returns list of all node infos.
func (snapshot *deltaSnapshotStoreNodeLister) List() ([]schedulerinterface.NodeInfo, error) {
	return snapshot.data.getSchedNodeInfoList(), nil
}

// HavePodsWithAffinityList returns list of all node infos with pods that have affinity constrints.
func (snapshot *deltaSnapshotStoreNodeLister) HavePodsWithAffinityList() ([]schedulerinterface.NodeInfo, error) {
	data := snapshot.data
	if data.havePodsWithAffinity != nil {
		return data.havePodsWithAffinity, nil
	}

	nodeInfoList := snapshot.data.getNodeInfoList()
	havePodsWithAffinityList := make([]schedulerinterface.NodeInfo, 0, len(nodeInfoList))
	for _, node := range nodeInfoList {
		if len(node.GetPodsWithAffinity()) > 0 {
			havePodsWithAffinityList = append(havePodsWithAffinityList, node)
		}
	}
	data.havePodsWithAffinity = havePodsWithAffinityList
	return data.havePodsWithAffinity, nil
}

// HavePodsWithRequiredAntiAffinityList returns the list of NodeInfos of nodes with pods with required anti-affinity terms.
func (snapshot *deltaSnapshotStoreNodeLister) HavePodsWithRequiredAntiAffinityList() ([]schedulerinterface.NodeInfo, error) {
	data := snapshot.data
	if data.havePodsWithRequiredAntiAffinity != nil {
		return data.havePodsWithRequiredAntiAffinity, nil
	}

	nodeInfoList := snapshot.data.getNodeInfoList()
	havePodsWithRequiredAntiAffinityList := make([]schedulerinterface.NodeInfo, 0, len(nodeInfoList))
	for _, node := range nodeInfoList {
		if len(node.GetPodsWithRequiredAntiAffinity()) > 0 {
			havePodsWithRequiredAntiAffinityList = append(havePodsWithRequiredAntiAffinityList, node)
		}
	}
	data.havePodsWithRequiredAntiAffinity = havePodsWithRequiredAntiAffinityList
	return data.havePodsWithRequiredAntiAffinity, nil
}

// HavePodsWithRequiredNonHostScopedAntiAffinityList returns nodes containing pods that require a wider topology scan (topologyKey other than hostname).
func (snapshot *deltaSnapshotStoreNodeLister) HavePodsWithRequiredNonHostScopedAntiAffinityList() ([]schedulerinterface.NodeInfo, error) {
	data := snapshot.data
	if data.havePodsWithRequiredNonHostScopedAntiAffinity != nil {
		return data.havePodsWithRequiredNonHostScopedAntiAffinity, nil
	}

	nodeInfoList := snapshot.data.getNodeInfoList()
	havePodsWithRequiredNonHostScopedAntiAffinityList := make([]schedulerinterface.NodeInfo, 0, len(nodeInfoList))
	for _, node := range nodeInfoList {
		if len(node.GetPodsWithRequiredNonHostScopedAntiAffinity()) > 0 {
			havePodsWithRequiredNonHostScopedAntiAffinityList = append(havePodsWithRequiredNonHostScopedAntiAffinityList, node)
		}
	}
	data.havePodsWithRequiredNonHostScopedAntiAffinity = havePodsWithRequiredNonHostScopedAntiAffinityList
	return data.havePodsWithRequiredNonHostScopedAntiAffinity, nil
}

// Get returns node info by node name.
func (snapshot *deltaSnapshotStoreNodeLister) Get(nodeName string) (schedulerinterface.NodeInfo, error) {
	return (*DeltaSnapshotStore)(snapshot).getNodeInfo(nodeName)
}

// IsPVCUsedByPods returns if PVC is used by pods
func (snapshot *deltaSnapshotStoreStorageLister) IsPVCUsedByPods(key string) bool {
	return (*DeltaSnapshotStore)(snapshot).IsPVCUsedByPods(key)
}

// Get returns pod group state by namespace and pod group name.
//
// This method is never supposed to be called in the cluster autoscaler simulations
// until PodGroups are integrated with cluster autoscaler.
func (snapshot *deltaSnapshotPodGroupStateLister) Get(namespace string, podGroupName string) (schedulerinterface.PodGroupState, error) {
	return nil, errorGettingPodGroupState
}

// This method is never supposed to be called in the cluster autoscaler simulations
// until PodGroups are integrated with cluster autoscaler.
func (snapshot *deltaSnapshotPodGroupLister) Get(namespace string, podGroupName string) (*schedulingv1beta1.PodGroup, error) {
	return nil, errorGettingPodGroup
}

// This method is never supposed to be called in the cluster autoscaler simulations
// until CompositePodGroups are integrated with cluster autoscaler.
func (snapshot *deltaSnapshotCompositePodGroupStateLister) Get(namespace string, name string) (schedulerinterface.CompositePodGroupState, error) {
	return nil, errorGettingCompositePodGroupState
}

// This method is never supposed to be called in the cluster autoscaler simulations
// until CompositePodGroups are integrated with cluster autoscaler.
func (snapshot *deltaSnapshotCompositePodGroupLister) Get(namespace string, name string) (*schedulingv1alpha3.CompositePodGroup, error) {
	return nil, errorGettingCompositePodGroup
}

func (snapshot *DeltaSnapshotStore) getNodeInfo(nodeName string) (schedulerinterface.NodeInfo, error) {
	data := snapshot.data
	node, found := data.getNodeInfo(nodeName)
	if !found {
		return nil, clustersnapshot.ErrNodeNotFound
	}
	return node, nil
}

// ListNodeInfos returns the internal NodeInfos for all Nodes tracked in the snapshot.
// The returned slice is owned by the store and must not be modified by the caller.
func (snapshot *DeltaSnapshotStore) ListNodeInfos() ([]*framework.NodeInfo, error) {
	return snapshot.data.getNodeInfoList(), nil
}

// NodeInfos returns node lister.
func (snapshot *DeltaSnapshotStore) NodeInfos() schedulerinterface.NodeInfoLister {
	return (*deltaSnapshotStoreNodeLister)(snapshot)
}

// StorageInfos returns storage lister
func (snapshot *DeltaSnapshotStore) StorageInfos() schedulerinterface.StorageInfoLister {
	return (*deltaSnapshotStoreStorageLister)(snapshot)
}

// PodGroupStates returns pod group state lister.
func (snapshot *DeltaSnapshotStore) PodGroupStates() schedulerinterface.PodGroupStateLister {
	return (*deltaSnapshotPodGroupStateLister)(snapshot)
}

// PodGroups returns pod group lister.
func (snapshot *DeltaSnapshotStore) PodGroups() schedulerinterface.PodGroupLister {
	return (*deltaSnapshotPodGroupLister)(snapshot)
}

// CompositePodGroupStates returns composite pod group state lister.
func (snapshot *DeltaSnapshotStore) CompositePodGroupStates() schedulerinterface.CompositePodGroupStateLister {
	return (*deltaSnapshotCompositePodGroupStateLister)(snapshot)
}

// CompositePodGroups returns composite pod group lister.
func (snapshot *DeltaSnapshotStore) CompositePodGroups() schedulerinterface.CompositePodGroupLister {
	return (*deltaSnapshotCompositePodGroupLister)(snapshot)
}

// NewDeltaSnapshotStore creates instances of DeltaSnapshotStore.
func NewDeltaSnapshotStore() *DeltaSnapshotStore {
	snapshot := &DeltaSnapshotStore{}
	snapshot.Clear()
	return snapshot
}

// RemoveNodeInfo removes nodes (and pods scheduled to it) from the snapshot.
func (snapshot *DeltaSnapshotStore) RemoveNodeInfo(ctx context.Context, nodeName string) error {
	return snapshot.data.removeNodeInfo(nodeName)
}

// StoreNodeInfo adds the given *framework.NodeInfo to the snapshot without checking scheduler predicates.
func (snapshot *DeltaSnapshotStore) StoreNodeInfo(nodeInfo *framework.NodeInfo) error {
	return snapshot.data.addNodeInfo(nodeInfo)
}

// StorePodInfo adds pod to the snapshot and schedules it to given node.
func (snapshot *DeltaSnapshotStore) StorePodInfo(podInfo *framework.PodInfo, nodeName string) error {
	return snapshot.data.addPodInfo(podInfo, nodeName)
}

// RemovePodInfo removes pod from the snapshot.
func (snapshot *DeltaSnapshotStore) RemovePodInfo(namespace, podName, nodeName string) error {
	return snapshot.data.removePod(namespace, podName, nodeName)
}

// IsPVCUsedByPods returns if the pvc is used by any pod
func (snapshot *DeltaSnapshotStore) IsPVCUsedByPods(key string) bool {
	return snapshot.data.isPVCUsedByPods(key)
}

// Fork creates a fork of snapshot state. All modifications can later be reverted to moment of forking via Revert()
// Time: O(1)
func (snapshot *DeltaSnapshotStore) Fork() {
	snapshot.data = snapshot.data.fork()
}

// Revert reverts snapshot state to moment of forking.
// Time: O(1)
func (snapshot *DeltaSnapshotStore) Revert() {
	if snapshot.data.baseData != nil {
		snapshot.data = snapshot.data.baseData
	}
}

// Commit commits changes done after forking.
// Time: O(n), where n = size of delta (number of nodes added, modified or deleted since forking)
func (snapshot *DeltaSnapshotStore) Commit() error {
	newData, err := snapshot.data.commit()
	if err != nil {
		return err
	}
	snapshot.data = newData
	return nil
}

// Clear reset cluster snapshot to empty, unforked state
// Time: O(1)
func (snapshot *DeltaSnapshotStore) Clear() {
	snapshot.data = newInternalDeltaSnapshotData()
}
