/*
Copyright 2025 The Kubernetes Authors.

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

package snapshot

import (
	"testing"

	"github.com/google/go-cmp/cmp"

	v1 "k8s.io/api/core/v1"
	resourceapi "k8s.io/api/resource/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
)

var (
	extendedClass = &resourceapi.DeviceClass{
		ObjectMeta: metav1.ObjectMeta{Name: "class-extended", UID: "class-extended"},
		Spec:       resourceapi.DeviceClassSpec{ExtendedResourceName: ptr.To("example.com/gpu")},
	}
)

func TestSnapshotDeviceClassResolverGetDeviceClass(t *testing.T) {
	classes := map[string]*resourceapi.DeviceClass{
		"class-1":        class1,
		"class-extended": extendedClass,
	}

	for _, tc := range []struct {
		testName     string
		resourceName v1.ResourceName
		wantClass    *resourceapi.DeviceClass
	}{
		{
			testName:     "class resolved by its prefixed name",
			resourceName: v1.ResourceName(resourceapi.ResourceDeviceClassPrefix + "class-1"),
			wantClass:    class1,
		},
		{
			testName:     "class resolved by its extended resource name",
			resourceName: "example.com/gpu",
			wantClass:    extendedClass,
		},
		{
			testName:     "class without an extended resource name is not resolved by a bare name",
			resourceName: "class-1",
			wantClass:    nil,
		},
		{
			testName:     "unknown resource name resolves to nil",
			resourceName: "example.com/nonexistent",
			wantClass:    nil,
		},
	} {
		t.Run(tc.testName, func(t *testing.T) {
			snapshot := NewSnapshot(nil, nil, nil, classes)
			got := snapshot.DeviceClassResolver().GetDeviceClass(tc.resourceName)
			if diff := cmp.Diff(tc.wantClass, got); diff != "" {
				t.Errorf("GetDeviceClass(%q): unexpected output (-want +got): %s", tc.resourceName, diff)
			}
		})
	}
}

// TestSnapshotDeviceClassResolverReused guards the caching in Snapshot.DeviceClassResolver: the
// scheduler asks for a resolver on every PreFilter, and rebuilding it there walks every DeviceClass.
func TestSnapshotDeviceClassResolverReused(t *testing.T) {
	classes := map[string]*resourceapi.DeviceClass{"class-1": class1, "class-2": class2}
	snapshot := NewSnapshot(nil, nil, nil, classes)

	first := snapshot.DeviceClassResolver()
	if second := snapshot.DeviceClassResolver(); first != second {
		t.Errorf("DeviceClassResolver(): got a freshly built resolver on the second call, want the cached one")
	}
}

// TestSnapshotDeviceClassResolverStableAcrossPatchLayers checks that the cached resolver still
// answers correctly after the snapshot is forked, committed and reverted. Those operations only
// stack empty layers over the DeviceClasses, which is the invariant the caching relies on.
func TestSnapshotDeviceClassResolverStableAcrossPatchLayers(t *testing.T) {
	classes := map[string]*resourceapi.DeviceClass{
		"class-1":        class1,
		"class-extended": extendedClass,
	}
	snapshot := NewSnapshot(nil, nil, nil, classes)

	resolveAll := func(t *testing.T, stage string) {
		t.Helper()
		resolver := snapshot.DeviceClassResolver()
		if got := resolver.GetDeviceClass(v1.ResourceName(resourceapi.ResourceDeviceClassPrefix + "class-1")); got != class1 {
			t.Errorf("%s: GetDeviceClass(class-1): got %v, want %v", stage, got, class1)
		}
		if got := resolver.GetDeviceClass("example.com/gpu"); got != extendedClass {
			t.Errorf("%s: GetDeviceClass(example.com/gpu): got %v, want %v", stage, got, extendedClass)
		}
	}

	resolveAll(t, "before fork")

	snapshot.Fork()
	resolveAll(t, "after fork")

	// Mutating unrelated parts of the snapshot must not disturb the resolver.
	if err := snapshot.AddNodeResourceSlices("node-1", nil); err != nil {
		t.Fatalf("AddNodeResourceSlices(): unexpected error: %v", err)
	}
	resolveAll(t, "after slice change")

	snapshot.Commit()
	resolveAll(t, "after commit")

	snapshot.Fork()
	snapshot.Revert()
	resolveAll(t, "after revert")
}
