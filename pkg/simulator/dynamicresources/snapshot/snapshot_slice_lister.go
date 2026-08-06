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

package snapshot

import (
	resourceapi "k8s.io/api/resource/v1"
)

type snapshotSliceLister struct {
	snapshot *Snapshot
}

// TODO(DRA): Actually handle the taint rules.
func (sl snapshotSliceLister) ListWithDeviceTaintRules() ([]*resourceapi.ResourceSlice, error) {
	// resourceSlices is keyed by node, so Len() only bounds the number of nodes
	// holding slices. It is a lower bound on the result size, enough to avoid the
	// first few growths of the slice.
	result := make([]*resourceapi.ResourceSlice, 0, sl.snapshot.resourceSlices.Len())
	sl.snapshot.WalkResourceSlices(func(slices []*resourceapi.ResourceSlice) bool {
		result = append(result, slices...)
		return true
	})
	return result, nil
}
