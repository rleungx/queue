# queue

![GitHub Actions Workflow Status](https://img.shields.io/github/actions/workflow/status/rleungx/queue/go.yml)
![Codecov](https://img.shields.io/codecov/c/github/rleungx/queue)
![GitHub License](https://img.shields.io/github/license/rleungx/queue)

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
    pq := queue.NewPriorityQueue[string](10, time.Minute)

    // Add items to the priority queue
    pq.Put("v1", 1, 2 * time.Minute)
    pq.Put("v2", 2, time.Minute)

    // Remove the highest priority item
    pq.Remove("v2")

    // Show items
    elems := pq.Elems()
    fmt.Printf("%v\n", elems)
}
```

## Contributing
Contributions are welcome! Please open an issue or submit a pull request.

## License
This project is licensed under the Apache License 2.0. See the LICENSE file for details. 