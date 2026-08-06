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

package common

import (
	"fmt"
	"testing"
)

// The benchmarks below model the access patterns the PatchSet actually sees in
// the simulator: a large base layer populated once from the cluster state, then
// a Fork/mutate/read/Revert cycle per scheduling attempt, with FindValue lookups
// dominating by call count.

var benchSizes = []int{100, 1000, 10000}

func benchKeys(n int) []string {
	keys := make([]string, n)
	for i := range keys {
		keys[i] = fmt.Sprintf("key-%06d", i)
	}
	return keys
}

func benchPatchSet(n int) (*PatchSet[string, int], []string) {
	keys := benchKeys(n)
	base := make(map[string]int, n)
	for i, key := range keys {
		base[key] = i
	}
	return NewPatchSetFromMap(base), keys
}

// BenchmarkPatchSetFindValueWarm measures the hottest operation - a lookup that
// the cache can serve directly.
func BenchmarkPatchSetFindValueWarm(b *testing.B) {
	const n = 10000
	ps, keys := benchPatchSet(n)
	for _, key := range keys {
		ps.FindValue(key)
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ps.FindValue(keys[i%n])
	}
}

// BenchmarkPatchSetFindValueCold measures populating the cache one key at a time,
// which is what happens on the first pass over a fresh snapshot.
func BenchmarkPatchSetFindValueCold(b *testing.B) {
	const n = 10000
	keys := benchKeys(n)
	base := make(map[string]int, n)
	for i, key := range keys {
		base[key] = i
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ps := NewPatchSetFromMap(base)
		for _, key := range keys {
			ps.FindValue(key)
		}
	}
}

// BenchmarkPatchSetFindValueMissing measures lookups for keys that are absent,
// which must be answered without walking the whole patch stack every time.
func BenchmarkPatchSetFindValueMissing(b *testing.B) {
	for _, depth := range []int{1, 5, 20} {
		b.Run(fmt.Sprintf("depth=%d", depth), func(b *testing.B) {
			ps, keys := benchPatchSet(1000)
			for d := 0; d < depth; d++ {
				ps.Fork()
				ps.SetCurrent(keys[d], d)
			}

			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				ps.FindValue("absent")
			}
		})
	}
}

// BenchmarkPatchSetEmptyForkRevert models a scheduling attempt that forks the
// snapshot but never touches it - the common case when a cluster carries DRA or
// CSI objects that a given pod does not use. An empty layer changes nothing, so
// the following read must not trigger a rebuild.
func BenchmarkPatchSetEmptyForkRevert(b *testing.B) {
	for _, n := range benchSizes {
		b.Run(fmt.Sprintf("n=%d", n), func(b *testing.B) {
			ps, _ := benchPatchSet(n)
			ps.Len()

			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				ps.Fork()
				ps.Revert()
				ps.WalkValues(func(int) bool { return true })
			}
		})
	}
}

// BenchmarkPatchSetForkMutateListRevert models a scheduling attempt that does
// allocate: fork, record a change, list the result, then roll back. The Revert
// invalidates the cache, so every iteration pays for a full rebuild.
func BenchmarkPatchSetForkMutateListRevert(b *testing.B) {
	for _, n := range benchSizes {
		b.Run(fmt.Sprintf("n=%d", n), func(b *testing.B) {
			ps, keys := benchPatchSet(n)
			ps.Len()

			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				ps.Fork()
				ps.SetCurrent(keys[i%n], i)
				ps.ListValues()
				ps.Revert()
			}
		})
	}
}

// BenchmarkPatchSetListValuesInSync isolates the cost of listing when the cache
// already reflects the current state.
func BenchmarkPatchSetListValuesInSync(b *testing.B) {
	for _, n := range benchSizes {
		b.Run(fmt.Sprintf("n=%d", n), func(b *testing.B) {
			ps, _ := benchPatchSet(n)
			ps.Len()

			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				ps.ListValues()
			}
		})
	}
}

// BenchmarkPatchSetRebuildDeepStack measures a rebuild across several stacked
// layers, covering the nested Fork case.
func BenchmarkPatchSetRebuildDeepStack(b *testing.B) {
	const n = 1000
	for _, depth := range []int{1, 5, 20} {
		b.Run(fmt.Sprintf("depth=%d", depth), func(b *testing.B) {
			ps, keys := benchPatchSet(n)
			for d := 0; d < depth; d++ {
				ps.Fork()
				for j := 0; j < 10; j++ {
					ps.SetCurrent(keys[(d*10+j)%n], d)
				}
			}

			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				ps.Fork()
				ps.SetCurrent("scratch", i)
				ps.Revert()
				ps.Len()
			}
		})
	}
}

// BenchmarkPatchSetRebuildWithDeletions covers a rebuild over a layer that
// deletes a large share of the base, which is what fills deletedCache.
func BenchmarkPatchSetRebuildWithDeletions(b *testing.B) {
	const n = 10000
	ps, keys := benchPatchSet(n)
	ps.Fork()
	for i := 0; i < n/2; i++ {
		ps.DeleteCurrent(keys[i])
	}
	ps.Len()

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ps.Fork()
		ps.SetCurrent("scratch", i)
		ps.Revert()
		ps.Len()
	}
}
