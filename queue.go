package queue

import (
	"container/heap"
	"sync"
	"time"
)

// PriorityQueue queue is a priority queue that supports adding, removing, and updating elements with priorities.
type PriorityQueue[T comparable] struct {
	items    map[T]*Entry[T]
	maxHeap  *entryHeap[T]
	minHeap  *entryHeap[T]
	capacity int
	sync.RWMutex
	done chan struct{}
	wg   sync.WaitGroup
}

// New constructs a priority queue.
func New[T comparable](capacity int, cleanupInterval time.Duration) *PriorityQueue[T] {
	if capacity <= 0 {
		return nil
	}

	pq := &PriorityQueue[T]{
		items:    make(map[T]*Entry[T], capacity),
		maxHeap:  &entryHeap[T]{isMax: true},
		minHeap:  &entryHeap[T]{isMax: false},
		capacity: capacity,
		done:     make(chan struct{}),
	}

	pq.wg.Add(1) // Start a goroutine to clean up expired entries periodically.
	go func() {
		defer pq.wg.Done()
		ticker := time.NewTicker(cleanupInterval)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				pq.Cleanup()
			case <-pq.done:
				return
			}
		}
	}()

	return pq
}

// Push adds a value to the queue with a priority and ttl.
func (pq *PriorityQueue[T]) Push(value T, priority int, ttl time.Duration) {
	pq.Lock()
	defer pq.Unlock()

	expireAt := time.Now().Add(ttl)
	if existingEntry, ok := pq.items[value]; ok {
		oldPriority := existingEntry.Priority
		oldExpireAt := existingEntry.expireAt

		existingEntry.Priority = priority
		existingEntry.expireAt = expireAt

		if oldPriority != priority || oldExpireAt != expireAt {
			heap.Fix(pq.maxHeap, existingEntry.maxIndex)
			heap.Fix(pq.minHeap, existingEntry.minIndex)
		}
		return
	}

	entry := &Entry[T]{
		Value:    value,
		Priority: priority,
		expireAt: expireAt,
	}

	if len(pq.items) >= pq.capacity {
		if len(pq.minHeap.entries) > 0 && pq.minHeap.entries[0].Priority < priority {
			// Remove the element with the lowest priority
			removed := heap.Pop(pq.minHeap).(*Entry[T])
			pq.removeFromMaxHeap(removed)
			delete(pq.items, removed.Value)
		} else {
			// New element's priority is not high enough, don't add it
			return
		}
	}

	heap.Push(pq.maxHeap, entry)
	heap.Push(pq.minHeap, entry)
	pq.items[value] = entry
}

// Pop returns the highest priority entry and removes it from the queue.
func (pq *PriorityQueue[T]) Pop() (value T) {
	pq.Lock()
	defer pq.Unlock()

	if len(pq.maxHeap.entries) == 0 {
		return value
	}

	entry := heap.Pop(pq.maxHeap).(*Entry[T])
	pq.removeFromMinHeap(entry)
	delete(pq.items, entry.Value)

	return entry.Value
}

// Peek returns the highest priority entry without removing it.
func (pq *PriorityQueue[T]) Peek() (value T) {
	pq.RLock()
	defer pq.RUnlock()

	if len(pq.maxHeap.entries) == 0 {
		return value
	}

	return pq.maxHeap.entries[0].Value
}

// Elems returns all elements in the queue sorted by priority from high to low.
func (pq *PriorityQueue[T]) Elems() []T {
	pq.RLock()
	defer pq.RUnlock()

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
	}
}

// Cleanup removes expired entries from the queue.
func (pq *PriorityQueue[T]) Cleanup() {
	pq.Lock()
	defer pq.Unlock()

	now := time.Now()

	// 创建一个临时切片来存储需要删除的条目
	var toRemove []*Entry[T]

	// 遍历最小堆，找出所有过期的条目
	for i := 0; i < len(pq.minHeap.entries); i++ {
		if pq.minHeap.entries[i].expireAt.Before(now) {
			toRemove = append(toRemove, pq.minHeap.entries[i])
		} else {
			// 由于最小堆是按过期时间排序的，一旦遇到未过期的条目，就可以停止遍历
			break
		}
	}

	// 删除所有过期的条目
	for _, entry := range toRemove {
		pq.removeFromMinHeap(entry)
		pq.removeFromMaxHeap(entry)
		delete(pq.items, entry.Value)
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

// Close stops the cleanup goroutine.
func (pq *PriorityQueue[T]) Close() {
	close(pq.done)
	pq.wg.Wait()
}

// Entry is a pair of region and its priority.
type Entry[T any] struct {
	Priority int
	Value    T
	expireAt time.Time
	maxIndex int // The index of the item in the max heap.
	minIndex int // The index of the item in the min heap.
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

func (h entryHeap[T]) Less(i, j int) bool {
	if h.isMax {
		return h.entries[i].Priority > h.entries[j].Priority
	}
	return h.entries[i].Priority < h.entries[j].Priority
}
func (h entryHeap[T]) Swap(i, j int) {
	h.entries[i], h.entries[j] = h.entries[j], h.entries[i]
	if h.isMax {
		h.entries[i].maxIndex = i
		h.entries[j].maxIndex = j
	} else {
		h.entries[i].minIndex = i
		h.entries[j].minIndex = j
	}
}

func (h *entryHeap[T]) Push(x interface{}) {
	n := len(h.entries)
	item := x.(*Entry[T])
	item.maxIndex = n
	h.entries = append(h.entries, item)
}

func (h *entryHeap[T]) Pop() interface{} {
	old := h.entries
	n := len(old)
	item := old[n-1]
	old[n-1] = nil
	item.maxIndex = -1
	h.entries = old[0 : n-1]
	return item
}

// removeFromMaxHeap removes an entry from the max heap.
func (pq *PriorityQueue[T]) removeFromMaxHeap(entry *Entry[T]) {
	for i, e := range pq.maxHeap.entries {
		if e == entry {
			heap.Remove(pq.maxHeap, i)
			return
		}
	}
}

// removeFromMinHeap removes an entry from the min heap.
func (pq *PriorityQueue[T]) removeFromMinHeap(entry *Entry[T]) {
	for i, e := range pq.minHeap.entries {
		if e == entry {
			heap.Remove(pq.minHeap, i)
			return
		}
	}
}
