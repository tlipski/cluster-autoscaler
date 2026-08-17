/*
Copyright 2023 The Kubernetes Authors.

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

package kwok

import (
	"context"
	"fmt"
	resourceapi "k8s.io/api/resource/v1"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	apiv1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
	core "k8s.io/client-go/testing"
	"sigs.k8s.io/cluster-autoscaler/pkg/cloudprovider"
	"sigs.k8s.io/cluster-autoscaler/pkg/config"
	kube_util "sigs.k8s.io/cluster-autoscaler/pkg/utils/kubernetes"
)

func TestIncreaseSize(t *testing.T) {
	fakeClient := &fake.Clientset{}

	nodes := []*apiv1.Node{}

	fakeClient.Fake.AddReactor("create", "nodes",
		func(action core.Action) (bool, runtime.Object, error) {
			createAction := action.(core.CreateAction)
			if createAction == nil {
				return false, nil, nil
			}

			nodes = append(nodes, createAction.GetObject().(*apiv1.Node))

			return true, nil, nil
		})

	ng := NodeGroup{
		name:       "ng",
		kubeClient: fakeClient,
		lister:     kube_util.NewTestNodeLister(nil),
		nodeTemplate: &apiv1.Node{
			ObjectMeta: metav1.ObjectMeta{
				Name: "template-node-ng",
			},
		},
		minSize:    0,
		targetSize: 2,
		maxSize:    3,
	}

	// usual case
	err := ng.IncreaseSize(context.Background(), 1)
	assert.Nil(t, err)
	assert.Len(t, nodes, 1)
	assert.Equal(t, 3, ng.targetSize)
	for _, n := range nodes {
		assert.Contains(t, n.Spec.ProviderID, "kwok")
		assert.Contains(t, n.GetName(), ng.name)
		assert.Contains(t, n.Annotations["metrics.k8s.io/resource-metrics-path"], fmt.Sprintf("/metrics/nodes/%s/metrics/resource", n.GetName()))
	}

	// delta is negative
	nodes = []*apiv1.Node{}
	err = ng.IncreaseSize(context.Background(), -1)
	assert.NotNil(t, err)
	assert.Contains(t, err.Error(), sizeIncreaseMustBePositiveErr)
	assert.Len(t, nodes, 0)

	// delta is greater than max size
	nodes = []*apiv1.Node{}
	err = ng.IncreaseSize(context.Background(), ng.maxSize+1)
	assert.NotNil(t, err)
	assert.Contains(t, err.Error(), maxSizeReachedErr)
	assert.Len(t, nodes, 0)

}

func TestDeleteNodes(t *testing.T) {
	fakeClient := &fake.Clientset{}

	deletedNodes := make(map[string]bool)
	fakeClient.Fake.AddReactor("delete", "nodes", func(action core.Action) (bool, runtime.Object, error) {
		deleteAction := action.(core.DeleteAction)

		if deleteAction == nil {
			return false, nil, nil
		}

		deletedNodes[deleteAction.GetName()] = true

		return true, nil, nil

	})

	ng := NodeGroup{
		name:       "ng",
		kubeClient: fakeClient,
		lister:     kube_util.NewTestNodeLister(nil),
		nodeTemplate: &apiv1.Node{
			ObjectMeta: metav1.ObjectMeta{
				Name: "template-node-ng",
			},
		},
		minSize:    0,
		targetSize: 1,
		maxSize:    3,
	}

	nodeToDelete1 := &apiv1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: "node-to-delete-1",
			Annotations: map[string]string{
				KwokManagedAnnotation: "fake",
			},
		},
	}

	nodeToDelete2 := &apiv1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: "node-to-delete-2",
			Annotations: map[string]string{
				KwokManagedAnnotation: "fake",
			},
		},
	}

	nodeWithoutKwokAnnotation := &apiv1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name:        "node-to-delete-3",
			Annotations: map[string]string{},
		},
	}

	// usual case
	err := ng.DeleteNodes(context.Background(), []*apiv1.Node{nodeToDelete1})
	assert.Nil(t, err)
	assert.True(t, deletedNodes[nodeToDelete1.GetName()])

	// min size reached
	deletedNodes = make(map[string]bool)
	ng.targetSize = 0
	err = ng.DeleteNodes(context.Background(), []*apiv1.Node{nodeToDelete1})
	assert.NotNil(t, err)
	assert.Contains(t, err.Error(), minSizeReachedErr)
	assert.False(t, deletedNodes[nodeToDelete1.GetName()])
	ng.targetSize = 1

	// too many nodes to delete - goes below ng's minSize
	deletedNodes = make(map[string]bool)
	err = ng.DeleteNodes(context.Background(), []*apiv1.Node{nodeToDelete1, nodeToDelete2})
	assert.NotNil(t, err)
	assert.Contains(t, err.Error(), belowMinSizeErr)
	assert.False(t, deletedNodes[nodeToDelete1.GetName()])
	assert.False(t, deletedNodes[nodeToDelete2.GetName()])

	// kwok annotation is not present on the node to delete
	deletedNodes = make(map[string]bool)
	err = ng.DeleteNodes(context.Background(), []*apiv1.Node{nodeWithoutKwokAnnotation})
	assert.NotNil(t, err)
	assert.Contains(t, err.Error(), "not managed by kwok")
	assert.False(t, deletedNodes[nodeWithoutKwokAnnotation.GetName()])

}

func TestDecreaseTargetSize(t *testing.T) {
	fakeClient := &fake.Clientset{}

	fakeNodes := []*apiv1.Node{
		{
			ObjectMeta: metav1.ObjectMeta{
				Name: "node-1",
			},
		},
		{
			ObjectMeta: metav1.ObjectMeta{
				Name: "node-2",
			},
		},
	}

	ng := NodeGroup{
		name:       "ng",
		kubeClient: fakeClient,
		lister:     kube_util.NewTestNodeLister(fakeNodes),
		nodeTemplate: &apiv1.Node{
			ObjectMeta: metav1.ObjectMeta{
				Name: "template-node-ng",
			},
		},
		minSize:    0,
		targetSize: 3,
		maxSize:    4,
	}

	// usual case
	err := ng.DecreaseTargetSize(context.Background(), -1)
	assert.Nil(t, err)
	assert.Equal(t, 2, ng.targetSize)

	// delta is positive
	ng.targetSize = 3
	err = ng.DecreaseTargetSize(context.Background(), 1)
	assert.NotNil(t, err)
	assert.Contains(t, err.Error(), sizeDecreaseMustBeNegativeErr)
	assert.Equal(t, 3, ng.targetSize)

	// attempt to delete existing nodes
	err = ng.DecreaseTargetSize(context.Background(), -2)
	assert.NotNil(t, err)
	assert.Contains(t, err.Error(), attemptToDeleteExistingNodesErr)
	assert.Equal(t, 3, ng.targetSize)

	// error from lister
	ng.lister = &ErroneousNodeLister{}
	err = ng.DecreaseTargetSize(context.Background(), -1)
	assert.NotNil(t, err)
	assert.Equal(t, cloudprovider.ErrNotImplemented.Error(), err.Error())
	assert.Equal(t, 3, ng.targetSize)
	ng.lister = kube_util.NewTestNodeLister(fakeNodes)
}

func TestNodes(t *testing.T) {
	fakeClient := &fake.Clientset{}

	fakeNodes := []*apiv1.Node{
		{
			ObjectMeta: metav1.ObjectMeta{
				Name: "node-1",
			},
		},
		{
			ObjectMeta: metav1.ObjectMeta{
				Name: "node-2",
			},
		},
	}

	ng := NodeGroup{
		name:       "ng",
		kubeClient: fakeClient,
		lister:     kube_util.NewTestNodeLister(fakeNodes),
		nodeTemplate: &apiv1.Node{
			ObjectMeta: metav1.ObjectMeta{
				Name: "template-node-ng",
			},
		},
		minSize:    0,
		targetSize: 2,
		maxSize:    3,
	}

	// usual case
	cpInstances, err := ng.Nodes(context.Background())
	assert.Nil(t, err)
	assert.Len(t, cpInstances, 2)
	for i := range cpInstances {
		assert.Contains(t, cpInstances[i].Id, fakeNodes[i].GetName())
		assert.Equal(t, &cloudprovider.InstanceStatus{
			State:     cloudprovider.InstanceRunning,
			ErrorInfo: nil,
		}, cpInstances[i].Status)
	}

	// error from lister
	ng.lister = &ErroneousNodeLister{}
	cpInstances, err = ng.Nodes(context.Background())
	assert.NotNil(t, err)
	assert.Len(t, cpInstances, 0)
	assert.Equal(t, cloudprovider.ErrNotImplemented.Error(), err.Error())

}

func TestTemplateNodeInfo(t *testing.T) {
	fakeClient := &fake.Clientset{}

	ng := NodeGroup{
		name:       "ng",
		kubeClient: fakeClient,
		lister:     kube_util.NewTestNodeLister(nil),
		nodeTemplate: &apiv1.Node{
			ObjectMeta: metav1.ObjectMeta{
				Name: "template-node-ng",
			},
		},
		minSize:    0,
		targetSize: 2,
		maxSize:    3,
	}

	// usual case
	ti, err := ng.TemplateNodeInfo(context.Background())
	assert.Nil(t, err)
	assert.NotNil(t, ti)
	assert.Len(t, ti.Pods(), 1)
	assert.Contains(t, ti.Pods()[0].Pod.Name, fmt.Sprintf("kube-proxy-%s", ng.name))
	assert.Equal(t, ng.nodeTemplate, ti.Node())

}

func TestTemplateNodeInfoResourceSlices(t *testing.T) {
	nodeName := "template-node-ng"
	slice := &resourceapi.ResourceSlice{
		ObjectMeta: metav1.ObjectMeta{Name: "slice-ng"},
		Spec: resourceapi.ResourceSliceSpec{
			NodeName: &nodeName,
			Driver:   "gpu.example.com",
			Pool:     resourceapi.ResourcePool{Name: nodeName, ResourceSliceCount: 1},
			Devices:  []resourceapi.Device{{Name: "gpu-0"}},
		},
	}

	ng := NodeGroup{
		name:           "ng",
		kubeClient:     &fake.Clientset{},
		lister:         kube_util.NewTestNodeLister(nil),
		nodeTemplate:   &apiv1.Node{ObjectMeta: metav1.ObjectMeta{Name: nodeName}},
		resourceSlices: []*resourceapi.ResourceSlice{slice},
		minSize:        0,
		targetSize:     2,
		maxSize:        3,
	}

	ti, err := ng.TemplateNodeInfo(context.Background())
	assert.Nil(t, err)
	assert.Len(t, ti.LocalResourceSlices, 1)
	assert.Equal(t, "gpu-0", ti.LocalResourceSlices[0].Spec.Devices[0].Name)

	// The returned slices must be copies: the estimator sanitizes them per
	// simulated node, and mutating the group's template would corrupt every
	// later simulation.
	ti.LocalResourceSlices[0].Spec.Pool.Name = "mutated"
	assert.Equal(t, nodeName, ng.resourceSlices[0].Spec.Pool.Name)

	// A group with no slices must behave exactly as before.
	ng.resourceSlices = nil
	ti, err = ng.TemplateNodeInfo(context.Background())
	assert.Nil(t, err)
	assert.Empty(t, ti.LocalResourceSlices)
}

func TestGetOptions(t *testing.T) {
	fakeClient := &fake.Clientset{}

	ng := NodeGroup{
		name:       "ng",
		kubeClient: fakeClient,
		lister:     kube_util.NewTestNodeLister(nil),
		nodeTemplate: &apiv1.Node{
			ObjectMeta: metav1.ObjectMeta{
				Name: "template-node-ng",
			},
		},
		minSize:    0,
		targetSize: 2,
		maxSize:    3,
	}

	// dummy values
	autoscalingOptions := config.NodeGroupAutoscalingOptions{
		ScaleDownUtilizationThreshold:    50.0,
		ScaleDownGpuUtilizationThreshold: 50.0,
		ScaleDownUnneededTime:            time.Minute * 5,
		ScaleDownUnreadyTime:             time.Minute * 5,
		MaxNodeProvisionTime:             time.Minute * 5,
		ZeroOrMaxNodeScaling:             true,
		IgnoreDaemonSetsUtilization:      true,
	}

	// usual case
	opts, err := ng.GetOptions(context.Background(), autoscalingOptions)
	assert.Nil(t, err)
	assert.Equal(t, autoscalingOptions, *opts)

}

// ErroneousNodeLister is used to check if the caller function throws an error
// if lister throws an error
type ErroneousNodeLister struct {
}

func (e *ErroneousNodeLister) List() ([]*apiv1.Node, error) {
	return nil, cloudprovider.ErrNotImplemented
}

func (e *ErroneousNodeLister) Get(name string) (*apiv1.Node, error) {
	return nil, cloudprovider.ErrNotImplemented
}
