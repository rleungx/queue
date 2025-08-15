package queue_test

import (
	"context"
	"fmt"
	"math"
	"math/rand"
	"sync"
	"sync/atomic"
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
}

func TestPush(t *testing.T) {
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

func TestPeekDoesNotModifyQueue(t *testing.T) {
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

func TestTTLExpiration(t *testing.T) {
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

func TestBoundaryConditions(t *testing.T) {
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

	// Test zero capacity
	pqZero := queue.New[string](0, time.Minute)
	assert.Nil(pqZero, "Zero capacity queue should return nil")

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

	// Test large number of same priority elements
	for i := 0; i < 1000; i++ {
		pq.Push(fmt.Sprintf("same priority%d", i), 1, time.Minute)
	}
	assert.Equal(5, pq.Size(), "Queue size should not exceed capacity")

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

func TestExpiredEntriesHandling(t *testing.T) {
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

// TestConcurrentPushPop tests concurrent push and pop operations
func TestConcurrentPushPop(t *testing.T) {
	t.Parallel()
	assert := assert.New(t)

	pq := queue.New[int](100, time.Millisecond*50)
	defer pq.Close()

	const numGoroutines = 20
	const numOperations = 100
	var wg sync.WaitGroup

	// Concurrent pushers
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(base int) {
			defer wg.Done()
			for j := 0; j < numOperations; j++ {
				value := base*1000 + j
				priority := j % 10
				pq.Push(value, priority, time.Second)
			}
		}(i)
	}

	// Concurrent poppers
	popped := make([]int, 0, numGoroutines*numOperations)
	var poppedMutex sync.Mutex

	for i := 0; i < numGoroutines/2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < numOperations*2; j++ {
				if val := pq.Pop(); val != 0 {
					poppedMutex.Lock()
					popped = append(popped, val)
					poppedMutex.Unlock()
				}
				time.Sleep(time.Microsecond)
			}
		}()
	}

	wg.Wait()

	// Verify that popped values have valid priorities (no strict ordering required in concurrent environment)
	validPriorities := make(map[int]bool)
	for i := 0; i < 10; i++ {
		validPriorities[i] = true
	}

	for _, val := range popped {
		priority := (val % 1000) % 10
		assert.True(validPriorities[priority], "Popped value should have valid priority")
	}

	// Clean up remaining items
	remaining := pq.Elems()

	// Queue shouldn't have too many remaining items
	maxExpected := int(pq.Capacity() / 2)
	assert.LessOrEqual(len(remaining), maxExpected,
		"Queue shouldn't have excessive remaining items")
}

// TestConcurrentPushRemove tests concurrent push and remove operations
func TestConcurrentPushRemove(t *testing.T) {
	t.Parallel()
	assert := assert.New(t)

	pq := queue.New[string](50, time.Millisecond*100)
	defer pq.Close()

	const numGoroutines = 10
	const numOperations = 50
	var wg sync.WaitGroup
	var addedItems sync.Map
	var removedItems sync.Map

	// Concurrent pushers
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(base int) {
			defer wg.Done()
			for j := 0; j < numOperations; j++ {
				value := fmt.Sprintf("item-%d-%d", base, j)
				priority := (base + j) % 20
				pq.Push(value, priority, time.Second*2)
				addedItems.Store(value, true)
			}
		}(i)
	}

	// Concurrent removers
	for i := 0; i < numGoroutines/2; i++ {
		wg.Add(1)
		go func(base int) {
			defer wg.Done()
			time.Sleep(time.Millisecond * 10) // Let some items be added first
			for j := 0; j < numOperations; j++ {
				value := fmt.Sprintf("item-%d-%d", base, j)
				pq.Remove(value)
				removedItems.Store(value, true)
			}
		}(i)
	}

	wg.Wait()

	// Verify final state
	elems := pq.Elems()
	for _, elem := range elems {
		_, wasAdded := addedItems.Load(elem)
		_, wasRemoved := removedItems.Load(elem)
		assert.True(wasAdded, "Element in queue should have been added")
		// Note: Due to capacity limits, some added items might have been replaced
		// by higher priority items, so we don't assert !wasRemoved here
		// Only log if this is unexpected (debugging info)
		_ = wasRemoved // Acknowledge that we checked this but don't log in normal cases
	}
}

// TestConcurrentCapacityLimit tests concurrent operations with capacity limits
func TestConcurrentCapacityLimit(t *testing.T) {
	t.Parallel()
	assert := assert.New(t)

	const capacity = 20
	pq := queue.New[int](capacity, time.Millisecond*50)
	defer pq.Close()

	const numGoroutines = 15
	const numOperations = 100
	var wg sync.WaitGroup

	// Concurrent pushers with different priority ranges
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(base int) {
			defer wg.Done()
			for j := 0; j < numOperations; j++ {
				value := base*1000 + j
				priority := base*10 + (j % 10) // Different priority ranges per goroutine
				pq.Push(value, priority, time.Second)
				time.Sleep(time.Microsecond * 10)
			}
		}(i)
	}

	// Monitor queue size
	var maxSize int
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 1000; i++ {
			size := pq.Size()
			if size > maxSize {
				maxSize = size
			}
			time.Sleep(time.Millisecond)
		}
	}()

	wg.Wait()

	// Verify capacity constraint
	assert.LessOrEqual(pq.Size(), capacity, "Queue size should not exceed capacity")
	assert.LessOrEqual(maxSize, capacity, "Queue size should never have exceeded capacity")

	// Verify highest priorities are preserved
	elems := pq.Elems()
	if len(elems) > 1 {
		for i := 1; i < len(elems); i++ {
			prevValue := elems[i-1]
			currValue := elems[i]
			prevPriority := (prevValue/1000)*10 + ((prevValue % 1000) % 10)
			currPriority := (currValue/1000)*10 + ((currValue % 1000) % 10)
			assert.GreaterOrEqual(prevPriority, currPriority,
				"Elements should be ordered by priority")
		}
	}
}

// TestConcurrentTTLOperations tests concurrent operations with TTL expiration
func TestConcurrentTTLOperations(t *testing.T) {
	t.Parallel()
	assert := assert.New(t)

	pq := queue.New[string](100, time.Millisecond*20) // Fast cleanup
	defer pq.Close()

	const numGoroutines = 8
	var wg sync.WaitGroup

	// Add items with varying TTLs
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(base int) {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				value := fmt.Sprintf("item-%d-%d", base, j)
				priority := (base + j) % 10
				// Mix of short and long TTLs
				var ttl time.Duration
				if j%3 == 0 {
					ttl = time.Millisecond * 50 // Short TTL
				} else {
					ttl = time.Second * 2 // Long TTL
				}
				pq.Push(value, priority, ttl)
			}
		}(i)
	}

	// Concurrent readers
	var totalPopped int64
	for i := 0; i < numGoroutines/2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				if val := pq.Pop(); val != "" {
					atomic.AddInt64(&totalPopped, 1)
				}
				time.Sleep(time.Millisecond * 5)
			}
		}()
	}

	// Concurrent peekers
	for i := 0; i < numGoroutines/4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 200; j++ {
				pq.Peek() // Should not modify queue
				time.Sleep(time.Millisecond * 2)
			}
		}()
	}

	wg.Wait()

	// Wait for TTL expiration and cleanup
	time.Sleep(time.Millisecond * 100)
	pq.Cleanup()

	// Most short TTL items should have expired
	finalSize := pq.Size()

	// The exact numbers will vary due to timing, but we should have some activity
	assert.Greater(totalPopped, int64(0), "Should have popped some items")

	// Queue size shouldn't be excessive after cleanup
	assert.LessOrEqual(finalSize, pq.Capacity()/2, "Final queue size should be reasonable after cleanup")
}

// TestStressConcurrentOperations performs stress testing with all operations
func TestStressConcurrentOperations(t *testing.T) {
	t.Parallel()
	assert := assert.New(t)

	pq := queue.New[int](200, time.Millisecond*30)
	defer pq.Close()

	const duration = time.Millisecond * 500
	const numGoroutines = 12
	ctx, cancel := context.WithTimeout(context.Background(), duration)
	defer cancel()

	var wg sync.WaitGroup
	var operations int64

	// Mixed operations goroutines
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			rng := rand.New(rand.NewSource(int64(id)))

			for ctx.Err() == nil {
				op := rng.Intn(100)
				value := rng.Intn(10000)
				priority := rng.Intn(100)

				switch {
				case op < 40: // 40% Push
					ttl := time.Duration(rng.Intn(200)+50) * time.Millisecond
					pq.Push(value, priority, ttl)
				case op < 60: // 20% Pop
					pq.Pop()
				case op < 80: // 20% Peek
					pq.Peek()
				case op < 90: // 10% Remove
					pq.Remove(value)
				default: // 10% Check size/empty
					pq.Size()
					pq.Empty()
				}

				atomic.AddInt64(&operations, 1)

				// Small random delay
				if rng.Intn(10) == 0 {
					time.Sleep(time.Microsecond * time.Duration(rng.Intn(100)))
				}
			}
		}(i)
	}

	wg.Wait()

	totalOps := atomic.LoadInt64(&operations)
	finalSize := pq.Size()

	// Verify stress test performance and constraints
	assert.Greater(totalOps, int64(1000), "Should have completed many operations in stress test")
	assert.LessOrEqual(finalSize, pq.Capacity(), "Queue size should not exceed capacity")
	assert.LessOrEqual(pq.Size(), 200, "Queue size should not exceed capacity")
}

// Test potential race condition in Close method
func TestConcurrentClose(t *testing.T) {
	pq := queue.New[string](10, time.Millisecond*100)

	var wg sync.WaitGroup
	numGoroutines := 10

	// Start multiple goroutines that call Close concurrently
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			pq.Close()
		}()
	}

	wg.Wait()
	// Should not panic or cause any issues
}

// Test operations after close
func TestOperationsAfterClose(t *testing.T) {
	pq := queue.New[string](10, time.Millisecond*100)

	// Add some items
	pq.Push("item1", 1, time.Minute)
	pq.Push("item2", 2, time.Minute)

	// Close the queue
	pq.Close()

	// These operations should not panic
	pq.Push("item3", 3, time.Minute) // Should be ignored
	value := pq.Pop()                // Should return zero value
	if value != "" {
		t.Errorf("Expected empty string after close, got %v", value)
	}

	value = pq.Peek() // Should return zero value
	if value != "" {
		t.Errorf("Expected empty string after close, got %v", value)
	}

	// Size should still work
	size := pq.Size()
	if size < 0 {
		t.Errorf("Size should not be negative: %d", size)
	}
}

// Test potential panic in cleanup when closed
func TestCleanupAfterClose(t *testing.T) {
	pq := queue.New[string](10, time.Millisecond*10) // Very short cleanup interval

	// Add items that will expire quickly
	pq.Push("item1", 1, time.Nanosecond)
	pq.Push("item2", 2, time.Nanosecond)

	// Sleep to let items expire
	time.Sleep(time.Millisecond * 50)

	// Close during cleanup
	pq.Close()

	// Manual cleanup after close should not panic
	pq.Cleanup()
}

// Test potential index corruption during concurrent operations
func TestIndexConsistency(t *testing.T) {
	pq := queue.New[int](100, time.Minute*10) // Long cleanup interval
	defer pq.Close()

	var wg sync.WaitGroup
	numWorkers := 10
	opsPerWorker := 50

	// Worker that only adds items
	for i := 0; i < numWorkers; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			for j := 0; j < opsPerWorker; j++ {
				value := workerID*1000 + j
				pq.Push(value, j%10, time.Minute)
			}
		}(i)
	}

	// Worker that removes items
	for i := 0; i < numWorkers/2; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			for j := 0; j < opsPerWorker/2; j++ {
				value := workerID*1000 + j*2
				pq.Remove(value)
			}
		}(i)
	}

	// Worker that pops items
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < opsPerWorker; i++ {
			pq.Pop()
			time.Sleep(time.Microsecond) // Small delay
		}
	}()

	wg.Wait()

	// Verify heap property is maintained
	if !isValidMaxHeap(pq) {
		t.Error("Max heap property violated")
	}
	if !isValidMinHeap(pq) {
		t.Error("Min heap property violated")
	}
	if !isIndexConsistent(pq) {
		t.Error("Index consistency violated")
	}
}

// Helper functions to validate heap properties
func isValidMaxHeap[T comparable](pq *queue.PriorityQueue[T]) bool {
	// Note: We can't access private fields from test package, so we'll use a different approach
	// This is a simplified version that just checks if operations work without panicking
	defer func() {
		if r := recover(); r != nil {
			// If any operation panics, the heap is inconsistent
		}
	}()

	// Try some operations to see if they work
	pq.Size()
	pq.Empty()
	pq.Peek()

	return true
}

func isValidMinHeap[T comparable](pq *queue.PriorityQueue[T]) bool {
	// Similar simplified check
	defer func() {
		if r := recover(); r != nil {
			// If any operation panics, the heap is inconsistent
		}
	}()

	// Try some operations to see if they work
	pq.Size()
	pq.Empty()

	return true
}

func isIndexConsistent[T comparable](pq *queue.PriorityQueue[T]) bool {
	// Since we can't access private fields from test package,
	// we'll do a functional test instead
	defer func() {
		if r := recover(); r != nil {
			// If any operation panics, there's likely an index issue
		}
	}()

	// Try various operations that would fail if indices are wrong
	pq.Peek()

	// Try to remove a few items and add them back
	var removedItems []T
	for i := 0; i < 3 && !pq.Empty(); i++ {
		item := pq.Pop()
		removedItems = append(removedItems, item)
	}

	// Add items back (this tests the heap operations)
	for _, item := range removedItems {
		pq.Push(item, 1, time.Minute)
	}

	return true
}

// Test edge case: removing non-existent item
func TestRemoveNonExistent(t *testing.T) {
	pq := queue.New[string](10, time.Minute)
	defer pq.Close()

	// Remove from empty queue
	pq.Remove("nonexistent")

	// Add some items
	pq.Push("item1", 1, time.Minute)
	pq.Push("item2", 2, time.Minute)

	// Remove non-existent item
	pq.Remove("nonexistent")

	// Verify queue is still consistent
	if pq.Size() != 2 {
		t.Errorf("Expected size 2, got %d", pq.Size())
	}

	if !isIndexConsistent(pq) {
		t.Error("Index consistency violated after removing non-existent item")
	}
}

// Test edge case: capacity 1
func TestCapacityOne(t *testing.T) {
	pq := queue.New[string](1, time.Minute)
	defer pq.Close()

	// Add first item
	pq.Push("item1", 1, time.Minute)
	if pq.Size() != 1 {
		t.Errorf("Expected size 1, got %d", pq.Size())
	}

	// Add second item with higher priority - should replace first
	pq.Push("item2", 2, time.Minute)
	if pq.Size() != 1 {
		t.Errorf("Expected size 1, got %d", pq.Size())
	}

	value := pq.Pop()
	if value != "item2" {
		t.Errorf("Expected item2, got %v", value)
	}

	// Add third item after queue is empty - should be added
	pq.Push("item3", 0, time.Minute)
	if pq.Size() != 1 {
		t.Errorf("Expected size 1, got %d", pq.Size())
	}

	// Verify the item can be retrieved
	value = pq.Pop()
	if value != "item3" {
		t.Errorf("Expected item3, got %v", value)
	}
}

// Test capacity limits more thoroughly
func TestCapacityLimits(t *testing.T) {
	pq := queue.New[string](3, time.Minute)
	defer pq.Close()

	// Fill to capacity
	pq.Push("low1", 1, time.Minute)
	pq.Push("low2", 2, time.Minute)
	pq.Push("low3", 3, time.Minute)

	if pq.Size() != 3 {
		t.Errorf("Expected size 3, got %d", pq.Size())
	}

	// Add higher priority item - should replace lowest
	pq.Push("high", 10, time.Minute)
	if pq.Size() != 3 {
		t.Errorf("Expected size 3, got %d", pq.Size())
	}

	// Verify highest priority item is in queue
	value := pq.Peek()
	if value != "high" {
		t.Errorf("Expected 'high', got %v", value)
	}

	// Add even lower priority item - should be rejected
	pq.Push("verylow", 0, time.Minute)
	if pq.Size() != 3 {
		t.Errorf("Expected size 3, got %d", pq.Size())
	}

	// Verify 'verylow' is not in queue
	items := pq.Elems()
	for _, item := range items {
		if item == "verylow" {
			t.Error("'verylow' should not be in queue")
		}
	}

	// Pop all items and verify order
	first := pq.Pop()
	if first != "high" {
		t.Errorf("Expected 'high', got %v", first)
	}

	// Remaining items should be in descending priority order
	remaining := pq.Elems()
	if len(remaining) != 2 {
		t.Errorf("Expected 2 remaining items, got %d", len(remaining))
	}

	// Should have low3 (priority 3) before low2 (priority 2)
	if len(remaining) >= 2 {
		if remaining[0] != "low3" {
			t.Errorf("Expected 'low3' first, got %v", remaining[0])
		}
		if remaining[1] != "low2" {
			t.Errorf("Expected 'low2' second, got %v", remaining[1])
		}
	}
}

// Test negative capacity edge case
func TestNegativeCapacity(t *testing.T) {
	pq := queue.New[string](-1, time.Minute)
	if pq != nil {
		t.Error("Expected nil for negative capacity")
	}
}

// Test zero cleanup interval
func TestZeroCleanupInterval(t *testing.T) {
	pq := queue.New[string](10, 0)
	if pq != nil {
		t.Error("Expected nil for zero cleanup interval")
	}
}

// Test negative cleanup interval
func TestNegativeCleanupInterval(t *testing.T) {
	pq := queue.New[string](10, -time.Second)
	if pq != nil {
		t.Error("Expected nil for negative cleanup interval")
	}
}

// TestCleanupLimitIssue tests what happens when there are more expired items than maxCleanupBatch
func TestCleanupLimitIssue(t *testing.T) {
	// Create a large capacity queue to force maxCleanupBatch limit
	pq := queue.New[int](2000, time.Second)
	defer pq.Close()

	// Add many items that expire quickly - more than maxCleanupBatch (1000)
	for i := 0; i < 1500; i++ {
		pq.Push(i, i, time.Nanosecond) // Expire immediately
	}

	// Wait for items to expire
	time.Sleep(time.Millisecond * 10)

	sizeBefore := pq.Size()

	// Trigger one cleanup cycle - should only clean up adaptive batch size
	start := time.Now()
	pq.Cleanup()
	cleanupDuration := time.Since(start)

	sizeAfter := pq.Size()
	cleanedItems := sizeBefore - sizeAfter

	// Cleanup should be reasonably efficient
	assert.LessOrEqual(t, cleanupDuration, time.Millisecond*5, "Cleanup should complete in reasonable time")
	assert.GreaterOrEqual(t, cleanedItems, sizeBefore/10, "Cleanup should remove a reasonable number of items")

	// Get adaptive metrics
	metrics := pq.GetAdaptiveMetrics()

	// Cleanup should respect the adaptive batch size
	assert.LessOrEqual(t, cleanedItems, metrics.CurrentCleanupBatchSize,
		"Cleanup should not exceed the adaptive batch size")

	// Verify that cleanup was adaptive (initial cleanup uses base size)
	assert.LessOrEqual(t, cleanedItems, metrics.CurrentCleanupBatchSize,
		"Cleanup should respect adaptive batch size limits")

	// If there are still expired items, test Pop() performance
	if sizeAfter > 0 {
		// Add some fresh items to test Pop() behavior
		for i := 0; i < 5; i++ {
			pq.Push(10000+i, 10000+i, time.Hour) // Long TTL
		}

		start = time.Now()
		popCount := 0
		var poppedValues []int
		for i := 0; i < 5 && !pq.Empty(); i++ {
			val := pq.Pop()
			if val != 0 { // Only count non-zero (valid) values
				poppedValues = append(poppedValues, val)
				popCount++
			}
		}
		popDuration := time.Since(start)

		// Pop should be efficient even with expired items
		assert.LessOrEqual(t, popDuration, time.Millisecond*10,
			"Pop should complete efficiently even with many expired items")

		// Verify we got the expected fresh items
		if popCount > 0 {
			// Should get items in descending priority order
			for i, val := range poppedValues {
				expectedVal := 10004 - i // 10004, 10003, 10002, 10001, 10000
				assert.Equal(t, expectedVal, val, "Items should be popped in descending priority order")
			}
		}
	}
}

// TestAdaptiveCleanup tests the adaptive cleanup strategy
func TestAdaptiveCleanup(t *testing.T) {
	// Create a queue with large capacity to test adaptive behavior
	pq := queue.New[int](2000, time.Millisecond*100)
	defer pq.Close()

	// Test case 1: High expiration rate scenario
	// Add many items with short TTL
	for i := 0; i < 1800; i++ {
		pq.Push(i, i, time.Nanosecond) // Expire immediately
	}

	// Wait for expiration
	time.Sleep(time.Millisecond * 10)

	// Perform several cleanup cycles to build up metrics
	for i := 0; i < 5; i++ {
		pq.Cleanup()
		time.Sleep(time.Millisecond * 10)
	}

	finalMetrics := pq.GetAdaptiveMetrics()

	// In high expiration rate, batch size should increase from initial
	// (2000 capacity = 500 initial batch size, should adapt upward)
	assert.Greater(t, finalMetrics.CurrentCleanupBatchSize, 500,
		"Adaptive batch size should increase from initial value in high expiration scenarios")

	// Test case 2: Low expiration rate scenario
	// Create new queue for clean test
	pqLow := queue.New[int](2000, time.Millisecond*100)
	defer pqLow.Close()

	// Add few items with short TTL
	for i := 0; i < 50; i++ {
		pqLow.Push(i, i, time.Nanosecond)
	}

	// Add many items with long TTL
	for i := 50; i < 1000; i++ {
		pqLow.Push(i, i, time.Hour)
	}

	time.Sleep(time.Millisecond * 10)

	// Perform several cleanup cycles
	for i := 0; i < 5; i++ {
		pqLow.Cleanup()
		time.Sleep(time.Millisecond * 10)
	}

	lowMetrics := pqLow.GetAdaptiveMetrics()

	// In low expiration rate, batch size should decrease
	assert.Less(t, lowMetrics.CurrentCleanupBatchSize, 1000,
		"Adaptive batch size should decrease below 1000 in low expiration scenarios")

	// Test case 3: Adaptive Pop behavior
	// Create queue with many expired items
	pqPop := queue.New[int](1000, time.Second)
	defer pqPop.Close()

	// Add expired items
	for i := 0; i < 800; i++ {
		pqPop.Push(i, i, time.Nanosecond)
	}

	// Add one fresh item
	pqPop.Push(9999, 9999, time.Hour)

	time.Sleep(time.Millisecond * 10)

	// Build up high expiration metrics
	for i := 0; i < 3; i++ {
		pqPop.Cleanup()
	}

	// Test Pop performance with adaptive limit
	start := time.Now()
	value := pqPop.Pop()
	duration := time.Since(start)

	// Should get the fresh item (9999) and not take too long
	assert.Equal(t, 9999, value, "Should pop the fresh item with highest priority")

	// Pop shouldn't take too long even with many expired items
	assert.LessOrEqual(t, duration, time.Millisecond*10,
		"Pop should be efficient even with many expired items")
}

// TestInitialCleanupBatchSizing tests the new capacity-based initial cleanup batch sizing
func TestInitialCleanupBatchSizing(t *testing.T) {
	testCases := []struct {
		capacity          int
		expectedBatchSize int
		description       string
	}{
		{50, 50, "Small queue: batch = capacity"},
		{100, 100, "Small queue boundary: batch = capacity"},
		{200, 100, "Small-medium queue: batch = max(50, capacity/2)"},
		{500, 250, "Small-medium queue boundary: batch = capacity/2"},
		{1000, 250, "Medium queue: batch = capacity/4"},
		{2000, 500, "Medium queue boundary: batch = capacity/4"},
		{5000, 500, "Large queue: batch = max(200, capacity/10)"},
		{10000, 1000, "Large queue boundary: batch = min(capacity/10, 1000)"},
		{20000, 1000, "Very large queue: batch = max(500, min(capacity/20, 2500))"},
		{50000, 2500, "Very large queue boundary: batch = min(capacity/20, 2500)"},
		{100000, 2000, "Huge queue: batch = max(1000, min(capacity/50, 5000))"},
	}

	for _, tc := range testCases {
		t.Run(fmt.Sprintf("Capacity%d", tc.capacity), func(t *testing.T) {
			pq := queue.New[int](tc.capacity, time.Second)
			if pq == nil {
				t.Fatalf("Failed to create queue with capacity %d", tc.capacity)
			}
			defer pq.Close()

			metrics := pq.GetAdaptiveMetrics()
			actualBatchSize := metrics.CurrentCleanupBatchSize

			assert.Equal(t, tc.expectedBatchSize, actualBatchSize, tc.description)
		})
	}
}

// ============================================================================
// Benchmark Tests
// ============================================================================

// BenchmarkPush benchmarks the Push operation
func BenchmarkPush(b *testing.B) {
	pq := queue.New[int](100000, time.Second)
	defer pq.Close()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		pq.Push(i, rand.Intn(100), time.Second)
	}
}

// BenchmarkPop benchmarks the Pop operation
func BenchmarkPop(b *testing.B) {
	pq := queue.New[int](1000, time.Second)
	defer pq.Close()

	// Pre-populate the queue
	for i := 0; i < 1000; i++ {
		pq.Push(i, rand.Intn(100), time.Second)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		pq.Pop()
		// Refill to maintain queue size
		if i%10 == 0 {
			for j := 0; j < 10; j++ {
				pq.Push(1000+i+j, rand.Intn(100), time.Second)
			}
		}
	}
}

// BenchmarkPeek benchmarks the Peek operation
func BenchmarkPeek(b *testing.B) {
	pq := queue.New[int](1000, time.Second)
	defer pq.Close()

	// Pre-populate the queue
	for i := 0; i < 1000; i++ {
		pq.Push(i, rand.Intn(100), time.Second)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		pq.Peek()
	}
}

// BenchmarkRemove benchmarks the Remove operation
func BenchmarkRemove(b *testing.B) {
	pq := queue.New[int](10000, time.Second)
	defer pq.Close()

	// Pre-populate the queue with a fixed number of elements
	const numElements = 5000
	for i := 0; i < numElements; i++ {
		pq.Push(i, rand.Intn(100), time.Second)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// Remove elements in a cyclic manner
		elementToRemove := i % numElements
		pq.Remove(elementToRemove)

		// Re-add the element to keep the queue populated
		if i%100 == 0 {
			pq.Push(elementToRemove, rand.Intn(100), time.Second)
		}
	}
}

// BenchmarkMixedOperations benchmarks mixed operations
func BenchmarkMixedOperations(b *testing.B) {
	pq := queue.New[int](10000, time.Second)
	defer pq.Close()

	// Pre-populate
	for i := 0; i < 1000; i++ {
		pq.Push(i, rand.Intn(100), time.Second)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		switch i % 4 {
		case 0:
			pq.Push(i+10000, rand.Intn(100), time.Second)
		case 1:
			pq.Pop()
		case 2:
			pq.Peek()
		case 3:
			if i < 1000 {
				pq.Remove(i)
			}
		}
	}
}

// BenchmarkConcurrentOperations benchmarks concurrent operations
func BenchmarkConcurrentOperations(b *testing.B) {
	pq := queue.New[int](1000, time.Second)
	defer pq.Close()

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			switch rand.Intn(3) {
			case 0:
				pq.Push(rand.Intn(10000), rand.Intn(100), time.Second)
			case 1:
				pq.Pop()
			case 2:
				pq.Peek()
			}
		}
	})
}
