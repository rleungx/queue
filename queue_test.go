package queue_test

import (
	"fmt"
	"math"
	"sync"
	"testing"
	"time"

	"github.com/rleungx/queue"
	"github.com/stretchr/testify/assert"
	"go.uber.org/goleak"
)

func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m)
}

func TestNew(t *testing.T) {
	t.Parallel()
	assert := assert.New(t)

	pq := queue.New[int](5, time.Millisecond*100)
	defer pq.Close()
	assert.NotNil(t, pq, "Newly created PriorityQueue should not be nil")
	assert.Equal(5, pq.Capacity(), "Capacity should be 5")
	assert.Empty(pq.Elems(), "Newly created queue should be empty")

	// Test removing from an empty queue
	var zero int
	pq.Remove(zero)
	assert.Empty(pq.Elems(), "Queue should still be empty after removing from an empty queue")

	// Test queue with zero capacity
	pqZeroCapacity := queue.New[int](0, time.Millisecond*100)
	assert.Empty(pqZeroCapacity, "Zero capacity queue should be empty")

	// Test queue full capacity
	pqFull := queue.New[int](2, time.Millisecond*100)
	defer pqFull.Close()

	pqFull.Push(1, 1, time.Millisecond*200)
	pqFull.Push(2, 2, time.Millisecond*200)
	pqFull.Push(3, 3, time.Millisecond*200) // This should remove the element with priority 1
	pqFull.Push(4, 1, time.Millisecond*200) // It will be rejected because the queue is full
	assert.Len(pqFull.Elems(), 2, "Full capacity queue length should be 2")
	assert.Equal(3, pqFull.Elems()[0], "First element should be 3")
	assert.Equal(2, pqFull.Elems()[1], "Second element should be 2")
}

func TestPushAndElems(t *testing.T) {
	t.Parallel()
	assert := assert.New(t)

	pq := queue.New[int](5, time.Millisecond*100)
	defer pq.Close()

	// Test empty queue
	assert.Empty(pq.Elems(), "Newly created queue should be empty")

	// Test adding a single element
	pq.Push(1, 1, time.Second)
	elems := pq.Elems()
	assert.Len(elems, 1, "Queue should have 1 element")
	assert.Equal(1, elems[0], "First element should be 1")

	// Test adding multiple elements, sorted by priority
	pq.Push(2, 2, time.Second)
	pq.Push(3, 3, time.Second)
	elems = pq.Elems()
	assert.Len(elems, 3, "Queue should have 3 elements")
	assert.Equal([]int{3, 2, 1}, elems, "Elements should be sorted by priority")

	// Test adding elements with the same priority
	pq.Push(4, 2, time.Second)
	elems = pq.Elems()
	assert.Len(elems, 4, "Queue should have 4 elements")
	assert.Contains(elems, 4, "Element 4 should be in the queue")
	assert.Equal(3, elems[0], "Highest priority element should still be 3")

	// Test adding to a nearly full queue
	pq.Push(5, 5, time.Second)
	elems = pq.Elems()
	assert.Len(elems, 5, "Queue should have 5 elements (at capacity)")

	// Test adding to a full queue
	pq.Push(6, 6, time.Second)
	elems = pq.Elems()
	assert.Len(elems, 5, "Queue should still have 5 elements")
	assert.Contains(elems, 6, "Element 6 should be added")
	assert.NotContains(elems, 1, "Lowest priority element should be removed")

	// Test updating the priority of an existing element
	pq.Push(4, 7, time.Second)
	elems = pq.Elems()
	assert.Len(elems, 5, "Queue should still have 5 elements")
	assert.Equal(4, elems[0], "Element 4 should now have highest priority")

	// Test adding an element with zero priority
	pq.Push(7, 0, time.Second)
	elems = pq.Elems()
	assert.Len(elems, 5, "Queue should still have 5 elements")
	assert.NotContains(elems, 7, "Element with zero priority should not be added")

	// Test adding an element with a very short TTL
	pq.Push(8, 10, time.Nanosecond)
	time.Sleep(time.Millisecond)
	pq.Cleanup() // Manually trigger cleanup
	elems = pq.Elems()
	assert.Len(elems, 4, "Queue should have 4 elements (after cleanup)")
	assert.NotContains(elems, 8, "Element with a very short TTL should not exist")

	// Clear the queue and test re-adding an element
	for len(pq.Elems()) > 0 {
		pq.Pop()
	}
	assert.Empty(pq.Elems(), "Queue should be empty after popping all elements")

	pq.Push(10, 10, time.Second)
	elems = pq.Elems()
	assert.Len(elems, 1, "Queue should have 1 element after re-adding")
	assert.Equal(10, elems[0], "Re-added element should be 10")
}

func TestPop(t *testing.T) {
	t.Parallel()
	assert := assert.New(t)

	pq := queue.New[int](5, time.Millisecond*100)
	defer pq.Close()
	assert.NotNil(t, pq, "Newly created PriorityQueue should not be nil")

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
		assert.NotNil(t, entry, "Popped element should not be nil")
		assert.Equal(expected, entry, "Popped element should match the expected order")
	}

	// Ensure the queue is empty after popping all elements
	assert.Empty(pq.Pop(), "Popping from an empty queue should return zero value")
	assert.Empty(pq.Elems(), "Queue should be empty after popping all elements")

	// Add some already expired elements
	pq.Push(1, 1, time.Nanosecond)
	pq.Push(2, 2, time.Nanosecond)

	// Wait briefly to ensure elements are expired
	time.Sleep(10 * time.Millisecond)

	// Pop an element from the queue
	value := pq.Pop()

	// Verify that the returned value is the zero value
	assert.Empty(value, "Expected empty string when popping from a queue with all expired elements")

	// Verify that the queue is now empty
	assert.True(pq.Empty(), "Expected queue to be empty after popping all expired elements")
}

func TestPeek(t *testing.T) {
	t.Parallel()
	assert := assert.New(t)

	pq := queue.New[int](5, time.Millisecond*100)
	defer pq.Close()
	peek := pq.Peek()
	assert.Empty(peek, "Peeking empty queue should return zero value")
	pq.Push(1, 1, time.Second)
	pq.Push(3, 2, time.Second)
	pq.Push(2, 3, time.Second)
	peek = pq.Peek()
	assert.Equal(2, peek, "Peek should return the highest priority element")
}

func TestRemove(t *testing.T) {
	t.Parallel()
	assert := assert.New(t)

	pq := queue.New[int](5, time.Millisecond*100)
	defer pq.Close()

	// Test removing from an empty queue
	pq.Remove(1)
	assert.Empty(pq.Elems(), "Removing from an empty queue should not affect the queue")

	// Test removing the only element
	pq.Push(1, 1, time.Second)
	pq.Remove(1)
	assert.Empty(pq.Elems(), "Queue should be empty after removing the only element")

	// Test removing from a queue with multiple elements
	pq.Push(1, 1, time.Second)
	pq.Push(2, 2, time.Second)
	pq.Push(3, 3, time.Second)
	pq.Remove(2)
	elems := pq.Elems()
	assert.Len(elems, 2, "Queue should have 2 elements after removing one from 3")
	assert.Equal(3, elems[0], "First element should be 3")
	assert.Equal(1, elems[1], "Second element should be 1")

	// Test removing the highest priority element
	pq.Remove(3)
	elems = pq.Elems()
	assert.Len(elems, 1, "Queue should have 1 element after removing the highest priority")
	assert.Equal(1, elems[0], "Remaining element should be 1")

	// Test removing a non-existent element
	pq.Remove(4)
	elems = pq.Elems()
	assert.Len(elems, 1, "Queue should still have 1 element after attempting to remove a non-existent element")
	assert.Equal(1, elems[0], "Element should still be 1")

	// Test removing the last element
	pq.Remove(1)
	assert.Empty(pq.Elems(), "Queue should be empty after removing the last element")

	// Test removing from a queue with elements of the same priority
	pq.Push(1, 1, time.Second)
	pq.Push(2, 1, time.Second)
	pq.Push(3, 1, time.Second)
	pq.Remove(2)
	elems = pq.Elems()
	assert.Len(elems, 2, "Queue should have 2 elements after removing one from 3 with same priority")
	assert.Contains(elems, 1, "Queue should contain 1")
	assert.Contains(elems, 3, "Queue should contain 3")

	// Test removing all elements one by one
	pq.Remove(1)
	pq.Remove(3)
	assert.Empty(pq.Elems(), "Queue should be empty after removing all elements one by one")
}

func TestSizeAndEmpty(t *testing.T) {
	t.Parallel()
	assert := assert.New(t)

	pq := queue.New[int](5, time.Millisecond*100)
	defer pq.Close()
	assert.NotNil(t, pq, "Newly created PriorityQueue should not be nil")

	assert.Equal(0, pq.Size(), "Initial size should be 0")

	pq.Push(1, 10, time.Second*1)
	pq.Push(2, 20, time.Second*1)
	pq.Push(3, 15, time.Second*1)

	assert.Equal(3, pq.Size(), "Size should be 3 after adding 3 elements")

	pq.Push(4, 5, time.Second*1)
	pq.Push(5, 25, time.Second*1)

	assert.Equal(5, pq.Size(), "Size should be 5 after adding 5 elements")

	pq.Pop()
	pq.Pop()

	assert.Equal(3, pq.Size(), "Size should be 3 after popping 2 elements")

	pq.Pop()
	pq.Pop()
	pq.Pop()

	assert.True(pq.Empty(), "Queue should be empty after popping all elements")
}

func TestExpiration(t *testing.T) {
	t.Parallel()
	assert := assert.New(t)

	pq := queue.New[int](5, time.Millisecond*100)
	defer pq.Close()

	// Test immediate expiration
	pq.Push(1, 1, time.Nanosecond)
	time.Sleep(time.Millisecond)
	pq.Cleanup()
	assert.Empty(pq.Elems(), "Element with immediate expiration should be removed")

	// Test expiration of multiple elements
	pq.Push(1, 1, time.Millisecond*50)
	pq.Push(2, 2, time.Millisecond*50)
	pq.Push(3, 3, time.Millisecond*150)
	time.Sleep(time.Millisecond * 100)
	pq.Cleanup()
	elems := pq.Elems()
	assert.Len(elems, 1, "Two elements should expire, leaving one")
	assert.Equal(3, elems[0], "Element with longer TTL should remain")

	// Test expiration with priority
	pq.Push(4, 4, time.Millisecond*200)
	pq.Push(5, 5, time.Millisecond*300)
	time.Sleep(time.Millisecond * 150)
	pq.Cleanup()
	elems = pq.Elems()
	if assert.Len(elems, 2, "Two elements should remain") {
		assert.Equal(5, elems[0], "Highest priority element should be first")
		assert.Equal(4, elems[1], "Second highest priority element should be second")
	}

	// Test expiration during operations
	pq.Push(6, 6, time.Millisecond*50)
	time.Sleep(time.Millisecond * 25)
	pq.Push(7, 7, time.Millisecond*100)
	time.Sleep(time.Millisecond * 50)
	pq.Cleanup()
	elems = pq.Elems()
	if assert.Len(elems, 2, "Two elements should remain after operations") {
		assert.Equal(7, elems[0], "Newly added element should be first")
		assert.Equal(5, elems[1], "Previously highest priority element should be second")
	}

	// Test all elements expiring
	time.Sleep(time.Millisecond * 300)
	pq.Cleanup()
	assert.Empty(pq.Elems(), "All elements should expire")

	// Test expiration with constant cleanup
	done := make(chan bool)
	go func() {
		for {
			select {
			case <-done:
				return
			default:
				pq.Cleanup()
				time.Sleep(time.Millisecond * 10)
			}
		}
	}()

	pq.Push(8, 8, time.Millisecond*30)
	pq.Push(9, 9, time.Millisecond*60)
	time.Sleep(time.Millisecond * 45)
	elems = pq.Elems()
	if assert.Len(elems, 1, "One element should expire with constant cleanup") {
		assert.Equal(9, elems[0], "Element with longer TTL should remain")
	}

	time.Sleep(time.Millisecond * 20)
	assert.Empty(pq.Elems(), "All elements should expire with constant cleanup")

	close(done) // Stop the cleanup goroutine
}

func TestCleanup(t *testing.T) {
	t.Parallel()
	assert := assert.New(t)

	pq := queue.New[int](10, time.Millisecond*50)
	defer pq.Close()

	// Add elements with different expiration times
	pq.Push(1, 1, time.Millisecond*50)  // Expires quickly
	pq.Push(2, 2, time.Millisecond*200) // Expires later
	pq.Push(3, 3, time.Millisecond*500) // Expires much later
	pq.Push(4, 4, time.Second)          // Expires very late

	assert.Len(pq.Elems(), 4, "Queue should initially have 4 elements")

	// First cleanup: should only remove the first element
	time.Sleep(time.Millisecond * 70)
	pq.Cleanup()
	elems := pq.Elems()
	assert.Len(elems, 3, "Queue should have 3 elements after first cleanup")
	assert.NotContains(elems, 1, "Element 1 should be removed")

	// Second cleanup: should remove the second element
	time.Sleep(time.Millisecond * 150)
	pq.Cleanup()
	elems = pq.Elems()
	assert.Len(elems, 2, "Queue should have 2 elements after second cleanup")
	assert.NotContains(elems, 2, "Element 2 should be removed")

	// Add a new element while cleaning
	pq.Push(5, 5, time.Millisecond*300)

	// Third cleanup: should keep the newly added element
	time.Sleep(time.Millisecond * 250)
	pq.Cleanup()
	elems = pq.Elems()
	assert.Len(elems, 3, "Queue should have 3 elements")
	assert.Contains(elems, 4, "Element 4 should still be present")
	assert.Contains(elems, 5, "Newly added element 5 should be present")

	// Final cleanup
	time.Sleep(time.Second)
	pq.Cleanup()
	elems = pq.Elems()
	assert.Empty(elems, "Queue should be empty after final cleanup")

	// Test cleanup doesn't affect newly added non-expired element
	pq.Push(6, 6, time.Millisecond*100)
	time.Sleep(time.Millisecond * 50)
	pq.Cleanup()
	elems = pq.Elems()
	assert.Len(elems, 1, "Queue should have 1 element")
	assert.Equal(6, elems[0], "Element 6 should still be present")

	// Ensure the last element is also correctly cleaned up
	time.Sleep(time.Millisecond * 60)
	pq.Cleanup()
	assert.Empty(pq.Elems(), "Queue should be empty after all elements expire")
}

func TestPriorityAndExpiration(t *testing.T) {
	t.Parallel()
	assert := assert.New(t)

	pq := queue.New[int](10, time.Millisecond*100)
	defer pq.Close()
	pq.Push(1, 1, time.Millisecond*100)
	pq.Push(2, 3, time.Millisecond*50)
	pq.Push(3, 2, time.Millisecond*100)

	time.Sleep(time.Millisecond * 60)
	pq.Cleanup()

	elems := pq.Elems()
	assert.Len(elems, 2, "Queue length should be 2 after high priority element expires")

	expectedValues := map[int]struct{}{
		1: {},
		3: {},
	}

	for _, elem := range elems {
		_, found := expectedValues[elem]
		assert.True(found, "Remaining elements should be either 1 or 3")
		delete(expectedValues, elem)
	}

	assert.Empty(expectedValues, "All expected elements should be in the queue")
}

func TestCleanupWithActiveEntries(t *testing.T) {
	t.Parallel()
	assert := assert.New(t)

	pq := queue.New[int](10, time.Millisecond*100)
	defer pq.Close()

	// Add elements with different expiration times
	pq.Push(1, 1, time.Millisecond*50)  // Will expire soon
	pq.Push(2, 2, time.Millisecond*200) // Will expire later
	pq.Push(3, 3, time.Millisecond*500) // Will expire much later
	pq.Push(4, 4, time.Second)          // Will expire last

	assert.Len(pq.Elems(), 4, "Queue should initially have 4 elements")

	// First cleanup: should remove the first element
	time.Sleep(time.Millisecond * 70)
	pq.Cleanup()
	elems := pq.Elems()
	assert.Len(elems, 3, "Queue should have 3 elements after first cleanup")
	assert.NotContains(elems, 1, "Element 1 should be removed")

	// Second cleanup: should remove the second element
	time.Sleep(time.Millisecond * 150)
	pq.Cleanup()
	elems = pq.Elems()
	assert.Len(elems, 2, "Queue should have 2 elements after second cleanup")
	assert.NotContains(elems, 2, "Element 2 should be removed")

	// Add a new element during cleanup process
	pq.Push(5, 5, time.Millisecond*300)

	// Third cleanup: should keep the newly added element
	time.Sleep(time.Millisecond * 250)
	pq.Cleanup()
	elems = pq.Elems()
	assert.Len(elems, 3, "Queue should have 3 elements")
	assert.Contains(elems, 4, "Element 4 should still be present")
	assert.Contains(elems, 5, "Newly added element 5 should be present")

	// Final cleanup
	time.Sleep(time.Second)
	pq.Cleanup()
	elems = pq.Elems()
	assert.Empty(elems, "Queue should be empty after final cleanup")

	// Test cleanup doesn't affect newly added non-expired element
	pq.Push(6, 6, time.Millisecond*100)
	time.Sleep(time.Millisecond * 50)
	pq.Cleanup()
	elems = pq.Elems()
	assert.Len(elems, 1, "Queue should have 1 element")
	assert.Equal(6, elems[0], "Element 6 should still be present")

	// Ensure the last element is also correctly cleaned up
	time.Sleep(time.Millisecond * 60)
	pq.Cleanup()
	assert.Empty(pq.Elems(), "Queue should be empty after all elements expire")
}

func TestConcurrentAddAndRemove(t *testing.T) {
	t.Parallel()
	assert := assert.New(t)

	pq := queue.New[int](10, time.Millisecond*100)
	defer pq.Close()
	for i := 1; i <= 5; i++ {
		pq.Push(i, i, time.Second)
	}

	var wg sync.WaitGroup

	funcP := func(val int) {
		defer wg.Done()
		pq.Push(val, val, time.Second)
	}

	funcR := func(val int) {
		defer wg.Done()
		pq.Remove(val)
	}

	for i := 6; i <= 10; i++ {
		wg.Add(1)
		go funcP(i)
	}

	for i := 1; i <= 5; i++ {
		wg.Add(1)
		go funcR(i)
	}

	wg.Wait()
	assert.Len(pq.Elems(), 5, "Queue length should be 5 after concurrent operations")
	time.Sleep(time.Second)
	assert.Empty(pq.Elems(), "Queue should be empty after all elements expire")
}

func TestPriorityChange(t *testing.T) {
	t.Parallel()
	assert := assert.New(t)

	pq := queue.New[int](10, time.Millisecond*100)
	defer pq.Close()
	pq.Push(1, 1, time.Second)
	pq.Push(2, 3, time.Second)
	pq.Push(3, 2, time.Second)

	pq.Remove(1)
	pq.Push(1, 4, time.Second)
	elems := pq.Elems()
	assert.Len(elems, 3, "Queue length should be 3 after changing priority")
	assert.Equal(1, elems[0], "Highest priority element should be 1")

	pq.Push(2, 1, time.Second)
	elems = pq.Elems()
	assert.Len(elems, 3, "Queue length should be 3 after changing priority")
	assert.Equal(2, elems[2], "Lowest priority element should be 2")
}

func TestOrderAfterMultipleRemovals(t *testing.T) {
	t.Parallel()
	assert := assert.New(t)

	pq := queue.New[int](10, time.Millisecond*100)
	defer pq.Close()
	pq.Push(4, 1, time.Second)
	pq.Push(3, 3, time.Second)
	pq.Push(2, 2, time.Second)
	pq.Push(1, 4, time.Second)
	pq.Remove(1)
	pq.Remove(3)

	elems := pq.Elems()
	assert.Len(elems, 2, "Queue length should be 2 after multiple removals")
	assert.Equal(2, elems[0], "First element should be 2")
	assert.Equal(4, elems[1], "Second element should be 4")
}

func TestMixedOperations(t *testing.T) {
	t.Parallel()
	assert := assert.New(t)

	pq := queue.New[int](10, time.Millisecond*100)
	defer pq.Close()

	pq.Push(1, 1, time.Millisecond*50)
	pq.Push(2, 3, time.Millisecond*200)
	pq.Push(3, 2, time.Millisecond*100)

	time.Sleep(time.Millisecond * 80)
	pq.Cleanup()

	elems := pq.Elems()
	assert.Len(elems, 2, "Queue length should be 2 after cleanup")

	pq.Remove(elems[0])

	elems = pq.Elems()
	assert.Len(elems, 1, "Queue length should be 1 after removal")

	assert.Equal(3, elems[0], "Remaining element should be 3")
}

func TestDifferentTypes(t *testing.T) {
	t.Parallel()
	assert := assert.New(t)

	// Test with int type
	pqInt := queue.New[int](10, time.Millisecond*100)
	defer pqInt.Close()
	pqInt.Push(1, 1, time.Millisecond*50)
	pqInt.Push(2, 3, time.Millisecond*200)
	pqInt.Push(3, 2, time.Millisecond*100)
	time.Sleep(time.Millisecond * 60)
	pqInt.Cleanup()
	elemsInt := pqInt.Elems()
	assert.Len(elemsInt, 2)
	assert.Equal(2, elemsInt[0])
	pqInt.Remove(elemsInt[0])
	elemsInt = pqInt.Elems()
	assert.Len(elemsInt, 1)
	assert.Equal(3, elemsInt[0])

	// Test with string type
	pqString := queue.New[string](10, time.Millisecond*100)
	defer pqString.Close()
	pqString.Push("a", 1, time.Millisecond*50)
	pqString.Push("b", 3, time.Millisecond*200)
	pqString.Push("c", 2, time.Millisecond*100)
	time.Sleep(time.Millisecond * 60)
	pqString.Cleanup()
	elemsString := pqString.Elems()
	assert.Len(elemsString, 2)
	assert.Equal("b", elemsString[0])
	pqString.Remove(elemsString[0])
	elemsString = pqString.Elems()
	assert.Len(elemsString, 1)
	assert.Equal("c", elemsString[0])

	// Test with custom struct type
	type CustomStruct struct {
		ID   int
		Name string
	}

	pqStruct := queue.New[CustomStruct](10, time.Millisecond*100)
	defer pqStruct.Close()
	pqStruct.Push(CustomStruct{ID: 1, Name: "a"}, 1, time.Millisecond*50)
	pqStruct.Push(CustomStruct{ID: 2, Name: "b"}, 3, time.Millisecond*200)
	pqStruct.Push(CustomStruct{ID: 3, Name: "c"}, 2, time.Millisecond*100)
	time.Sleep(time.Millisecond * 60)
	pqStruct.Cleanup()
	elemsStruct := pqStruct.Elems()
	assert.Len(elemsStruct, 2)
	assert.Equal(2, elemsStruct[0].ID)
	assert.Equal("b", elemsStruct[0].Name)
	pqStruct.Remove(elemsStruct[0])
	elemsStruct = pqStruct.Elems()
	assert.Len(elemsStruct, 1)
	assert.Equal(3, elemsStruct[0].ID)
	assert.Equal("c", elemsStruct[0].Name)
}

func TestEdgeCases(t *testing.T) {
	t.Parallel()
	assert := assert.New(t)

	pq := queue.New[string](5, time.Millisecond*100)
	defer pq.Close()

	// Test empty queue operations
	assert.Equal("", pq.Pop(), "Popping from empty queue should return empty string")
	assert.Equal("", pq.Peek(), "Peeking empty queue should return empty string")
	pq.Remove("non-existent")
	assert.True(pq.Empty(), "Queue should be empty")
	assert.Equal(0, pq.Size(), "Empty queue size should be 0")

	// Test adding nil value
	pq.Push("", 1, time.Minute)
	assert.Equal(1, pq.Size(), "Empty string should be added")
	assert.Equal("", pq.Pop(), "Should be able to pop empty string")

	// Test extreme priorities
	pq.Push("lowest priority", math.MinInt64, time.Minute)
	pq.Push("highest priority", math.MaxInt64, time.Minute)
	assert.Equal("highest priority", pq.Pop(), "Should return highest priority element")
	assert.Equal("lowest priority", pq.Pop(), "Should return lowest priority element")

	// Test extremely short and long TTL
	pq.Push("very short TTL", 1, time.Nanosecond)
	pq.Push("very long TTL", 2, time.Hour*24*365*100) // 100 years
	time.Sleep(time.Millisecond)
	pq.Cleanup()
	assert.Equal(1, pq.Size(), "Very short TTL element should expire")
	assert.Equal("very long TTL", pq.Pop(), "Should return very long TTL element")

	// Test capacity boundary
	for i := 0; i < 5; i++ {
		pq.Push(fmt.Sprintf("element%d", i), i, time.Minute)
	}
	assert.Equal(5, pq.Size(), "Queue should reach capacity limit")
	pq.Push("over capacity element", 10, time.Minute)
	assert.Equal(5, pq.Size(), "Queue should not exceed capacity limit")
	assert.Equal("over capacity element", pq.Pop(), "Should return highest priority element")

	// Test large number of same priority elements
	for i := 0; i < 1000; i++ {
		pq.Push(fmt.Sprintf("same priority%d", i), 1, time.Minute)
	}
	assert.Equal(5, pq.Size(), "Queue size should not exceed capacity")

	// Test removing non-existent element
	pq.Remove("non-existent")
	assert.Equal(5, pq.Size(), "Removing non-existent element should not change queue size")

	// Test emptying the queue
	for !pq.Empty() {
		pq.Pop()
	}
	assert.True(pq.Empty(), "Queue should be empty")

	// Test concurrent operations
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			pq.Push(fmt.Sprintf("concurrent element%d", i), i, time.Minute)
			pq.Pop()
		}(i)
	}
	wg.Wait()
	assert.True(pq.Size() <= 5, "Queue size should not exceed capacity after concurrent operations")
}

func TestClose(t *testing.T) {
	t.Parallel()
	assert := assert.New(t)

	pq := queue.New[string](5, time.Millisecond*100)

	// Add some elements
	pq.Push("item1", 1, time.Minute)
	pq.Push("item2", 2, time.Minute)

	assert.Len(pq.Elems(), 2, "Queue should contain 2 elements")

	// Close the queue
	pq.Close()
	pq.Cleanup()

	// Try to add an element after closing
	pq.Push("item3", 3, time.Minute)

	elems := pq.Elems()
	assert.Empty(elems, "Queue should be empty after closing")

	// Try to close the queue again (should not produce an error)
	pq.Close()

	// Verify that the queue state hasn't changed
	elems = pq.Elems()
	assert.Empty(elems, "Queue should remain empty after repeated closing")

	// Verify that the cleanup loop has stopped (this may require waiting for a short time)
	time.Sleep(time.Millisecond * 150)
	assert.Empty(pq.Elems(), "Queue should remain empty after waiting")

	// Test Pop on closed queue
	result := pq.Pop()
	assert.Empty(result, "Pop() on closed queue should return zero value")

	// Test Peek on closed queue
	peeked := pq.Peek()
	assert.Empty(peeked, "Peek() on closed queue should return zero value")

	// Verify queue state hasn't changed after Pop and Peek operations
	elems = pq.Elems()
	assert.Empty(elems, "Queue should remain empty after operations on closed queue")

	// Try to add an element to a closed queue
	pq.Push("item4", 4, time.Minute)
	assert.Empty(pq.Elems(), "Queue should not accept new elements when closed")
}

func TestPopAndPeekExpiredEntries(t *testing.T) {
	t.Parallel()
	assert := assert.New(t)

	pq := queue.New[string](5, time.Millisecond*100)
	defer pq.Close()

	// Add some entries, some of which will expire quickly
	pq.Push("item1", 1, time.Millisecond)
	pq.Push("item2", 2, time.Hour)
	pq.Push("item3", 3, time.Millisecond)
	pq.Push("item4", 4, time.Hour)

	// Wait for some entries to expire
	time.Sleep(10 * time.Millisecond)

	// Test Pop
	popped := pq.Pop()
	assert.Equal("item4", popped, "Pop should return the highest priority non-expired item")

	// Test Peek
	peeked := pq.Peek()
	assert.Equal("item2", peeked, "Peek should return the next highest priority non-expired item")

	// Verify queue size
	assert.Equal(2, pq.Size(), "Queue size should be 2")

	// Verify remaining entries
	elems := pq.Elems()
	expected := []string{"item2"}
	assert.ElementsMatch(expected, elems, "Remaining elements should match expected")

	// Verify queue behavior when only one item remains
	assert.Equal("item2", pq.Pop(), "Pop should return the last remaining item")
	assert.Empty(pq.Peek(), "Peek on empty queue should return zero value")
	assert.Empty(pq.Pop(), "Pop on empty queue should return zero value")
}
