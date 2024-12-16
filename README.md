```markdown
# Go Work-Stealing Scheduler Implementation

A high-performance work-stealing scheduler implementation in Go, featuring task distribution visualization, comprehensive metrics, and support for different workload patterns.

## Table of Contents
- [Overview](#overview)
- [Architecture](#architecture)
- [Features](#features)
- [Installation](#installation)
- [Usage](#usage)
- [Components](#components)
- [Visualization](#visualization)
- [Performance Metrics](#performance-metrics)
- [Examples](#examples)

## Overview

This project implements a work-stealing scheduler that efficiently distributes tasks across multiple processors using the work-stealing algorithm. It includes a real-time visualization system and supports various task types including CPU-bound, I/O-bound, and mixed workloads.

### Key Features
- Work-stealing algorithm implementation
- Real-time task distribution visualization
- Support for different task types
- Comprehensive performance metrics
- Network poller integration
- Interactive demo application

## Architecture

The scheduler is built with a modular architecture consisting of several key components:

```
work_stealing/
├── cmd/
│   └── main.go           # Demo application
├── internal/
│   ├── core/             # Core components
│   │   ├── goroutine.go  # Task representation
│   │   └── processor.go  # Processor implementation
│   ├── queue/            # Queue implementations
│   ├── scheduler/        # Scheduler logic
│   ├── poller/          # Network poller
│   └── visualization/    # UI components
└── README.md
```

## Features

### Work-Stealing Algorithm
- Efficient task distribution across processors
- Adaptive stealing strategies
- Load balancing mechanisms
- Source tracking for tasks

### Task Support
- CPU-bound tasks
- I/O-bound tasks
- Mixed workloads
- Short-lived tasks
- Long-running tasks

### Performance Monitoring
- Real-time metrics
- Task completion statistics
- Steal rate tracking
- Queue utilization monitoring

## Installation

1. Clone the repository:
```bash
git clone https://github.com/yourusername/work-stealing.git
```

2. Install dependencies:
```bash
go mod download
```

3. Build the project:
```bash
go build ./...
```

## Usage

### Running the Demo

```bash
go run cmd/main.go -p 4 -q 1000 -i 100ms
```

Parameters:
- `-p`: Number of processors (default: 4)
- `-q`: Global queue size (default: 1000)
- `-i`: UI update interval (default: 100ms)

### Interactive Controls
- `q`: Quit the application
- `p`: Pause/resume workload generation
- `s`: Show current statistics

## Components

### Core Package
The core package (`internal/core`) provides fundamental components:

#### Goroutine
```go
type Goroutine struct {
    // Task representation
}
```
Represents individual tasks with properties:
- Execution duration
- Blocking/non-blocking behavior
- Task source tracking
- State management

#### Processor
```go
type Processor struct {
    // Processor implementation
}
```
Handles:
- Local task queue management
- Work stealing implementation
- Task execution
- Performance metrics

### Scheduler
The scheduler (`internal/scheduler`) manages:
- Task distribution
- Work-stealing coordination
- Load balancing
- Global queue management

### Network Poller
The poller (`internal/poller`) handles:
- I/O task management
- Event monitoring
- Task unblocking
- Processor notification

### Visualization
The visualization system (`internal/visualization`) provides:
- Real-time task distribution display
- Processor utilization metrics
- Work-stealing statistics
- Queue status monitoring

### Processor Metrics
- Tasks executed
- Steal attempts/successes
- Queue utilization
- Idle/busy time

### Scheduler Metrics
- Global tasks scheduled
- Task completion rate
- Steal distribution
- Queue statistics

### Visualization Metrics
- Real-time processor state
- Task distribution graphs
- Steal activity monitoring
- Queue utilization display

## Examples

### Basic Usage
```go
// Initialize scheduler
s := scheduler.NewScheduler(4, 1000)
s.Start()

// Submit tasks
g := core.NewGoroutine(100*time.Millisecond, false)
s.Submit(g)

// Start visualization
v := visualization.NewTerminalVisualizer(s, 100*time.Millisecond)
v.Start()
```

### Custom Task Types
```go
// CPU-bound task
g := core.NewGoroutine(100*time.Millisecond, false)

// I/O-bound task
g := core.NewGoroutine(50*time.Millisecond, true)
```

### Optimal Configuration
- Processor count: Match CPU core count
- Queue size: Balance memory usage vs contention
- Steal threshold: Adjust based on workload

### Best Practices
- Use appropriate task granularity
- Balance processor count
- Monitor steal rates
- Adjust queue sizes based on load

## Acknowledgments

- Inspired by Go runtime scheduler
- Built with termui for visualization
```
