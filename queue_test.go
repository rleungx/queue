package queue_test

import (
	"sync"
	"testing"
	"time"

	"github.com/rleungx/queue"
	"github.com/stretchr/testify/assert"
)

// TestNewPriorityQueue tests the creation of a new PriorityQueue.
func TestNewPriorityQueue(t *testing.T) {
	t.Parallel()

	pq := queue.NewPriorityQueue[int](5, time.Millisecond*100)
	assert.NotNil(t, pq)
	assert.Equal(t, 5, pq.Capacity())
	assert.Empty(t, pq.Elems())

	// Test removing from an empty queue
	var zero int

	pq.Remove(zero)
	assert.Empty(t, pq.Elems())

	// Test queue with zero capacity
	pqZeroCapacity := queue.NewPriorityQueue[int](0, time.Millisecond*100)
	assert.Empty(t, pqZeroCapacity)

	// Test queue full capacity
	pqFull := queue.NewPriorityQueue[int](2, time.Millisecond*100)
	pqFull.Push(1, 1, time.Millisecond*200)
	pqFull.Push(2, 2, time.Millisecond*200)
	pqFull.Push(3, 3, time.Millisecond*200) // This should remove the element with priority 1
	pqFull.Push(4, 1, time.Millisecond*200) // It will be rejected because the queue is full
	assert.Len(t, pqFull.Elems(), 2)
	assert.Equal(t, 3, pqFull.Elems()[0])
	assert.Equal(t, 2, pqFull.Elems()[1])
}

// TestPushAndElems tests adding elements and retrieving them in the correct order.
func TestPushAndElems(t *testing.T) {
	t.Parallel()

	pq := queue.NewPriorityQueue[int](5, time.Millisecond*100)
	pq.Push(1, 1, time.Second)
	pq.Push(2, 2, time.Second)
	pq.Push(3, 3, time.Second)
	elems := pq.Elems()
	assert.Len(t, elems, 3)
	assert.Equal(t, 3, elems[0])
	assert.Equal(t, 2, elems[1])
	assert.Equal(t, 1, elems[2])
}

// TestPop tests popping elements from the queue.
func TestPop(t *testing.T) {
	t.Parallel()

	pq := queue.NewPriorityQueue[int](5, time.Millisecond*100)
	assert.NotNil(t, pq)

	// Add elements with different priorities
	pq.Push(1, 10, time.Second)
	pq.Push(2, 20, time.Second)
	pq.Push(3, 15, time.Second)
	pq.Push(4, 5, time.Second)
	pq.Push(5, 25, time.Second)

	// Pop elements and check the order
	expectedOrder := []int{5, 2, 3, 1, 4}
	for _, expected := range expectedOrder {
		entry := pq.Pop()
		assert.NotNil(t, entry)
		assert.Equal(t, expected, entry)
	}

	// Ensure the queue is empty after popping all elements
	assert.Nil(t, pq.Pop())
	assert.Empty(t, pq.Elems())
}

// TestPeek tests peeking the highest priority element.
func TestPeek(t *testing.T) {
	t.Parallel()

	pq := queue.NewPriorityQueue[int](5, time.Millisecond*100)
	peek := pq.Peek()
	assert.Empty(t, peek)
	pq.Push(1, 1, time.Second)
	pq.Push(3, 2, time.Second)
	pq.Push(2, 3, time.Second)
	peek = pq.Peek()
	assert.Equal(t, 2, peek)
}

// TestRemove tests removing an entry from the queue.
func TestRemove(t *testing.T) {
	t.Parallel()

	pq := queue.NewPriorityQueue[int](5, time.Millisecond*100)

	// Add an entry to the queue.
	pq.Push(1, 1, time.Second) // This adds an entry with value 1 and priority 1.

	// Now remove the entry from the queue.
	pq.Remove(1)

	// Check the elements after removal.
	assert.Empty(t, pq.Elems())
}

// TestSizeAndEmpty tests getting the size of the queue and checking if it is empty.
func TestSizeAndEmpty(t *testing.T) {
	t.Parallel()

	pq := queue.NewPriorityQueue[int](5, time.Millisecond*100)
	assert.NotNil(t, pq)

	// Initially, the size should be 0
	assert.Equal(t, 0, pq.Size())

	// Add elements to the queue
	pq.Push(1, 10, time.Second*1)
	pq.Push(2, 20, time.Second*1)
	pq.Push(3, 15, time.Second*1)

	// The size should be 3
	assert.Equal(t, 3, pq.Size())

	// Add more elements to the queue
	pq.Push(4, 5, time.Second*1)
	pq.Push(5, 25, time.Second*1)

	// The size should be 5
	assert.Equal(t, 5, pq.Size())

	// Pop elements from the queue
	pq.Pop()
	pq.Pop()

	// The size should be 3
	assert.Equal(t, 3, pq.Size())

	// Pop all elements from the queue
	pq.Pop()
	pq.Pop()
	pq.Pop()

	// The queue should be empty again
	assert.True(t, pq.Empty())
}

// TestExpiration tests the expiration of entries.
func TestExpiration(t *testing.T) {
	t.Parallel()

	pq := queue.NewPriorityQueue[int](5, time.Millisecond*100)
	pq.Push(1, 1, time.Millisecond*50)
	pq.Push(2, 2, time.Millisecond*50)
	time.Sleep(time.Millisecond * 60) // Wait for entries to expire.
	pq.Cleanup()                      // Manually trigger cleanup.
	assert.Empty(t, pq.Elems())
}

// TestCleanup tests the automatic cleanup of expired entries.
func TestCleanup(t *testing.T) {
	t.Parallel()
	pq := queue.NewPriorityQueue[int](5, time.Millisecond*50)
	pq.Push(1, 1, time.Millisecond*50)
	pq.Push(2, 1, time.Millisecond*50)
	pq.Push(3, 1, time.Millisecond*100)
	pq.Push(4, 1, time.Millisecond*100)
	pq.Push(5, 1, time.Millisecond*100)
	time.Sleep(time.Millisecond * 60) // Wait for entries to expire.
	assert.Len(t, pq.Elems(), 3)

	time.Sleep(time.Millisecond * 60) // Wait for entries to expire.
	assert.Empty(t, pq.Elems())
}

// TestPriorityAndExpiration tests that higher priority entries are not removed when they are still valid.
func TestPriorityAndExpiration(t *testing.T) {
	t.Parallel()
	pq := queue.NewPriorityQueue[int](10, time.Millisecond*100)
	pq.Push(1, 1, time.Millisecond*100) // Low priority
	pq.Push(2, 3, time.Millisecond*50)  // High priority
	pq.Push(3, 2, time.Millisecond*100) // Medium priority

	time.Sleep(time.Millisecond * 60) // Wait for the high priority entry to expire.
	// Trigger cleanup
	pq.Cleanup()

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

// TestCleanupWithActiveEntries tests that cleanup does not remove active entries.
func TestCleanupWithActiveEntries(t *testing.T) {
	t.Parallel()

	pq := queue.NewPriorityQueue[int](10, time.Millisecond*100)

	pq.Push(1, 1, time.Millisecond*50)  // Low priority, should expire quickly
	pq.Push(2, 2, time.Millisecond*200) // High priority, should still be active

	time.Sleep(time.Millisecond * 70) // Wait for the low priority entry to expire.
	pq.Cleanup()                      // Manually trigger cleanup.

	elems := pq.Elems()
	assert.Len(t, elems, 1)
	assert.Equal(t, 2, elems[0])
}

// TestConcurrentAddAndRemove tests adding and removing entries concurrently.
func TestConcurrentAddAndRemove(t *testing.T) {
	t.Parallel()

	pq := queue.NewPriorityQueue[int](10, time.Millisecond*100)
	for i := 1; i <= 5; i++ {
		pq.Push(i, i, time.Second) // Each entry has a TTL of 1s
	}

	var wg sync.WaitGroup

	funcP := func(val int) {
		defer wg.Done()
		pq.Push(val, val, time.Second) // Each entry has a TTL of 1s
	}

	funcR := func(val int) {
		defer wg.Done()
		pq.Remove(val)
	}

	// Adding entries concurrently
	for i := 6; i <= 10; i++ {
		wg.Add(1)

		go funcP(i)
	}

	// Removing entries concurrently
	for i := 1; i <= 5; i++ {
		wg.Add(1)

		go funcR(i)
	}

	wg.Wait() // Wait for all goroutines to finish
	assert.Len(t, pq.Elems(), 5)
	time.Sleep(time.Second)
	assert.Empty(t, pq.Elems())
}

// TestPriorityChange tests changing the priority of an entry.
func TestPriorityChange(t *testing.T) {
	t.Parallel()

	pq := queue.NewPriorityQueue[int](10, time.Millisecond*100)
	pq.Push(1, 1, time.Second) // Low priority
	pq.Push(2, 3, time.Second) // High priority
	pq.Push(3, 2, time.Second) // Medium priority

	// Change priority of entry with value 1 to a higher priority
	pq.Remove(1)               // Remove it first
	pq.Push(1, 4, time.Second) // Add it back with a higher priority
	elems := pq.Elems()
	assert.Len(t, elems, 3)
	// Check the order: highest priority (4) should come first
	assert.Equal(t, 1, elems[0])

	// Change priority of entry with value 3 to a lower priority
	pq.Push(2, 1, time.Second) // Change the priority
	elems = pq.Elems()
	assert.Len(t, elems, 3)
	// Check the order: lowest priority (1) should come last
	assert.Equal(t, 2, elems[2])
}

// TestOrderAfterMultipleRemovals tests the order of elements after multiple removals.
func TestOrderAfterMultipleRemovals(t *testing.T) {
	t.Parallel()

	pq := queue.NewPriorityQueue[int](10, time.Millisecond*100)
	pq.Push(4, 1, time.Second) // Low priority
	pq.Push(3, 3, time.Second) // High priority
	pq.Push(2, 2, time.Second) // Medium priority
	pq.Push(1, 4, time.Second) // Highest priority
	pq.Remove(1)               // Remove the highest priority
	pq.Remove(3)               // Remove the next

	elems := pq.Elems()
	assert.Len(t, elems, 2)
	assert.Equal(t, 2, elems[0])
	assert.Equal(t, 4, elems[1])
}

// TestMixedOperations tests mixed operations of adding, removing, and expiring entries.
func TestMixedOperations(t *testing.T) {
	t.Parallel()
	pq := queue.NewPriorityQueue[int](10, time.Millisecond*100)

	// Add entries
	pq.Push(1, 1, time.Millisecond*50)  // Low priority, should expire quickly
	pq.Push(2, 3, time.Millisecond*200) // High priority
	pq.Push(3, 2, time.Millisecond*100) // Medium priority

	time.Sleep(time.Millisecond * 80) // Wait for the low priority entry to expire
	pq.Cleanup()                      // Clean up expired entries

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

func TestDifferentTypes(t *testing.T) {
	t.Parallel()

	// Test with int type
	pqInt := queue.NewPriorityQueue[int](10, time.Millisecond*100)
	pqInt.Push(1, 1, time.Millisecond*50)
	pqInt.Push(2, 3, time.Millisecond*200)
	pqInt.Push(3, 2, time.Millisecond*100)
	time.Sleep(time.Millisecond * 60)
	pqInt.Cleanup()
	elemsInt := pqInt.Elems()
	assert.Len(t, elemsInt, 2)
	assert.Equal(t, 2, elemsInt[0])
	pqInt.Remove(elemsInt[0])
	elemsInt = pqInt.Elems()
	assert.Len(t, elemsInt, 1)
	assert.Equal(t, 3, elemsInt[0])

	// Test with string type
	pqString := queue.NewPriorityQueue[string](10, time.Millisecond*100)
	pqString.Push("a", 1, time.Millisecond*50)
	pqString.Push("b", 3, time.Millisecond*200)
	pqString.Push("c", 2, time.Millisecond*100)
	time.Sleep(time.Millisecond * 60)
	pqString.Cleanup()
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

	pqStruct := queue.NewPriorityQueue[CustomStruct](10, time.Millisecond*100)
	pqStruct.Push(CustomStruct{ID: 1, Name: "a"}, 1, time.Millisecond*50)
	pqStruct.Push(CustomStruct{ID: 2, Name: "b"}, 3, time.Millisecond*200)
	pqStruct.Push(CustomStruct{ID: 3, Name: "c"}, 2, time.Millisecond*100)
	time.Sleep(time.Millisecond * 60)
	pqStruct.Cleanup()
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

func BenchmarkPush(b *testing.B) {
	pq := queue.NewPriorityQueue[int](1000, time.Minute)

	for i := 0; i < b.N; i++ {
		pq.Push(i, i, time.Minute)
	}
}

func BenchmarkPop(b *testing.B) {
	pq := queue.NewPriorityQueue[int](1000, time.Minute)

	for i := 0; i < 1000; i++ {
		pq.Push(i, i, time.Minute)
	}

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		pq.Pop()
	}
}

func BenchmarkElems(b *testing.B) {
	pq := queue.NewPriorityQueue[int](1000, time.Minute)

	for i := 0; i < 1000; i++ {
		pq.Push(i, i, time.Minute)
	}

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		pq.Elems()
	}
}
