package queue

import (
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// TestNewPriorityQueue tests the creation of a new PriorityQueue.
func TestNewPriorityQueue(t *testing.T) {
	pq := NewPriorityQueue[int](5, time.Millisecond*100)
	assert.NotNil(t, pq)
	assert.Equal(t, 5, pq.capacity)
	assert.Empty(t, pq.Elems())

	// Test removing from an empty queue
	var zero int
	pq.Remove(zero)
	assert.Empty(t, pq.Elems())

	// Test queue with zero capacity
	pqZeroCapacity := NewPriorityQueue[int](0, time.Millisecond*100)
	assert.Empty(t, pqZeroCapacity)

	// Test queue full capacity
	pqFull := NewPriorityQueue[int](2, time.Millisecond*100)
	pqFull.Put(1, 1, time.Millisecond*200)
	pqFull.Put(2, 2, time.Millisecond*200)
	pqFull.Put(3, 3, time.Millisecond*200) // This should remove the element with priority 1
	assert.Len(t, pqFull.Elems(), 2)
	assert.Equal(t, 3, pqFull.Elems()[0])
	assert.Equal(t, 2, pqFull.Elems()[1])
}

// TestPutAndElems tests adding elements and retrieving them in the correct order.
func TestPutAndElems(t *testing.T) {
	pq := NewPriorityQueue[int](5, time.Millisecond*100)
	pq.Put(1, 1, time.Second)
	pq.Put(2, 2, time.Second)
	pq.Put(3, 3, time.Second)
	elems := pq.Elems()
	assert.Len(t, elems, 3)
	assert.Equal(t, 3, elems[0])
	assert.Equal(t, 2, elems[1])
	assert.Equal(t, 1, elems[2])
}

// TestTail tests the Tail method.
func TestTail(t *testing.T) {
	pq := NewPriorityQueue[int](5, time.Millisecond*100)
	pq.Put(1, 1, time.Second)
	pq.Put(2, 2, time.Second)
	pq.Put(3, 3, time.Second)
	tail := pq.Tail()
	assert.Equal(t, 1, tail)
}

// TestRemove tests removing an entry from the queue.
func TestRemove(t *testing.T) {
	pq := NewPriorityQueue[int](5, time.Millisecond*100)

	// Add an entry to the queue.
	pq.Put(1, 1, time.Second) // This adds an entry with value 1 and priority 1.

	// Retrieve the entry to remove it.
	entryToRemove := pq.entries[0] // Get the entry we just added.

	// Now remove the actual entry from the queue.
	pq.Remove(entryToRemove.Value)

	// Check the elements after removal.
	assert.Empty(t, pq.Elems())
}

// TestExpiration tests the expiration of entries.
func TestExpiration(t *testing.T) {
	pq := NewPriorityQueue[int](5, time.Millisecond*100)
	pq.Put(1, 1, time.Millisecond*50)
	pq.Put(2, 2, time.Millisecond*50)
	time.Sleep(time.Millisecond * 60) // Wait for entries to expire.
	pq.cleanup()                      // Manually trigger cleanup.
	assert.Empty(t, pq.Elems())
}

// TestCleanup tests the automatic cleanup of expired entries.
func TestCleanup(t *testing.T) {
	pq := NewPriorityQueue[int](5, time.Millisecond*50)
	pq.Put(1, 1, time.Millisecond*50)
	pq.Put(2, 2, time.Millisecond*50)
	time.Sleep(time.Millisecond * 60) // Wait for entries to expire.

	// Let the cleanup goroutine run.
	time.Sleep(time.Millisecond * 100)
	assert.Empty(t, pq.Elems())
}

// TestPriorityAndExpiration tests that higher priority entries are not removed when they are still valid.
func TestPriorityAndExpiration(t *testing.T) {
	pq := NewPriorityQueue[int](10, time.Millisecond*100)
	pq.Put(1, 1, time.Millisecond*100) // Low priority
	pq.Put(2, 3, time.Millisecond*50)  // High priority
	pq.Put(3, 2, time.Millisecond*100) // Medium priority

	time.Sleep(time.Millisecond * 60) // Wait for the high priority entry to expire.
	// Trigger cleanup
	pq.cleanup()

	elems := pq.Elems()
	assert.Len(t, elems, 2)

	// Check that the high priority entry is gone
	expectedValues := map[int]struct{}{
		1: {},
		3: {},
	}

	for _, elem := range elems {
		_, found := expectedValues[elem]
		assert.True(t, found)
		delete(expectedValues, elem) // Remove found element
	}
	assert.Empty(t, expectedValues)
}

// TestCleanupWithActiveEntries tests that cleanup does not remove active entries.// TestCleanupWithActiveEntries tests that cleanup does not remove active entries.
func TestCleanupWithActiveEntries(t *testing.T) {
	pq := NewPriorityQueue[int](10, time.Millisecond*100)

	pq.Put(1, 1, time.Millisecond*50)  // Low priority, should expire quickly
	pq.Put(2, 2, time.Millisecond*200) // High priority, should still be active

	time.Sleep(time.Millisecond * 70) // Wait for the low priority entry to expire.
	pq.cleanup()                      // Manually trigger cleanup.

	elems := pq.Elems()
	assert.Len(t, elems, 1)
	assert.Equal(t, 2, elems[0])
}

// TestConcurrentAddAndRemove tests adding and removing entries concurrently.
func TestConcurrentAddAndRemove(t *testing.T) {
	pq := NewPriorityQueue[int](10, time.Millisecond*100)
	for i := 1; i <= 5; i++ {
		pq.Put(i, i, time.Second) // Each entry has a TTL of 1s
	}

	var wg sync.WaitGroup
	// Adding entries concurrently
	for i := 6; i <= 10; i++ {
		wg.Add(1)
		go func(val int) {
			defer wg.Done()
			pq.Put(val, val, time.Second) // Each entry has a TTL of 1s
		}(i)
	}

	// Removing entries concurrently
	for i := 1; i <= 5; i++ {
		wg.Add(1)
		go func(val int) {
			defer wg.Done()
			pq.Remove(val)
		}(i)
	}

	wg.Wait() // Wait for all goroutines to finish
	assert.Len(t, pq.Elems(), 5)
	time.Sleep(time.Second)
	assert.Empty(t, pq.Elems())
}

// TestPriorityChange tests changing the priority of an entry.
func TestPriorityChange(t *testing.T) {
	pq := NewPriorityQueue[int](10, time.Millisecond*100)
	pq.Put(1, 1, time.Second) // Low priority
	pq.Put(2, 3, time.Second) // High priority
	pq.Put(3, 2, time.Second) // Medium priority

	// Change priority of entry with value 1 to a higher priority
	entryToChange := pq.entries[0] // This should be the entry with value 1
	pq.Remove(entryToChange.Value) // Remove it first
	pq.Put(1, 4, time.Second)      // Add it back with a higher priority
	elems := pq.Elems()
	assert.Len(t, elems, 3)
	// Check the order: highest priority (4) should come first
	assert.Equal(t, 1, elems[0])
}

// TestOrderAfterMultipleRemovals tests the order of elements after multiple removals.
func TestOrderAfterMultipleRemovals(t *testing.T) {
	pq := NewPriorityQueue[int](10, time.Millisecond*100)
	pq.Put(4, 1, time.Second)      // Low priority
	pq.Put(3, 3, time.Second)      // High priority
	pq.Put(2, 2, time.Second)      // Medium priority
	pq.Put(1, 4, time.Second)      // Highest priority
	pq.Remove(pq.entries[0].Value) // Remove the lowest priority (1)
	pq.Remove(pq.entries[1].Value) // Remove the next (2)

	elems := pq.Elems()
	assert.Len(t, elems, 2)
	assert.Equal(t, 1, elems[0])
	assert.Equal(t, 2, elems[1])
}

// TestMixedOperations tests mixed operations of adding, removing, and expiring entries.
func TestMixedOperations(t *testing.T) {
	pq := NewPriorityQueue[int](10, time.Millisecond*100)

	// Add entries
	pq.Put(1, 1, time.Millisecond*50)  // Low priority, should expire quickly
	pq.Put(2, 3, time.Millisecond*200) // High priority
	pq.Put(3, 2, time.Millisecond*100) // Medium priority

	time.Sleep(time.Millisecond * 80) // Wait for the low priority entry to expire
	pq.cleanup()                      // Clean up expired entries

	// Check remaining entries
	elems := pq.Elems()
	assert.Len(t, elems, 2)

	// Remove the entry with the highest priority (should be 2)
	pq.Remove(elems[0])

	// Check remaining entries
	elems = pq.Elems()
	assert.Len(t, elems, 1)

	// Verify the remaining element
	assert.Equal(t, 3, elems[0])
}

func TestPriorityQueueWithDifferentTypes(t *testing.T) {
	// Test with int type
	pqInt := NewPriorityQueue[int](10, time.Millisecond*100)
	pqInt.Put(1, 1, time.Millisecond*50)
	pqInt.Put(2, 3, time.Millisecond*200)
	pqInt.Put(3, 2, time.Millisecond*100)

	time.Sleep(time.Millisecond * 60)
	pqInt.cleanup()

	elemsInt := pqInt.Elems()
	assert.Len(t, elemsInt, 2)
	assert.Equal(t, 2, elemsInt[0])
	pqInt.Remove(elemsInt[0])
	elemsInt = pqInt.Elems()
	assert.Len(t, elemsInt, 1)
	assert.Equal(t, 3, elemsInt[0])

	// Test with string type
	pqString := NewPriorityQueue[string](10, time.Millisecond*100)
	pqString.Put("a", 1, time.Millisecond*50)
	pqString.Put("b", 3, time.Millisecond*200)
	pqString.Put("c", 2, time.Millisecond*100)

	time.Sleep(time.Millisecond * 60)
	pqString.cleanup()

	elemsString := pqString.Elems()
	assert.Len(t, elemsString, 2)
	assert.Equal(t, "b", elemsString[0])
	pqString.Remove(elemsString[0])
	elemsString = pqString.Elems()
	assert.Len(t, elemsString, 1)
	assert.Equal(t, "c", elemsString[0])

	// Test with custom struct type
	type CustomStruct struct {
		ID   int
		Name string
	}

	pqStruct := NewPriorityQueue[CustomStruct](10, time.Millisecond*100)
	pqStruct.Put(CustomStruct{ID: 1, Name: "a"}, 1, time.Millisecond*50)
	pqStruct.Put(CustomStruct{ID: 2, Name: "b"}, 3, time.Millisecond*200)
	pqStruct.Put(CustomStruct{ID: 3, Name: "c"}, 2, time.Millisecond*100)

	time.Sleep(time.Millisecond * 60)
	pqStruct.cleanup()

	elemsStruct := pqStruct.Elems()
	assert.Len(t, elemsStruct, 2)
	assert.Equal(t, 2, elemsStruct[0].ID)
	assert.Equal(t, "b", elemsStruct[0].Name)

	pqStruct.Remove(elemsStruct[0])
	elemsStruct = pqStruct.Elems()
	assert.Len(t, elemsStruct, 1)
	assert.Equal(t, 3, elemsStruct[0].ID)
	assert.Equal(t, "c", elemsStruct[0].Name)
}
