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
    // Create a new priority queue with a capacity of 10 and a cleanup interval of 1 minute
    pq := queue.NewPriorityQueue[string](10, time.Second)

    // Add items to the priority queue
    pq.Push("v1", 1, time.Second * 2)
    pq.Push("v2", 2, time.Second)
    pq.Push("v3", 3, time.Second * 3)

    // Show all items
    elems := pq.Elems()
    fmt.Printf("All items: %v\n", elems)

    // Peek the highest priority item
    peeked := pq.Peek()
    fmt.Printf("Peeked item: %v\n", peeked)

    // Remove the highest priority item
    popped := pq.Pop()
    fmt.Printf("Popped item: %v\n", popped)

    // Show remaining items
    elems = pq.Elems()
    fmt.Printf("Remaining items: %v\n", elems)

    // Remove a specific item
    pq.Remove("v1")
    fmt.Printf("Removed item: v1\n")

    // Show remaining items after removal
    elems = pq.Elems()
    fmt.Printf("Remaining items after removal: %v\n", elems)

    time.Sleep(time.Second * 2)
    // Show remaining items after cleanup
    elems = pq.Elems()
    fmt.Printf("Remaining items after cleanup: %v\n", elems)
}
```

## Contributing
Contributions are welcome! Please open an issue or submit a pull request.

## License
This project is licensed under the Apache License 2.0. See the [LICENSE](./LICENSE) file for details.
