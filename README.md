# Go Work-Stealing Scheduler

## Overview

This is an advanced, high-performance concurrent task scheduling system implemented in Go, featuring sophisticated work-stealing mechanisms, comprehensive task management, and detailed runtime monitoring.

## Project Architecture

```
workstealing/
├── internal/
│   ├── core/
│   │   ├── goroutine.go      # Task lifecycle and management
│   │   ├── processor.go      # Processor implementation
│   │   └── global_queue.go   # Centralized task queue
│   ├── scheduler/
│   │   └── scheduler.go      # Task distribution coordinator
│   └── poller/
│       └── network_poller.go # Blocking I/O event management
```

## Key Components

### 1. Goroutine (`core.Goroutine`)
- Lightweight task representation
- Unique ID generation
- Advanced state tracking (Created, Runnable, Running, Blocked, Finished)
- Transition history logging
- Support for blocking/non-blocking tasks

### 2. Processor (`core.Processor`)
- Local task queue management
- Work-stealing from other processors
- Performance metrics tracking
- Dynamic task allocation strategies

### 3. Global Queue (`core.GlobalQueue`)
- Centralized, thread-safe task repository
- Task submission and stealing mechanisms
- Capacity-based task management
- Atomic metric tracking

### 4. Network Poller (`poller.NetworkPoller`)
- Event-driven blocking I/O management
- Timeout handling
- Dynamic task rescheduling
- Non-blocking task processing

### 5. Scheduler (`scheduler.Scheduler`)
- Multi-processor task orchestration
- Global/local queue coordination
- Advanced work-stealing implementation
- Real-time statistics collection

## Features

- 🚀 Efficient work-stealing algorithm
- 📊 Comprehensive performance metrics
- 🔄 Dynamic task distribution
- 🌐 Blocking and non-blocking task support
- 📈 Real-time monitoring

## Performance Characteristics

- Minimal lock contention
- Atomic operation-based synchronization
- Randomized work-stealing victim selection
- Low-overhead task tracking
- Adaptive load balancing

## Usage Example

```go
// Create a scheduler with 4 processors and global queue size of 1000
scheduler := NewScheduler(4, 1000)
scheduler.Start()

// Create a non-blocking task with 100ms workload
task := NewGoroutine(100 * time.Millisecond, false)
scheduler.Submit(task)

// Retrieve runtime statistics
stats := scheduler.GetStats()
```

## Configuration Options

- Processor count customization
- Global queue size configuration
- Configurable work-stealing strategies
- Flexible timeout settings

## Metrics Tracking

The system provides detailed metrics across multiple dimensions:
- Tasks scheduled and completed
- Work-stealing statistics
- Processor utilization
- Network poller performance
- Queue state tracking

## Performance Tuning Recommendations

1. Match processor count to available CPU cores
2. Configure queue sizes based on expected workload
3. Monitor steal rates and queue utilization
4. Adjust timeout configurations for I/O-bound tasks

## Dependencies

- Go 1.20+
- Standard library concurrency primitives
