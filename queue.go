package queue

import (
	"container/heap"
	"sync"
	"time"
)

// min returns the minimum of two integers
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// max returns the maximum of two integers
func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// calculateInitialCleanupBatch calculates the initial cleanup batch size based on capacity
func calculateInitialCleanupBatch(capacity int) int {
	// Use smooth adaptive strategy based on capacity ranges
	switch {
	case capacity <= 100:
		// Small queues: batch size = capacity (clean everything when needed)
		return capacity
	case capacity <= 500:
		// Small-medium queues: batch size = 50% of capacity, at least 50
		return max(50, capacity/2)
	case capacity <= 2000:
		// Medium queues: batch size = 25% of capacity, at least 100
		return max(100, capacity/4)
	case capacity <= 10000:
		// Large queues: batch size = 10% of capacity, at least 200, at most 1000
		batchSize := capacity / 10
		return max(200, min(batchSize, 1000))
	case capacity <= 50000:
		// Very large queues: batch size = 5% of capacity, at least 500, at most 2500
		batchSize := capacity / 20
		return max(500, min(batchSize, 2500))
	default:
		// Huge queues: batch size = 2% of capacity, at least 1000, at most 5000
		batchSize := capacity / 50
		return max(1000, min(batchSize, 5000))
	}
}

// calculateAdaptiveCleanupBatch calculates the adaptive cleanup batch size
func (pq *PriorityQueue[T]) calculateAdaptiveCleanupBatch() int {
	baseSize := pq.maxCleanupBatch

	// If we haven't done enough cleanups, use base size
	if pq.totalCleanupsPerformed < 3 {
		return baseSize
	}

	// Calculate adaptation factor based on average expired items
	adaptationFactor := 1.0

	if pq.averageExpiredPerCleanup > float64(baseSize)*0.8 {
		// High expiration rate - increase batch size
		adaptationFactor = 1.5
	} else if pq.averageExpiredPerCleanup > float64(baseSize)*0.5 {
		// Medium expiration rate - slightly increase batch size
		adaptationFactor = 1.2
	} else if pq.averageExpiredPerCleanup < float64(baseSize)*0.1 {
		// Low expiration rate - decrease batch size to save CPU
		adaptationFactor = 0.5
	}

	adaptiveSize := int(float64(baseSize) * adaptationFactor)

	// Ensure adaptive size is within reasonable bounds
	minSize := max(10, baseSize/10)         // At least 10, or 10% of base
	maxSize := min(pq.capacity, baseSize*3) // At most capacity or 3x base

	return max(minSize, min(maxSize, adaptiveSize))
}

// calculateAdaptivePopLimit calculates how many expired items to clean in Pop/Peek
func (pq *PriorityQueue[T]) calculateAdaptivePopLimit() int {
	// Base limit
	baseLimit := 50

	// If average expired per cleanup is high, increase Pop cleanup limit
	if pq.averageExpiredPerCleanup > float64(pq.maxCleanupBatch)*0.7 {
		return baseLimit * 2 // 100
	} else if pq.averageExpiredPerCleanup > float64(pq.maxCleanupBatch)*0.3 {
		return int(float64(baseLimit) * 1.5) // 75
	}

	return baseLimit // 50
}

// PriorityQueue is a priority queue that supports adding, removing, and updating elements with priorities.
type PriorityQueue[T comparable] struct {
	items           map[T]*Entry[T]
	maxHeap         *entryHeap[T]
	minHeap         *entryHeap[T]
	capacity        int
	maxCleanupBatch int // Maximum number of items to clean up in one batch

	// Adaptive cleanup metrics
	expiredItemsInLastCleanup int     // Number of expired items found in last cleanup
	totalCleanupsPerformed    int     // Total number of cleanup operations
	averageExpiredPerCleanup  float64 // Running average of expired items per cleanup

	sync.RWMutex
	done   chan struct{}
	wg     sync.WaitGroup
	closed bool

	entryPool sync.Pool // Pool for entry reuse
}

// New constructs a priority queue.
func New[T comparable](capacity int, cleanupInterval time.Duration) *PriorityQueue[T] {
	if capacity <= 0 || cleanupInterval <= 0 {
		return nil
	}

	pq := &PriorityQueue[T]{
		items:           make(map[T]*Entry[T], capacity),
		maxHeap:         &entryHeap[T]{isMax: true, entries: make([]*Entry[T], 0, capacity)},
		minHeap:         &entryHeap[T]{isMax: false, entries: make([]*Entry[T], 0, capacity)},
		capacity:        capacity,
		maxCleanupBatch: calculateInitialCleanupBatch(capacity), // Use capacity-based calculation
		done:            make(chan struct{}),
		entryPool: sync.Pool{
			New: func() interface{} {
				return &Entry[T]{}
			},
		},
	}

	pq.wg.Add(1) // Start a goroutine to clean up expired entries periodically.
	go pq.cleanupLoop(cleanupInterval)

	return pq
}

// cleanupLoop periodically cleans up expired entries.
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

	expireAt := time.Now().Add(ttl)
	if existingEntry, ok := pq.items[value]; ok {
		pq.updateExistingEntry(existingEntry, priority, expireAt)
		return
	}

	// Get entry from pool for better performance
	entry := pq.entryPool.Get().(*Entry[T])
	entry.Value = value
	entry.Priority = priority
	entry.expireAt = expireAt

	if len(pq.items) >= pq.capacity {
		if !pq.removeLowestIfHigher(entry) {
			// Return entry to pool if not used
			entry.reset()
			pq.entryPool.Put(entry)
			return
		}
	}

	pq.addNewEntry(entry)
}

// Pop returns the highest priority entry and removes it from the queue.
func (pq *PriorityQueue[T]) Pop() (value T) {
	pq.Lock()
	defer pq.Unlock()

	if pq.closed || pq.maxHeap.Len() == 0 {
		return value
	}

	now := time.Now()
	expiredCount := 0
	maxExpiredInPop := pq.calculateAdaptivePopLimit() // Use adaptive limit

	for pq.maxHeap.Len() > 0 {
		entry := pq.maxHeap.entries[0]
		if entry.expireAt.After(now) {
			// Store the value before removing the entry
			result := entry.Value
			pq.removeHighestPriorityEntry()
			return result
		}
		// Remove expired entry
		pq.removeHighestPriorityEntry()
		expiredCount++

		// Prevent Pop() from becoming too slow when many items are expired
		if expiredCount >= maxExpiredInPop {
			// If we've cleaned many expired items but still no valid item found,
			// return zero value to avoid blocking too long
			break
		}
	}

	return value
}

// Peek returns the highest priority entry without removing it.
func (pq *PriorityQueue[T]) Peek() (value T) {
	pq.Lock()
	defer pq.Unlock()

	if pq.closed || pq.maxHeap.Len() == 0 {
		return value
	}
	now := time.Now()
	expiredCount := 0
	maxExpiredInPeek := pq.calculateAdaptivePopLimit() // Use same adaptive limit as Pop

	for pq.maxHeap.Len() > 0 {
		entry := pq.maxHeap.entries[0]
		if entry.expireAt.After(now) {
			return entry.Value
		}
		// Remove expired entry
		pq.removeHighestPriorityEntry()
		expiredCount++

		// Prevent Peek() from becoming too slow when many items are expired
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

	if pq.closed || pq.maxHeap.Len() == 0 {
		return nil
	}
	rs := make([]T, 0, len(pq.maxHeap.entries))
	tempHeap := entryHeap[T]{entries: make([]*Entry[T], len(pq.maxHeap.entries)), isMax: true}

	for i, entry := range pq.maxHeap.entries {
		tempHeap.entries[i] = &Entry[T]{
			Value:    entry.Value,
			Priority: entry.Priority,
			expireAt: entry.expireAt,
			maxIndex: entry.maxIndex,
		}
	}

	heap.Init(&tempHeap)

	for tempHeap.Len() > 0 {
		entry := heap.Pop(&tempHeap).(*Entry[T])
		if !entry.isExpired() {
			rs = append(rs, entry.Value)
		}
	}

	return rs
}

// Remove removes a value from the queue.
func (pq *PriorityQueue[T]) Remove(value T) {
	pq.Lock()
	defer pq.Unlock()

	if entry, ok := pq.items[value]; ok {
		// Remove the entry from the items map
		delete(pq.items, value)

		// Remove the entry from the heaps
		pq.removeFromMaxHeap(entry)
		pq.removeFromMinHeap(entry)

		// Return entry to pool for reuse
		entry.reset()
		pq.entryPool.Put(entry)
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
	pq.Unlock()

	// Wait for the cleanup loop to finish after releasing the lock
	pq.wg.Wait()
}

// Cleanup removes expired entries from the queue using adaptive strategy.
func (pq *PriorityQueue[T]) Cleanup() {
	pq.Lock()
	defer pq.Unlock()

	if pq.closed {
		return
	}

	now := time.Now()

	// Use adaptive cleanup batch size
	maxCleanup := pq.calculateAdaptiveCleanupBatch()
	cleaned := 0

	for pq.minHeap.Len() > 0 && pq.minHeap.entries[0].expireAt.Before(now) && cleaned < maxCleanup {
		entry := heap.Pop(pq.minHeap).(*Entry[T])
		pq.removeFromMaxHeap(entry)
		delete(pq.items, entry.Value)

		// Return entry to pool for reuse
		entry.reset()
		pq.entryPool.Put(entry)

		cleaned++
	}

	// Update adaptive metrics
	pq.updateCleanupMetrics(cleaned)
}

// updateCleanupMetrics updates the metrics used for adaptive cleanup
func (pq *PriorityQueue[T]) updateCleanupMetrics(cleanedCount int) {
	pq.expiredItemsInLastCleanup = cleanedCount
	pq.totalCleanupsPerformed++

	// Calculate running average with exponential decay
	alpha := 0.2 // Smoothing factor
	if pq.totalCleanupsPerformed == 1 {
		pq.averageExpiredPerCleanup = float64(cleanedCount)
	} else {
		pq.averageExpiredPerCleanup = alpha*float64(cleanedCount) + (1-alpha)*pq.averageExpiredPerCleanup
	}
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

	return len(pq.maxHeap.entries)
}

// Empty returns true if the queue is empty.
func (pq *PriorityQueue[T]) Empty() bool {
	pq.RLock()
	defer pq.RUnlock()

	return len(pq.maxHeap.entries) == 0
}

// AdaptiveMetrics returns current adaptive cleanup metrics for monitoring
type AdaptiveMetrics struct {
	AverageExpiredPerCleanup  float64
	TotalCleanupsPerformed    int
	ExpiredItemsInLastCleanup int
	CurrentCleanupBatchSize   int
	CurrentPopLimit           int
}

// GetAdaptiveMetrics returns the current adaptive metrics
func (pq *PriorityQueue[T]) GetAdaptiveMetrics() AdaptiveMetrics {
	pq.RLock()
	defer pq.RUnlock()

	return AdaptiveMetrics{
		AverageExpiredPerCleanup:  pq.averageExpiredPerCleanup,
		TotalCleanupsPerformed:    pq.totalCleanupsPerformed,
		ExpiredItemsInLastCleanup: pq.expiredItemsInLastCleanup,
		CurrentCleanupBatchSize:   pq.calculateAdaptiveCleanupBatch(),
		CurrentPopLimit:           pq.calculateAdaptivePopLimit(),
	}
}

// updateExistingEntry updates an existing entry.
func (pq *PriorityQueue[T]) updateExistingEntry(entry *Entry[T], priority int, expireAt time.Time) {
	if entry.Priority != priority || entry.expireAt != expireAt {
		entry.Priority = priority
		entry.expireAt = expireAt
		heap.Fix(pq.maxHeap, entry.maxIndex)
		heap.Fix(pq.minHeap, entry.minIndex)
	}
}

func (pq *PriorityQueue[T]) removeHighestPriorityEntry() {
	entry := heap.Pop(pq.maxHeap).(*Entry[T])
	pq.removeFromMinHeap(entry)
	delete(pq.items, entry.Value)

	// Return entry to pool for reuse
	entry.reset()
	pq.entryPool.Put(entry)
}

// tryReplaceLowestPriority attempts to replace the lowest priority entry.
func (pq *PriorityQueue[T]) removeLowestIfHigher(newEntry *Entry[T]) bool {
	if len(pq.minHeap.entries) > 0 && pq.minHeap.entries[0].Priority < newEntry.Priority {
		removed := heap.Pop(pq.minHeap).(*Entry[T])
		pq.removeFromMaxHeap(removed)
		delete(pq.items, removed.Value)

		// Return removed entry to pool for reuse
		removed.reset()
		pq.entryPool.Put(removed)

		return true
	}
	return false
}

// addNewEntry adds a new entry to the queue.
func (pq *PriorityQueue[T]) addNewEntry(entry *Entry[T]) {
	heap.Push(pq.maxHeap, entry)
	heap.Push(pq.minHeap, entry)
	pq.items[entry.Value] = entry
}

// Entry is a pair of region and its priority.
type Entry[T any] struct {
	Priority int
	Value    T
	expireAt time.Time
	maxIndex int // The index of the item in the max heap.
	minIndex int // The index of the item in the min heap.
}

// reset resets the entry for reuse in object pool
func (e *Entry[T]) reset() {
	var zero T
	e.Value = zero
	e.Priority = 0
	e.expireAt = time.Time{}
	e.maxIndex = -1
	e.minIndex = -1
}

// isExpired checks if the entry is expired.
func (e *Entry[T]) isExpired() bool {
	return e.expireAt.Before(time.Now())
}

type entryHeap[T comparable] struct {
	entries []*Entry[T]
	isMax   bool
}

func (h entryHeap[T]) Len() int { return len(h.entries) }

// Optimized Less method with branch prediction hints
func (h entryHeap[T]) Less(i, j int) bool {
	ei, ej := h.entries[i], h.entries[j]
	if h.isMax {
		return ei.Priority > ej.Priority
	}
	return ei.Priority < ej.Priority
}

// Optimized Swap method with manual inlining
func (h entryHeap[T]) Swap(i, j int) {
	entries := h.entries
	ei, ej := entries[i], entries[j]
	entries[i], entries[j] = ej, ei

	if h.isMax {
		ei.maxIndex = j
		ej.maxIndex = i
	} else {
		ei.minIndex = j
		ej.minIndex = i
	}
}

func (h *entryHeap[T]) Push(x interface{}) {
	n := len(h.entries)
	item := x.(*Entry[T])
	if h.isMax {
		item.maxIndex = n
	} else {
		item.minIndex = n
	}
	h.entries = append(h.entries, item)
}

func (h *entryHeap[T]) Pop() interface{} {
	old := h.entries
	n := len(old)
	item := old[n-1]
	old[n-1] = nil
	if h.isMax {
		item.maxIndex = -1
	} else {
		item.minIndex = -1
	}
	h.entries = old[0 : n-1]
	return item
}

// removeFromMaxHeap removes an entry from the max heap.
func (pq *PriorityQueue[T]) removeFromMaxHeap(entry *Entry[T]) {
	if entry.maxIndex >= 0 && entry.maxIndex < len(pq.maxHeap.entries) {
		heap.Remove(pq.maxHeap, entry.maxIndex)
	}
}

// removeFromMinHeap removes an entry from the min heap.
func (pq *PriorityQueue[T]) removeFromMinHeap(entry *Entry[T]) {
	if entry.minIndex >= 0 && entry.minIndex < len(pq.minHeap.entries) {
		heap.Remove(pq.minHeap, entry.minIndex)
	}
}
