package main

import (
	"flag"
	"fmt"
	"log"
	"math/rand"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"
	"workstealing/internal/core"
	"workstealing/internal/scheduler"
	"workstealing/internal/visualization"
)

// TaskType represents different kinds of workloads
type TaskType int

const (
    CPUBound TaskType = iota
    IOBound
    MixedBound
    ShortLived
    LongRunning
)

// TaskConfig holds configuration for different task types
type TaskConfig struct {
    taskType    TaskType
    workload    time.Duration
    isBlocking  bool
    description string
}

var taskConfigs = map[TaskType]TaskConfig{
    CPUBound: {
        taskType:    CPUBound,
        workload:    100 * time.Millisecond,
        isBlocking:  false,
        description: "CPU-intensive computation",
    },
    IOBound: {
        taskType:    IOBound,
        workload:    50 * time.Millisecond,
        isBlocking:  true,
        description: "I/O operation (network/disk)",
    },
    MixedBound: {
        taskType:    MixedBound,
        workload:    75 * time.Millisecond,
        isBlocking:  true,
        description: "Mixed CPU and I/O operations",
    },
    ShortLived: {
        taskType:    ShortLived,
        workload:    10 * time.Millisecond,
        isBlocking:  false,
        description: "Quick computational task",
    },
    LongRunning: {
        taskType:    LongRunning,
        workload:    200 * time.Millisecond,
        isBlocking:  false,
        description: "Long-running computation",
    },
}

// WorkloadGenerator generates different types of tasks
type WorkloadGenerator struct {
    scheduler *scheduler.Scheduler
    stop      chan struct{}
    wg        sync.WaitGroup
    rand      *rand.Rand
}

func NewWorkloadGenerator(s *scheduler.Scheduler) *WorkloadGenerator {
    return &WorkloadGenerator{
        scheduler: s,
        stop:      make(chan struct{}),
        rand:      rand.New(rand.NewSource(time.Now().UnixNano())),
    }
}

func (wg *WorkloadGenerator) Start() {
    wg.wg.Add(1)
    go wg.generateTasks()
}

func (wg *WorkloadGenerator) Stop() {
    close(wg.stop)
    wg.wg.Wait()
}

func (wg *WorkloadGenerator) generateTasks() {
    defer wg.wg.Done()

    ticker := time.NewTicker(50 * time.Millisecond)
    defer ticker.Stop()

    taskTypes := []TaskType{CPUBound, IOBound, MixedBound, ShortLived, LongRunning}
    batchSizes := []int{1, 5, 10} // Different batch sizes for varying load

    for {
        select {
        case <-wg.stop:
            return
        case <-ticker.C:
            // Randomly select task type and batch size
            taskType := taskTypes[wg.rand.Intn(len(taskTypes))]
            batchSize := batchSizes[wg.rand.Intn(len(batchSizes))]

            config := taskConfigs[taskType]
            for i := 0; i < batchSize; i++ {
                // Add some randomness to workload
                workload := config.workload +
                    time.Duration(wg.rand.Int63n(int64(config.workload/2)))

                g := core.NewGoroutine(workload, config.isBlocking)
                wg.scheduler.Submit(g)
            }
        }
    }
}

func main() {
    // Parse command-line flags
    numProcessors := flag.Int("p", 4, "Number of processors")
    queueSize := flag.Int("q", 1000, "Global queue size")
    updateInterval := flag.Duration("i", 100*time.Millisecond, "UI update interval")
    flag.Parse()

    // Initialize scheduler
    s := scheduler.NewScheduler(int32(*numProcessors), int32(*queueSize))
    if err := s.Start(); err != nil {
        log.Fatalf("Failed to start scheduler: %v", err)
    }

    // Initialize visualizer
    v, err := visualization.NewTerminalVisualizer(s, *updateInterval)
    if err != nil {
        log.Fatalf("Failed to create visualizer: %v", err)
    }

    // Initialize workload generator
    gen := NewWorkloadGenerator(s)
    gen.Start()

    // Setup signal handling for graceful shutdown
    sigChan := make(chan os.Signal, 1)
    signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

    // Print usage instructions
    fmt.Println("Work-Stealing Scheduler Demo")
    fmt.Println("----------------------------")
    fmt.Println("Controls:")
    fmt.Println("- Press 'q' to quit")
    fmt.Println("- Press 'p' to pause/resume workload generation")
    fmt.Println("- Press 's' to show current statistics")
    fmt.Println("----------------------------")

    // Start visualization
    go func() {
        <-sigChan
        gen.Stop()
        s.Stop()
        v.Stop()
    }()

    // Start visualization (this blocks until 'q' is pressed)
    v.Start()

    // Cleanup
    gen.Stop()
    s.Stop()
}

// simulateCPUWork simulates CPU-bound work
func simulateCPUWork(duration time.Duration) {
    start := time.Now()
    for time.Since(start) < duration {
        // Perform some meaningless computation
        for i := 0; i < 1000; i++ {
            _ = i * i
        }
    }
}

// simulateIOWork simulates I/O-bound work
func simulateIOWork(duration time.Duration) {
    time.Sleep(duration)
}
