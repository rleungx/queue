package queue

import (
	"math/bits"
	"slices"
	"sync"
	"time"
)

type cleanupBatchPolicy struct {
	maxCapacity  int
	divisor      int
	minBatchSize int
	maxBatchSize int
}

// cleanupBatchPolicies define the fixed background cleanup budget by queue
// capacity. Small queues sweep everything; larger queues reclaim a bounded
// fraction of capacity per cycle so cleanup latency stays predictable.
var cleanupBatchPolicies = [...]cleanupBatchPolicy{
	{maxCapacity: 100, divisor: 1},
	{maxCapacity: 500, divisor: 2, minBatchSize: 50},
	{maxCapacity: 2000, divisor: 4, minBatchSize: 100},
	{maxCapacity: 10000, divisor: 10, minBatchSize: 200, maxBatchSize: 1000},
	{maxCapacity: 50000, divisor: 20, minBatchSize: 500, maxBatchSize: 2500},
	{maxCapacity: 0, divisor: 50, minBatchSize: 1000, maxBatchSize: 5000},
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func isExpiredAt(expireAtUnix, nowUnix int64) bool {
	return expireAtUnix <= nowUnix
}

// calculateInitialCleanupBatch calculates the initial cleanup batch size based on capacity.
func calculateInitialCleanupBatch(capacity int) int {
	for _, policy := range cleanupBatchPolicies {
		if policy.maxCapacity > 0 && capacity > policy.maxCapacity {
			continue
		}

		batchSize := capacity / policy.divisor
		batchSize = max(policy.minBatchSize, batchSize)
		if policy.maxBatchSize > 0 {
			batchSize = min(policy.maxBatchSize, batchSize)
		}
		return batchSize
	}

	return capacity
}

func (pq *PriorityQueue[T]) cleanupBatchSize() int {
	return pq.maxCleanupBatch
}

func (pq *PriorityQueue[T]) popCleanupLimit() int {
	// Reads may repair at most one background cleanup batch worth of stale roots.
	return pq.cleanupBatchSize()
}

// PriorityQueue is a priority queue that supports adding, removing, and updating elements with priorities.
type PriorityQueue[T comparable] struct {
	items        map[T]*Entry[T]
	priorityHeap *priorityHeap[T]
	expiryHeap   *expiryHeap[T]
	// entryStore keeps the preallocated entries alive so the freelist can reuse
	// them without additional steady-state allocations.
	entryStore      []Entry[T]
	freeEntries     []*Entry[T]
	capacity        int
	maxCleanupBatch int

	expiredItemsInLastCleanup int
	totalCleanupsPerformed    int
	totalExpiredItemsCleaned  int

	sync.RWMutex
	done   chan struct{}
	wg     sync.WaitGroup
	closed bool
}

// New constructs a priority queue.
func New[T comparable](capacity int, cleanupInterval time.Duration) *PriorityQueue[T] {
	if capacity <= 0 || cleanupInterval <= 0 {
		return nil
	}

	entryStore := make([]Entry[T], capacity)
	freeEntries := make([]*Entry[T], 0, capacity)
	for i := range entryStore {
		entryStore[i].priorityIndex = -1
		entryStore[i].expiryIndex = -1
		freeEntries = append(freeEntries, &entryStore[i])
	}

	pq := &PriorityQueue[T]{
		items:           make(map[T]*Entry[T], capacity),
		priorityHeap:    &priorityHeap[T]{entries: make([]*Entry[T], 0, capacity)},
		expiryHeap:      &expiryHeap[T]{entries: make([]*Entry[T], 0, capacity)},
		entryStore:      entryStore,
		freeEntries:     freeEntries,
		capacity:        capacity,
		maxCleanupBatch: calculateInitialCleanupBatch(capacity),
		done:            make(chan struct{}),
	}

	pq.wg.Add(1)
	go pq.cleanupLoop(cleanupInterval)

	return pq
}

func (pq *PriorityQueue[T]) cleanupLoop(interval time.Duration) {
	defer pq.wg.Done()

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			pq.Cleanup()
		case <-pq.done:
			return
		}
	}
}

// Push adds a value to the queue with a priority and TTL.
func (pq *PriorityQueue[T]) Push(value T, priority int, ttl time.Duration) {
	pq.Lock()
	defer pq.Unlock()

	if pq.closed {
		return
	}

	now := time.Now()
	nowUnix := now.UnixNano()
	expireAt := now.Add(ttl).UnixNano()
	if isExpiredAt(expireAt, nowUnix) {
		if existingEntry, ok := pq.items[value]; ok {
			pq.deleteEntry(existingEntry)
		}
		return
	}

	if existingEntry, ok := pq.items[value]; ok {
		pq.updateExistingEntry(existingEntry, priority, expireAt)
		return
	}

	if len(pq.items) >= pq.capacity && pq.hasExpiredEntriesLocked(nowUnix) {
		pq.cleanupExpiredLocked(nowUnix, 1, false)
	}

	entry := pq.allocateEntry()
	entry.Value = value
	entry.Priority = priority
	entry.expireAt = expireAt
	entry.live = true

	if len(pq.items) >= pq.capacity {
		if !pq.removeLowestIfHigher(entry) {
			pq.releaseEntry(entry)
			return
		}
	}

	pq.addNewEntry(entry)
}

// Pop returns the highest priority entry and removes it from the queue.
func (pq *PriorityQueue[T]) Pop() (value T) {
	pq.Lock()
	defer pq.Unlock()

	if pq.closed || pq.priorityHeap.Len() == 0 {
		return value
	}

	now := time.Now()
	nowUnix := now.UnixNano()
	expiredCount := 0
	maxExpiredInPop := pq.popCleanupLimit()

	for pq.priorityHeap.Len() > 0 {
		entry := pq.priorityHeap.entries[0]
		if !isExpiredAt(entry.expireAt, nowUnix) {
			result := entry.Value
			pq.removeHighestPriorityEntry()
			return result
		}

		pq.removeHighestPriorityEntry()
		expiredCount++
		if expiredCount >= maxExpiredInPop {
			break
		}
	}

	return value
}

// Peek returns the highest priority entry without removing it.
func (pq *PriorityQueue[T]) Peek() (value T) {
	pq.RLock()
	if pq.closed || pq.priorityHeap.Len() == 0 {
		pq.RUnlock()
		return value
	}

	now := time.Now()
	nowUnix := now.UnixNano()
	if entry := pq.priorityHeap.entries[0]; !isExpiredAt(entry.expireAt, nowUnix) {
		value = entry.Value
		pq.RUnlock()
		return value
	}
	pq.RUnlock()

	pq.Lock()
	defer pq.Unlock()

	if pq.closed || pq.priorityHeap.Len() == 0 {
		return value
	}

	now = time.Now()
	nowUnix = now.UnixNano()
	expiredCount := 0
	maxExpiredInPeek := pq.popCleanupLimit()

	for pq.priorityHeap.Len() > 0 {
		entry := pq.priorityHeap.entries[0]
		if !isExpiredAt(entry.expireAt, nowUnix) {
			return entry.Value
		}

		pq.removeHighestPriorityEntry()
		expiredCount++
		if expiredCount >= maxExpiredInPeek {
			break
		}
	}

	return value
}

// Elems returns all elements in the queue sorted by priority from high to low.
func (pq *PriorityQueue[T]) Elems() []T {
	pq.RLock()
	defer pq.RUnlock()

	if pq.closed || pq.priorityHeap.Len() == 0 {
		return nil
	}

	nowUnix := time.Now().UnixNano()
	entries := make([]*Entry[T], 0, len(pq.priorityHeap.entries))
	for _, entry := range pq.priorityHeap.entries {
		if entry != nil && entry.live && !isExpiredAt(entry.expireAt, nowUnix) {
			entries = append(entries, entry)
		}
	}

	slices.SortFunc(entries, func(a, b *Entry[T]) int {
		switch {
		case a.Priority > b.Priority:
			return -1
		case a.Priority < b.Priority:
			return 1
		default:
			return 0
		}
	})

	rs := make([]T, len(entries))
	for i, entry := range entries {
		rs[i] = entry.Value
	}

	return rs
}

// Remove removes a value from the queue.
func (pq *PriorityQueue[T]) Remove(value T) {
	pq.Lock()
	defer pq.Unlock()

	if entry, ok := pq.items[value]; ok {
		pq.deleteEntry(entry)
	}
}

// Close stops the cleanup goroutine.
func (pq *PriorityQueue[T]) Close() {
	pq.Lock()
	if pq.closed {
		pq.Unlock()
		return
	}

	close(pq.done)
	pq.closed = true
	pq.clearLocked()
	pq.Unlock()

	pq.wg.Wait()
}

// Cleanup removes expired entries using the queue's fixed cleanup policy.
func (pq *PriorityQueue[T]) Cleanup() {
	pq.Lock()
	defer pq.Unlock()

	if pq.closed {
		return
	}

	nowUnix := time.Now().UnixNano()
	cleaned := pq.cleanupExpiredLocked(nowUnix, pq.cleanupBatchSize(), false)
	pq.updateCleanupMetrics(cleaned)
}

func (pq *PriorityQueue[T]) updateCleanupMetrics(cleanedCount int) {
	pq.expiredItemsInLastCleanup = cleanedCount
	pq.totalCleanupsPerformed++
	pq.totalExpiredItemsCleaned += cleanedCount
}

// Capacity returns the capacity of the queue.
func (pq *PriorityQueue[T]) Capacity() int {
	pq.RLock()
	defer pq.RUnlock()

	return pq.capacity
}

// Size returns the number of elements in the queue.
func (pq *PriorityQueue[T]) Size() int {
	pq.RLock()
	defer pq.RUnlock()

	return len(pq.items)
}

// Empty returns true if the queue is empty.
func (pq *PriorityQueue[T]) Empty() bool {
	pq.RLock()
	defer pq.RUnlock()

	return len(pq.items) == 0
}

// CleanupMetrics reports observed cleanup history and the queue's fixed
// cleanup/read-path policy.
type CleanupMetrics struct {
	AverageExpiredPerCleanup  float64
	TotalCleanupsPerformed    int
	ExpiredItemsInLastCleanup int
	CurrentCleanupBatchSize   int
	CurrentPopLimit           int
}

func (pq *PriorityQueue[T]) cleanupMetricsLocked() CleanupMetrics {
	averageExpiredPerCleanup := 0.0
	if pq.totalCleanupsPerformed > 0 {
		averageExpiredPerCleanup = float64(pq.totalExpiredItemsCleaned) / float64(pq.totalCleanupsPerformed)
	}

	return CleanupMetrics{
		AverageExpiredPerCleanup:  averageExpiredPerCleanup,
		TotalCleanupsPerformed:    pq.totalCleanupsPerformed,
		ExpiredItemsInLastCleanup: pq.expiredItemsInLastCleanup,
		CurrentCleanupBatchSize:   pq.cleanupBatchSize(),
		CurrentPopLimit:           pq.popCleanupLimit(),
	}
}

// GetCleanupMetrics returns cleanup history and the current fixed policy.
func (pq *PriorityQueue[T]) GetCleanupMetrics() CleanupMetrics {
	pq.RLock()
	defer pq.RUnlock()
	return pq.cleanupMetricsLocked()
}

func (pq *PriorityQueue[T]) updateExistingEntry(entry *Entry[T], priority int, expireAt int64) {
	priorityChanged := entry.Priority != priority
	expiryChanged := entry.expireAt != expireAt
	if !priorityChanged && !expiryChanged {
		return
	}

	entry.Priority = priority
	entry.expireAt = expireAt

	if priorityChanged {
		pq.priorityHeap.fix(entry.priorityIndex)
	}
	if expiryChanged {
		pq.expiryHeap.fix(entry.expiryIndex)
	}
}

func (pq *PriorityQueue[T]) deleteEntry(entry *Entry[T]) {
	entry.live = false
	delete(pq.items, entry.Value)
	pq.removeFromPriorityHeap(entry)
	pq.removeFromExpiryHeap(entry)
	pq.releaseEntry(entry)
}

func (pq *PriorityQueue[T]) allocateEntry() *Entry[T] {
	last := len(pq.freeEntries) - 1
	if last >= 0 {
		entry := pq.freeEntries[last]
		pq.freeEntries = pq.freeEntries[:last]
		return entry
	}

	entry := &Entry[T]{}
	entry.priorityIndex = -1
	entry.expiryIndex = -1
	return entry
}

func (pq *PriorityQueue[T]) releaseEntry(entry *Entry[T]) {
	entry.reset()
	pq.freeEntries = append(pq.freeEntries, entry)
}

func (pq *PriorityQueue[T]) clearLocked() {
	for i, entry := range pq.priorityHeap.entries {
		if entry == nil {
			continue
		}
		pq.releaseEntry(entry)
		pq.priorityHeap.entries[i] = nil
	}
	for i := range pq.expiryHeap.entries {
		pq.expiryHeap.entries[i] = nil
	}
	pq.priorityHeap.entries = pq.priorityHeap.entries[:0]
	pq.expiryHeap.entries = pq.expiryHeap.entries[:0]
	clear(pq.items)
}

func (pq *PriorityQueue[T]) removeHighestPriorityEntry() {
	entry := pq.priorityHeap.popMax()
	entry.live = false
	pq.removeFromExpiryHeap(entry)
	delete(pq.items, entry.Value)
	pq.releaseEntry(entry)
}

func (pq *PriorityQueue[T]) removeLowestIfHigher(newEntry *Entry[T]) bool {
	for {
		minIndex := pq.priorityHeap.minIndex()
		if minIndex < 0 {
			return false
		}

		minEntry := pq.priorityHeap.entries[minIndex]
		if !minEntry.live {
			pq.priorityHeap.popMinAt(minIndex)
			pq.releaseEntry(minEntry)
			continue
		}
		if minEntry.Priority >= newEntry.Priority {
			return false
		}

		removed := pq.priorityHeap.popMinAt(minIndex)
		removed.live = false
		pq.removeFromExpiryHeap(removed)
		delete(pq.items, removed.Value)
		pq.releaseEntry(removed)
		return true
	}
}

func (pq *PriorityQueue[T]) addNewEntry(entry *Entry[T]) {
	pq.priorityHeap.push(entry)
	pq.expiryHeap.push(entry)
	pq.items[entry.Value] = entry
}

func (pq *PriorityQueue[T]) cleanupExpiredLocked(nowUnix int64, limit int, eager bool) int {
	cleaned := 0
	for pq.expiryHeap.Len() > 0 &&
		isExpiredAt(pq.expiryHeap.entries[0].expireAt, nowUnix) &&
		(limit <= 0 || cleaned < limit) {
		entry := pq.expiryHeap.popRoot()
		entry.live = false
		delete(pq.items, entry.Value)
		if eager {
			pq.removeFromPriorityHeap(entry)
			pq.releaseEntry(entry)
		}
		cleaned++
	}
	if !eager {
		pq.maybeCompactPriorityHeapLocked()
	}
	return cleaned
}

// Entry is a value and its priority/expiration metadata.
type Entry[T any] struct {
	Priority      int
	Value         T
	expireAt      int64
	priorityIndex int
	expiryIndex   int
	live          bool
}

func (e *Entry[T]) reset() {
	var zero T
	e.Priority = 0
	e.Value = zero
	e.expireAt = 0
	e.priorityIndex = -1
	e.expiryIndex = -1
	e.live = false
}

type priorityHeap[T comparable] struct {
	entries []*Entry[T]
}

const heapBranchingFactor = 2
const heapChildrenPerNode = heapBranchingFactor

func childIndex(parent, childOffset int) int {
	return parent*heapBranchingFactor + 1 + childOffset
}

func parentIndex(index int) int {
	return (index - 1) / heapBranchingFactor
}

func grandParentIndex(index int) int {
	return parentIndex(parentIndex(index))
}

func firstGrandChildIndex(index int) int {
	return childIndex(childIndex(index, 0), 0)
}

func hasGrandParent(index int) bool {
	return index >= firstGrandChildIndex(0)
}

func (h *priorityHeap[T]) Len() int {
	return len(h.entries)
}

func (h *priorityHeap[T]) push(entry *Entry[T]) {
	h.entries = append(h.entries, entry)
	h.setIndex(entry, len(h.entries)-1)
	h.bubbleUp(len(h.entries) - 1)
}

func (h *priorityHeap[T]) popMax() *Entry[T] {
	if len(h.entries) == 0 {
		return nil
	}

	last := len(h.entries) - 1
	removed := h.entries[0]
	if last == 0 {
		h.entries[0] = nil
		h.entries = h.entries[:0]
		removed.priorityIndex = -1
		return removed
	}

	moved := h.entries[last]
	h.entries[last] = nil
	h.entries = h.entries[:last]
	h.entries[0] = moved
	moved.priorityIndex = 0
	removed.priorityIndex = -1
	h.trickleDownMax(0)
	return removed
}

func (h *priorityHeap[T]) popMinAt(index int) *Entry[T] {
	if index < 0 {
		return nil
	}

	last := len(h.entries) - 1
	removed := h.entries[index]
	if index == last {
		h.entries[last] = nil
		h.entries = h.entries[:last]
		removed.priorityIndex = -1
		return removed
	}

	moved := h.entries[last]
	h.entries[last] = nil
	h.entries = h.entries[:last]
	h.entries[index] = moved
	moved.priorityIndex = index
	removed.priorityIndex = -1
	h.fixRootChildMinLevel(index)
	return removed
}

func (h *priorityHeap[T]) minIndex() int {
	switch len(h.entries) {
	case 0:
		return -1
	case 1:
		return 0
	case 2:
		return 1
	default:
		if h.entries[1].Priority <= h.entries[2].Priority {
			return 1
		}
		return 2
	}
}

func (h *priorityHeap[T]) remove(index int) *Entry[T] {
	last := len(h.entries) - 1
	if index < 0 || index > last {
		return nil
	}

	removed := h.entries[index]
	if index != last {
		moved := h.entries[last]
		h.entries[index] = moved
		h.setIndex(moved, index)
	}

	h.entries[last] = nil
	h.entries = h.entries[:last]
	h.setIndex(removed, -1)

	if index < len(h.entries) {
		h.fix(index)
	}

	return removed
}

func (h *priorityHeap[T]) fix(index int) {
	if index < 0 || index >= len(h.entries) {
		return
	}
	if index == 0 {
		h.trickleDownMax(0)
		return
	}

	parent := parentIndex(index)
	if h.isMaxLevel(index) {
		if h.entries[index].Priority < h.entries[parent].Priority {
			h.swap(index, parent)
			h.bubbleUpMin(parent)
			return
		}
		h.trickleDownMax(index)
		return
	}

	if h.entries[index].Priority > h.entries[parent].Priority {
		h.swap(index, parent)
		h.bubbleUpMax(parent)
		return
	}
	h.trickleDownMin(index)
}

func (h *priorityHeap[T]) fixRootChildMinLevel(index int) {
	if index < 0 || index >= len(h.entries) {
		return
	}
	if index == 0 {
		h.trickleDownMax(0)
		return
	}

	if h.entries[index].Priority > h.entries[0].Priority {
		h.swap(index, 0)
		h.trickleDownMax(0)
		return
	}

	h.trickleDownMin(index)
}

func (h *priorityHeap[T]) bubbleUp(index int) {
	if index == 0 {
		return
	}

	parent := parentIndex(index)
	if h.isMaxLevel(index) {
		if h.entries[index].Priority < h.entries[parent].Priority {
			h.swap(index, parent)
			h.bubbleUpMin(parent)
			return
		}
		h.bubbleUpMax(index)
		return
	}

	if h.entries[index].Priority > h.entries[parent].Priority {
		h.swap(index, parent)
		h.bubbleUpMax(parent)
		return
	}
	h.bubbleUpMin(index)
}

func (h *priorityHeap[T]) bubbleUpMax(index int) {
	for hasGrandParent(index) {
		grandParent := grandParentIndex(index)
		if h.entries[index].Priority <= h.entries[grandParent].Priority {
			return
		}
		h.swap(index, grandParent)
		index = grandParent
	}
}

func (h *priorityHeap[T]) bubbleUpMin(index int) {
	for hasGrandParent(index) {
		grandParent := grandParentIndex(index)
		if h.entries[index].Priority >= h.entries[grandParent].Priority {
			return
		}
		h.swap(index, grandParent)
		index = grandParent
	}
}

func (h *priorityHeap[T]) trickleDownMax(index int) {
	for {
		candidate, isGrandChild := h.maxDescendant(index)
		if candidate < 0 {
			return
		}
		if isGrandChild {
			if h.entries[candidate].Priority <= h.entries[index].Priority {
				return
			}
			h.swap(index, candidate)
			parent := parentIndex(candidate)
			if h.entries[candidate].Priority < h.entries[parent].Priority {
				h.swap(candidate, parent)
			}
			index = candidate
			continue
		}
		if h.entries[candidate].Priority > h.entries[index].Priority {
			h.swap(index, candidate)
		}
		return
	}
}

func (h *priorityHeap[T]) trickleDownMin(index int) {
	for {
		candidate, isGrandChild := h.minDescendant(index)
		if candidate < 0 {
			return
		}
		if isGrandChild {
			if h.entries[candidate].Priority >= h.entries[index].Priority {
				return
			}
			h.swap(index, candidate)
			parent := parentIndex(candidate)
			if h.entries[candidate].Priority > h.entries[parent].Priority {
				h.swap(candidate, parent)
			}
			index = candidate
			continue
		}
		if h.entries[candidate].Priority < h.entries[index].Priority {
			h.swap(index, candidate)
		}
		return
	}
}

func (h *priorityHeap[T]) maxDescendant(index int) (int, bool) {
	firstChild := childIndex(index, 0)
	if firstChild >= len(h.entries) {
		return -1, false
	}

	best := firstChild
	bestIsGrandChild := false
	for childOffset := 1; childOffset < heapChildrenPerNode; childOffset++ {
		child := childIndex(index, childOffset)
		if child >= len(h.entries) {
			break
		}
		if h.entries[child].Priority > h.entries[best].Priority {
			best = child
		}
	}

	// Children and grandchildren are not contiguous in the array layout
	// for non-root nodes, so probe the actual descendant positions explicitly.
	for childOffset := 0; childOffset < heapChildrenPerNode; childOffset++ {
		child := childIndex(index, childOffset)
		if child >= len(h.entries) {
			break
		}

		for grandChildOffset := 0; grandChildOffset < heapChildrenPerNode; grandChildOffset++ {
			grandChild := childIndex(child, grandChildOffset)
			if grandChild >= len(h.entries) {
				break
			}
			if h.entries[grandChild].Priority > h.entries[best].Priority {
				best = grandChild
				bestIsGrandChild = true
			}
		}
	}

	return best, bestIsGrandChild
}

func (h *priorityHeap[T]) minDescendant(index int) (int, bool) {
	firstChild := childIndex(index, 0)
	if firstChild >= len(h.entries) {
		return -1, false
	}

	best := firstChild
	bestIsGrandChild := false
	for childOffset := 1; childOffset < heapChildrenPerNode; childOffset++ {
		child := childIndex(index, childOffset)
		if child >= len(h.entries) {
			break
		}
		if h.entries[child].Priority < h.entries[best].Priority {
			best = child
		}
	}

	for childOffset := 0; childOffset < heapChildrenPerNode; childOffset++ {
		child := childIndex(index, childOffset)
		if child >= len(h.entries) {
			break
		}

		for grandChildOffset := 0; grandChildOffset < heapChildrenPerNode; grandChildOffset++ {
			grandChild := childIndex(child, grandChildOffset)
			if grandChild >= len(h.entries) {
				break
			}
			if h.entries[grandChild].Priority < h.entries[best].Priority {
				best = grandChild
				bestIsGrandChild = true
			}
		}
	}

	return best, bestIsGrandChild
}

func (h *priorityHeap[T]) isMaxLevel(index int) bool {
	return bits.Len(uint(index+1))%2 == 1
}

func (h *priorityHeap[T]) setIndex(entry *Entry[T], idx int) {
	entry.priorityIndex = idx
}

func (h *priorityHeap[T]) swap(i, j int) {
	entries := h.entries
	ei, ej := entries[i], entries[j]
	entries[i], entries[j] = ej, ei
	h.setIndex(ei, j)
	h.setIndex(ej, i)
}

type expiryHeap[T comparable] struct {
	entries []*Entry[T]
}

func (h *expiryHeap[T]) Len() int {
	return len(h.entries)
}

func (h *expiryHeap[T]) less(i, j int) bool {
	ei, ej := h.entries[i], h.entries[j]
	if ei.expireAt != ej.expireAt {
		return ei.expireAt < ej.expireAt
	}
	return ei.Priority > ej.Priority
}

func (h *expiryHeap[T]) setIndex(entry *Entry[T], idx int) {
	entry.expiryIndex = idx
}

func (h *expiryHeap[T]) swap(i, j int) {
	entries := h.entries
	ei, ej := entries[i], entries[j]
	entries[i], entries[j] = ej, ei
	h.setIndex(ei, j)
	h.setIndex(ej, i)
}

func (h *expiryHeap[T]) push(entry *Entry[T]) {
	h.entries = append(h.entries, entry)
	h.setIndex(entry, len(h.entries)-1)
	h.siftUp(len(h.entries) - 1)
}

func (h *expiryHeap[T]) popRoot() *Entry[T] {
	if len(h.entries) == 0 {
		return nil
	}

	last := len(h.entries) - 1
	removed := h.entries[0]
	if last == 0 {
		h.entries[0] = nil
		h.entries = h.entries[:0]
		removed.expiryIndex = -1
		return removed
	}

	moved := h.entries[last]
	h.entries[last] = nil
	h.entries = h.entries[:last]
	h.entries[0] = moved
	moved.expiryIndex = 0
	removed.expiryIndex = -1
	h.siftDownRoot()
	return removed
}

func (h *expiryHeap[T]) remove(index int) *Entry[T] {
	last := len(h.entries) - 1
	if index < 0 || index > last {
		return nil
	}

	removed := h.entries[index]
	if index != last {
		h.swap(index, last)
	}

	h.entries[last] = nil
	h.entries = h.entries[:last]
	h.setIndex(removed, -1)

	if index < len(h.entries) && !h.siftDown(index) {
		h.siftUp(index)
	}

	return removed
}

func (h *expiryHeap[T]) fix(index int) {
	if index < 0 || index >= len(h.entries) {
		return
	}
	if !h.siftDown(index) {
		h.siftUp(index)
	}
}

func (h *expiryHeap[T]) siftUp(index int) {
	for index > 0 {
		parent := (index - 1) / 2
		if !h.less(index, parent) {
			return
		}
		h.swap(index, parent)
		index = parent
	}
}

func (h *expiryHeap[T]) siftDown(index int) bool {
	original := index
	for {
		left := 2*index + 1
		if left >= len(h.entries) {
			return index > original
		}

		best := left
		right := left + 1
		if right < len(h.entries) && h.less(right, left) {
			best = right
		}
		if !h.less(best, index) {
			return index > original
		}

		h.swap(index, best)
		index = best
	}
}

func (h *expiryHeap[T]) siftDownRoot() {
	index := 0
	entries := h.entries
	for {
		left := 2*index + 1
		if left >= len(entries) {
			return
		}

		best := left
		right := left + 1
		if right < len(entries) {
			er, el := entries[right], entries[left]
			if er.expireAt < el.expireAt || (er.expireAt == el.expireAt && er.Priority > el.Priority) {
				best = right
			}
		}

		eb, ei := entries[best], entries[index]
		if eb.expireAt > ei.expireAt || (eb.expireAt == ei.expireAt && eb.Priority <= ei.Priority) {
			return
		}

		entries[index], entries[best] = eb, ei
		eb.expiryIndex = index
		ei.expiryIndex = best
		index = best
	}
}

func (pq *PriorityQueue[T]) removeFromPriorityHeap(entry *Entry[T]) {
	if entry.priorityIndex >= 0 && entry.priorityIndex < len(pq.priorityHeap.entries) {
		pq.priorityHeap.remove(entry.priorityIndex)
	}
}

func (pq *PriorityQueue[T]) removeFromExpiryHeap(entry *Entry[T]) {
	if entry.expiryIndex >= 0 && entry.expiryIndex < len(pq.expiryHeap.entries) {
		pq.expiryHeap.remove(entry.expiryIndex)
	}
}

func (pq *PriorityQueue[T]) maybeCompactPriorityHeapLocked() {
	stale := len(pq.priorityHeap.entries) - len(pq.items)
	if stale < len(pq.items) {
		return
	}

	oldEntries := pq.priorityHeap.entries
	pq.priorityHeap.entries = make([]*Entry[T], 0, len(pq.items))
	for _, entry := range oldEntries {
		if entry == nil {
			continue
		}
		if !entry.live {
			pq.releaseEntry(entry)
			continue
		}
		pq.priorityHeap.push(entry)
	}
}

func (pq *PriorityQueue[T]) hasExpiredEntriesLocked(nowUnix int64) bool {
	return pq.expiryHeap.Len() > 0 && isExpiredAt(pq.expiryHeap.entries[0].expireAt, nowUnix)
}
