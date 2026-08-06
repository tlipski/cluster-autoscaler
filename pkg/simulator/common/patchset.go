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

// PatchSet manages a stack of patches, allowing for fork/revert/commit operations.
// It provides a view of the data as if all patches were applied sequentially.
//
// Time Complexities:
//   - Fork(): O(1).
//   - Commit(): O(P), where P is the number of modified/deleted entries
//     in the current patch or no-op for PatchSet with a single patch.
//   - Revert(): O(P), where P is the number of modified/deleted entries
//     in the topmost patch, or O(1) if that patch is empty or the PatchSet
//     holds a single patch.
//   - FindValue(key): O(1) for cached keys and for any key once the cache is
//     in sync, O(N) otherwise.
//   - AsMap(), ListValues(), WalkValues(), Len(): O(1) when the cache is in
//     sync, O(N * P) otherwise, as the cache has to be rebuilt by replaying
//     every layer.
//   - SetCurrent(key, value): O(1).
//   - DeleteCurrent(key): O(1).
//   - InCurrentPatch(key): O(1).
//
// Variables used in complexity analysis:
//   - N: The number of patch layers in the PatchSet.
//   - P: The number of modified/deleted entries in a single patch layer
//
// Caching:
//
// The PatchSet keeps two caches: cache holds the effective value of every key
// known to be present, and deletedCache holds the keys known to be absent.
// Values are stored directly rather than behind a pointer, so a separate set is
// needed to tell "absent" apart from "present with the zero value".
//
// Both caches are filled lazily by FindValue and completely by syncCache. While
// cacheInSync is false the caches are partial: a hit is authoritative, but a miss
// only means the key has not been looked up yet. Once cacheInSync is true the
// caches describe the whole PatchSet, so a miss in both is conclusive.
//
// SetCurrent and DeleteCurrent keep the caches consistent in place, so they
// preserve cacheInSync. Fork and Commit do not change the effective value of any
// key and so leave the caches untouched. Revert drops the cached entries for the
// keys the reverted layer touched and clears cacheInSync, because the values
// underneath may differ - reverting an empty layer changes nothing and is a no-op.
type PatchSet[K comparable, V any] struct {
	// patches is a stack of individual modification layers. The base data is
	// at index 0, and subsequent modifications are layered on top.
	// PatchSet should always contain at least a single patch.
	patches []*Patch[K, V]

	// cache stores the computed effective value for keys that have been accessed.
	// Deleted entries are removed from the map.
	cache map[K]V

	// deletedCache stores the keys that are known to be deleted or non-existent.
	// This enables negative caching.
	deletedCache map[K]struct{}

	// cacheInSync indicates whether the cache map accurately reflects the
	// current state derived from applying all patches in the 'patches' slice.
	cacheInSync bool
}

// NewPatchSet creates a new PatchSet, initializing it with the provided base patches.
func NewPatchSet[K comparable, V any](patches ...*Patch[K, V]) *PatchSet[K, V] {
	return &PatchSet[K, V]{
		patches:      patches,
		cache:        make(map[K]V),
		deletedCache: make(map[K]struct{}),
		cacheInSync:  false,
	}
}

// NewPatchSetFromMap creates a new PatchSet initialized with the provided map.
func NewPatchSetFromMap[K comparable, V any](m map[K]V) *PatchSet[K, V] {
	patch := NewPatchFromMap(m)
	return NewPatchSet(patch)
}

// Fork adds a new, empty patch layer to the top of the stack.
// Subsequent modifications will be recorded in this new layer.
func (p *PatchSet[K, V]) Fork() {
	p.patches = append(p.patches, NewPatch[K, V]())
}

// Commit merges the topmost patch layer into the one below it.
// If there's only one layer (or none), it's a no-op.
func (p *PatchSet[K, V]) Commit() {
	if len(p.patches) < 2 {
		return
	}

	currentPatch := p.patches[len(p.patches)-1]
	previousPatch := p.patches[len(p.patches)-2]
	mergePatchesInPlace(previousPatch, currentPatch)
	p.patches = p.patches[:len(p.patches)-1]
}

// Revert removes the topmost patch layer.
// Any modifications or deletions recorded in that layer are discarded.
func (p *PatchSet[K, V]) Revert() {
	if len(p.patches) <= 1 {
		return
	}

	currentPatch := p.patches[len(p.patches)-1]
	p.patches = p.patches[:len(p.patches)-1]

	if len(currentPatch.modified) == 0 && len(currentPatch.deleted) == 0 {
		return
	}

	for key := range currentPatch.modified {
		delete(p.cache, key)
		delete(p.deletedCache, key)
	}

	for key := range currentPatch.deleted {
		delete(p.cache, key)
		delete(p.deletedCache, key)
	}

	p.cacheInSync = false
}

// FindValue searches for the effective value of a key by looking through the patches
// from top to bottom. It returns the value and true if found, or the zero value and false
// if the key is deleted or not found in any patch.
func (p *PatchSet[K, V]) FindValue(key K) (value V, found bool) {
	if cachedValue, cacheHit := p.cache[key]; cacheHit {
		return cachedValue, true
	}
	if _, isDeleted := p.deletedCache[key]; isDeleted {
		return value, false
	}
	if p.cacheInSync {
		// The caches describe every key in the PatchSet, so missing from both
		// of them is conclusive and there is no need to walk the layers.
		return value, false
	}

	found = false
	for i := len(p.patches) - 1; i >= 0; i-- {
		patch := p.patches[i]
		if patch.IsDeleted(key) {
			break
		}

		foundValue, ok := patch.Get(key)
		if ok {
			value = foundValue
			found = true
			break
		}
	}

	if found {
		p.cache[key] = value
	} else {
		p.deletedCache[key] = struct{}{}
	}

	return value, found
}

// AsMap returns the current effective state of the PatchSet as a map.
//
// The returned map is the PatchSet's internal cache rather than a copy: it must
// not be modified, and it only reflects the PatchSet until the next call that
// mutates it. Prefer WalkValues or ListValues for iteration.
func (p *PatchSet[K, V]) AsMap() map[K]V {
	if !p.cacheInSync {
		p.syncCache()
	}
	return p.cache
}

// ListValues returns a slice of all effective values in the PatchSet.
func (p *PatchSet[K, V]) ListValues() []V {
	if !p.cacheInSync {
		p.syncCache()
	}
	result := make([]V, 0, len(p.cache))
	for _, val := range p.cache {
		result = append(result, val)
	}
	return result
}

// WalkValues iterates over all effective values in the PatchSet.
func (p *PatchSet[K, V]) WalkValues(f func(V) bool) {
	if !p.cacheInSync {
		p.syncCache()
	}
	for _, val := range p.cache {
		if !f(val) {
			return
		}
	}
}

// Len returns the number of keys in the effective state of the PatchSet, that
// is across all of its layers. Use InCurrentPatch to inspect a single layer.
func (p *PatchSet[K, V]) Len() int {
	if !p.cacheInSync {
		p.syncCache()
	}
	return len(p.cache)
}

// syncCache rebuilds the cache by applying all patches from bottom to top.
func (p *PatchSet[K, V]) syncCache() {
	if p.cacheInSync {
		return
	}

	// Clear the caches instead of reallocating them. Every Revert of a non-empty
	// layer forces a rebuild, so this runs once per simulated scheduling attempt
	// and reusing the already sized maps keeps it allocation free.
	clear(p.cache)
	clear(p.deletedCache)

	for _, patch := range p.patches {
		for key, value := range patch.modified {
			p.cache[key] = value
			delete(p.deletedCache, key)
		}
		for key := range patch.deleted {
			delete(p.cache, key)
			p.deletedCache[key] = struct{}{}
		}
	}
	p.cacheInSync = true
}

// SetCurrent adds or updates a key-value pair in the topmost patch layer.
func (p *PatchSet[K, V]) SetCurrent(key K, value V) {
	if len(p.patches) == 0 {
		p.Fork()
	}

	currentPatch := p.patches[len(p.patches)-1]
	currentPatch.Set(key, value)
	p.cache[key] = value
	delete(p.deletedCache, key)
}

// DeleteCurrent marks a key as deleted in the topmost patch layer.
func (p *PatchSet[K, V]) DeleteCurrent(key K) {
	if len(p.patches) == 0 {
		p.Fork()
	}

	currentPatch := p.patches[len(p.patches)-1]
	currentPatch.Delete(key)
	delete(p.cache, key)
	p.deletedCache[key] = struct{}{}
}

// InCurrentPatch checks if the key is available in the topmost patch layer.
func (p *PatchSet[K, V]) InCurrentPatch(key K) bool {
	if len(p.patches) == 0 {
		return false
	}

	currentPatch := p.patches[len(p.patches)-1]
	_, found := currentPatch.Get(key)
	return found
}

// WalkCurrentPatchKeys calls f for every key modified or deleted in the topmost patch
// layer, stopping early if f returns false. These are exactly the keys whose effective
// value can change when the layer is reverted, which lets callers maintaining state
// derived from the PatchSet refresh only what a Revert affects.
func (p *PatchSet[K, V]) WalkCurrentPatchKeys(f func(K) bool) {
	if len(p.patches) == 0 {
		return
	}

	currentPatch := p.patches[len(p.patches)-1]
	for key := range currentPatch.modified {
		if !f(key) {
			return
		}
	}
	for key := range currentPatch.deleted {
		if !f(key) {
			return
		}
	}
}

// ClonePatchSet creates a deep copy of a PatchSet object with the same patch layers
// structure, while copying keys and values using cloneKey and cloneValue functions
// provided.
//
// This function is intended for testing purposes only.
func ClonePatchSet[K comparable, V any](ps *PatchSet[K, V], cloneKey func(K) K, cloneValue func(V) V) *PatchSet[K, V] {
	if ps == nil {
		return nil
	}

	cloned := NewPatchSet[K, V]()
	for _, patch := range ps.patches {
		clonedPatch := NewPatch[K, V]()
		for key, value := range patch.modified {
			clonedKey, clonedValue := cloneKey(key), cloneValue(value)
			clonedPatch.Set(clonedKey, clonedValue)
		}

		for key := range patch.deleted {
			clonedKey := cloneKey(key)
			clonedPatch.Delete(clonedKey)
		}

		cloned.patches = append(cloned.patches, clonedPatch)
	}

	return cloned
}
