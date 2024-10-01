# queue

![GitHub Actions Workflow Status](https://img.shields.io/github/actions/workflow/status/rleungx/queue/go.yml)
![Codecov](https://img.shields.io/codecov/c/github/rleungx/queue)
![GitHub License](https://img.shields.io/github/license/rleungx/queue)
[![Go Reference](https://pkg.go.dev/badge/github.com/rleungx/queue.svg)](https://pkg.go.dev/github.com/rleungx/queue)

This project is a Go implementation of a priority queue with expiration functionality.

## Features

- Priority queue with custom types
- Automatic cleanup of expired entries
- Thread-safe operations

## Usage

```go
package main

import (
    "fmt"
    "time"

    "github.com/rleungx/queue"
)

func main() {
    // Create a new priority queue with a capacity of 10 and a cleanup interval of 1 second
    pq := queue.New[string](10, time.Second)
    defer pq.Close()

    // Add items to the priority queue
    pq.Push("v1", 1, 2*time.Second)
    pq.Push("v2", 2, time.Second)
    pq.Push("v3", 3, 3*time.Second)

    // Show all items
    fmt.Printf("All items: %v\n", pq.Elems())

    // Peek the highest priority item
    fmt.Printf("Highest priority item: %v\n", pq.Peek())

    // Remove the highest priority item
    fmt.Printf("Removed highest priority item: %v\n", pq.Pop())

    // Show remaining items
    fmt.Printf("Remaining items: %v\n", pq.Elems())

    // Remove a specific item
    pq.Remove("v1")
    fmt.Println("Attempted to remove item: v1")

    // Show remaining items after removal
    fmt.Printf("Remaining items after removal: %v\n", pq.Elems())

    // Get queue size
    fmt.Printf("Queue size: %d\n", pq.Size())

    // Check if queue is empty
    fmt.Printf("Is queue empty: %v\n", pq.Empty())

    // Get queue capacity
    fmt.Printf("Queue capacity: %d\n", pq.Capacity())

    // Add new items
    pq.Push("v4", 4, 500*time.Millisecond)
    pq.Push("v5", 5, 5*time.Second)

    // Get all elements as a slice
    elems := pq.Elems()
    fmt.Printf("All elements: %v\n", elems)

    // Wait for 600 milliseconds to let v4 expire
    time.Sleep(600 * time.Millisecond)
    
    // Manually call Cleanup
    pq.Cleanup()
    fmt.Println("Manually called Cleanup")

    // Show remaining items after manual cleanup
    fmt.Printf("Remaining items after manual cleanup: %v\n", pq.Elems())

    // Wait for 5 seconds to allow automatic cleanup
    time.Sleep(5 * time.Second)
    
    // Show remaining items after automatic cleanup
    fmt.Printf("Remaining items after automatic cleanup: %v\n", pq.Elems())
}
```

## Contributing
Contributions are welcome! Please open an issue or submit a pull request.

## License
This project is licensed under the Apache License 2.0. See the [LICENSE](./LICENSE) file for details.
