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
// Each layer holds the whole effective state as of that layer rather than only the
// keys it changed, so a read never walks the stack. Layers are persistent hash array
// mapped tries (see persistentMap) which share all the structure the layer below them
// did not change, so holding a full state per layer costs memory proportional to the
// changes, not to the number of keys.
//
// Time Complexities:
//   - Fork(): O(1).
//   - Commit(): O(P), where P is the number of keys written in the topmost patch,
//     or no-op for a PatchSet with a single patch.
//   - Revert(): O(1).
//   - FindValue(key): O(log32 M) where M is the number of keys, so effectively O(1).
//   - AsMap(): O(M).
//   - SetCurrent(key, value): O(log32 M), effectively O(1).
//   - DeleteCurrent(key): O(log32 M), effectively O(1).
//   - InCurrentPatch(key): O(1).
//
// Variables used in complexity analysis:
//   - M: The number of keys in the flattened PatchSet.
//   - P: The number of modified/deleted entries in a single patch layer.
//
// The topmost layer is held as a transientMap, a mutable view that owns the nodes it
// allocates and so can update them in place. Reads and writes on it run at the speed
// of an ordinary map, and it is frozen back into a shareable persistentMap only when
// Fork or Commit needs a layer boundary there. This is what makes Revert O(1): the
// layer below is already a complete, immutable state, so dropping the top one is a
// slice truncation and the transient it abandons is simply left to the collector.
//
// A PatchSet is not safe for concurrent use.
type PatchSet[K comparable, V any] struct {
	// stack holds the effective state at each layer, bottom-up. Entry i is the state
	// after applying layers 0 through i. It always holds at least one entry, and its
	// last entry is the frozen state that top was forked from.
	stack []*persistentMap[K, V]

	// top is the mutable view of the topmost layer, and the only layer that accepts
	// writes. It supersedes stack[len(stack)-1], which is that layer's state as of
	// the last Fork or Commit.
	top *transientMap[K, V]

	// writes records, per layer, the keys written in that layer and whether the write
	// was a deletion. It is only what InCurrentPatch and WalkCurrentPatchKeys report
	// on - reads never consult it, because each layer already holds its full state.
	//
	// writes[0] is always nil. Every key in the base layer was written there by
	// definition, so the layer's own contents answer the question, and materialising a
	// key set for what is normally the largest layer by far would cost memory
	// proportional to the whole cluster to answer a question about the base layer that
	// no caller has reason to ask - see WalkCurrentPatchKeys.
	writes []map[K]bool
}

// NewPatchSet creates a new PatchSet, initializing it with the provided base patches.
func NewPatchSet[K comparable, V any](patches ...*Patch[K, V]) *PatchSet[K, V] {
	if len(patches) == 0 {
		return newEmptyPatchSet[K, V]()
	}

	stack := make([]*persistentMap[K, V], len(patches))
	writes := make([]map[K]bool, len(patches))
	state := &persistentMap[K, V]{}
	for i, patch := range patches {
		state = patch.applyTo(state)
		stack[i] = state
		if i > 0 {
			writes[i] = patchWrites(patch)
		}
	}

	return &PatchSet[K, V]{
		stack:  stack,
		top:    stack[len(stack)-1].AsTransient(),
		writes: writes,
	}
}

// NewPatchSetFromMap creates a new PatchSet holding the contents of the provided map
// as its base layer. It is cheaper than building a Patch and handing it to NewPatchSet,
// which has to allocate the Patch and its deletion set only to drop them again.
//
// The map is not retained: its contents are copied into the base layer.
func NewPatchSetFromMap[K comparable, V any](source map[K]V) *PatchSet[K, V] {
	base := newPersistentMapFromNativeMap(source)
	return &PatchSet[K, V]{
		stack:  []*persistentMap[K, V]{base},
		top:    base.AsTransient(),
		writes: []map[K]bool{nil},
	}
}

func newEmptyPatchSet[K comparable, V any]() *PatchSet[K, V] {
	base := &persistentMap[K, V]{}
	return &PatchSet[K, V]{
		stack:  []*persistentMap[K, V]{base},
		top:    base.AsTransient(),
		writes: []map[K]bool{nil},
	}
}

// applyTo returns the state that results from applying the patch on top of m. It runs
// the whole patch through a single edit session, so the intermediate states that the
// individual writes pass through are never frozen and never shared.
func (p *Patch[K, V]) applyTo(m *persistentMap[K, V]) *persistentMap[K, V] {
	transient := m.AsTransient()
	for key, value := range p.modified {
		transient.Store(key, value)
	}
	for key := range p.deleted {
		transient.Delete(key)
	}

	return transient.Persistent()
}

// patchWrites records the keys a patch writes, in the form the writes stack keeps them.
func patchWrites[K comparable, V any](patch *Patch[K, V]) map[K]bool {
	written := make(map[K]bool, len(patch.modified)+len(patch.deleted))
	for key := range patch.modified {
		written[key] = false
	}
	for key := range patch.deleted {
		written[key] = true
	}

	return written
}

// Fork adds a new, empty patch layer to the top of the stack.
// Subsequent modifications will be recorded in this new layer.
func (p *PatchSet[K, V]) Fork() {
	// Freezing the transient hands back an immutable state that the new layer can be
	// forked from and that a Revert can restore, and stops the layer being forked from
	// accepting further writes through the old view.
	frozen := p.top.Persistent()
	p.stack[len(p.stack)-1] = frozen

	p.stack = append(p.stack, frozen)
	p.top = frozen.AsTransient()
	p.writes = append(p.writes, map[K]bool{})
}

// Commit merges the topmost patch layer into the one below it.
// If there's only one layer, it's a no-op.
func (p *PatchSet[K, V]) Commit() {
	if len(p.stack) < 2 {
		return
	}

	frozen := p.top.Persistent()
	top := len(p.stack) - 1

	// The merged state becomes the layer below, which is exactly what the reads that
	// were seeing the top layer should keep seeing.
	p.stack[top-1] = frozen
	p.stack = p.stack[:top]

	// The keys written in the dropped layer were written in the layer that absorbs it,
	// which is what the next Revert would have to undo. Skipped for the base layer,
	// which does not track its writes.
	if top-1 > 0 {
		below := p.writes[top-1]
		for key, deleted := range p.writes[top] {
			below[key] = deleted
		}
	}
	p.writes = p.writes[:top]

	p.top = frozen.AsTransient()
}

// Revert removes the topmost patch layer.
// Any modifications or deletions recorded in that layer are discarded.
func (p *PatchSet[K, V]) Revert() {
	if len(p.stack) <= 1 {
		return
	}

	// The transient holding the discarded writes is simply abandoned. It only ever
	// mutated nodes its own edit session allocated - anything inherited from the layer
	// below was copied before being written to - so the state being restored cannot
	// have been touched by it.
	p.stack = p.stack[:len(p.stack)-1]
	p.writes = p.writes[:len(p.writes)-1]
	p.top = p.stack[len(p.stack)-1].AsTransient()
}

// FindValue returns the effective value of a key, or the zero value and false if the
// key is deleted or not present.
func (p *PatchSet[K, V]) FindValue(key K) (value V, found bool) {
	return p.top.Load(key)
}

// AsMap returns the current effective state as a plain map.
//
// The map is freshly built and owned by the caller. Callers that only need to read the
// contents should prefer WalkValues or ListValues, which do not build it.
func (p *PatchSet[K, V]) AsMap() map[K]V {
	return p.top.ToNativeMap()
}

// ListValues returns the effective values in the PatchSet, in no particular order.
// Unlike AsMap it does not build an intermediate map to iterate over.
func (p *PatchSet[K, V]) ListValues() []V {
	values := make([]V, 0, p.top.Len())
	p.top.Range(func(_ K, value V) bool {
		values = append(values, value)
		return true
	})

	return values
}

// WalkValues calls f for every effective value in the PatchSet, stopping early if f
// returns false. Unlike AsMap it does not build an intermediate map to iterate over.
func (p *PatchSet[K, V]) WalkValues(f func(V) bool) {
	p.top.Range(func(_ K, value V) bool {
		return f(value)
	})
}

// Len returns the number of keys with an effective value in the PatchSet.
func (p *PatchSet[K, V]) Len() int {
	return p.top.Len()
}

// SetCurrent adds or updates a key-value pair in the topmost patch layer.
func (p *PatchSet[K, V]) SetCurrent(key K, value V) {
	p.top.Store(key, value)
	if current := p.currentWrites(); current != nil {
		current[key] = false
	}
}

// DeleteCurrent marks a key as deleted in the topmost patch layer.
func (p *PatchSet[K, V]) DeleteCurrent(key K) {
	p.top.Delete(key)
	if current := p.currentWrites(); current != nil {
		current[key] = true
	}
}

// InCurrentPatch reports whether the key was set in the topmost patch layer. A key
// deleted there does not count as set, matching the treatment of a key that was never
// written to the layer at all.
//
// On an unforked PatchSet the topmost layer is the base layer, where every key present
// was set by definition, so this reports whether the key has an effective value.
func (p *PatchSet[K, V]) InCurrentPatch(key K) bool {
	current := p.currentWrites()
	if current == nil {
		_, found := p.top.Load(key)
		return found
	}

	deleted, written := current[key]
	return written && !deleted
}

// IsForked reports whether there is a layer above the base one, which is exactly when
// Revert has something to drop. Revert may always be called - without a Fork under it there
// is simply nothing to discard, so no key's effective value changes.
//
// Callers maintaining state derived from the PatchSet need this to tell an unforked
// PatchSet, where WalkCurrentPatchKeys reports the whole base layer, apart from one whose
// topmost layer is about to be discarded.
func (p *PatchSet[K, V]) IsForked() bool {
	return len(p.stack) > 1
}

// WalkCurrentPatchKeys calls f for every key modified or deleted in the topmost patch
// layer, stopping early if f returns false. When that layer is about to be reverted these
// are exactly the keys whose effective value changes, which lets callers maintaining state
// derived from the PatchSet refresh only what a Revert affects.
//
// Note the topmost layer is the base layer on an unforked PatchSet, and Revert does not
// drop that one - see IsForked, which such a caller has to consult first. In that case
// this walks every key with an effective value, which is the base layer's whole content.
func (p *PatchSet[K, V]) WalkCurrentPatchKeys(f func(K) bool) {
	current := p.currentWrites()
	if current == nil {
		p.top.Range(func(key K, _ V) bool {
			return f(key)
		})
		return
	}

	for key := range current {
		if !f(key) {
			return
		}
	}
}

// currentWrites returns the write record of the topmost layer, or nil when that is the
// base layer, which does not keep one.
func (p *PatchSet[K, V]) currentWrites() map[K]bool {
	return p.writes[len(p.writes)-1]
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

	cloned := &PatchSet[K, V]{
		stack:  make([]*persistentMap[K, V], len(ps.stack)),
		writes: make([]map[K]bool, len(ps.writes)),
	}

	for i, layer := range ps.stack {
		// The topmost layer's authoritative state is the transient, not the frozen
		// entry the stack still holds for it.
		source := layer.Range
		if i == len(ps.stack)-1 {
			source = ps.top.Range
		}

		transient := (&persistentMap[K, V]{}).AsTransient()
		source(func(key K, value V) bool {
			transient.Store(cloneKey(key), cloneValue(value))
			return true
		})
		cloned.stack[i] = transient.Persistent()
	}

	for i, written := range ps.writes {
		if written == nil {
			continue
		}
		clonedWrites := make(map[K]bool, len(written))
		for key, deleted := range written {
			clonedWrites[cloneKey(key)] = deleted
		}
		cloned.writes[i] = clonedWrites
	}

	cloned.top = cloned.stack[len(cloned.stack)-1].AsTransient()
	return cloned
}
