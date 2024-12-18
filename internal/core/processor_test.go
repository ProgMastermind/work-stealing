package core

import (
	"sync"
	"testing"
	"time"
)

type testSetup struct {
    globalQueue *GlobalQueue
    processors  []*Processor
}

func newTestSetup(t *testing.T, numProcessors int) *testSetup {
    globalQueue := NewGlobalQueue(1000)
    if globalQueue == nil {
        t.Fatal("Failed to create global queue")
    }

    processors := make([]*Processor, numProcessors)
    for i := 0; i < numProcessors; i++ {
        processors[i] = NewProcessor(uint32(i), 100, globalQueue)
        if processors[i] == nil {
            t.Fatalf("Failed to create processor %d", i)
        }
    }

    // Set processor list for work stealing
    for _, p := range processors {
        p.SetProcessors(processors)
    }

    return &testSetup{
        globalQueue: globalQueue,
        processors:  processors,
    }
}

func TestProcessorCreation(t *testing.T) {
    tests := []struct {
        name      string
        id        uint32
        queueSize int
        wantSize  int
    }{
        {
            name:      "Normal creation",
            id:        1,
            queueSize: 100,
            wantSize:  100,
        },
        {
            name:      "Zero queue size defaults to 256",
            id:        2,
            queueSize: 0,
            wantSize:  256,
        },
        {
            name:      "Negative queue size defaults to 256",
            id:        3,
            queueSize: -1,
            wantSize:  256,
        },
    }

    globalQueue := NewGlobalQueue(1000)
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            p := NewProcessor(tt.id, tt.queueSize, globalQueue)

            if p.ID() != tt.id {
                t.Errorf("ID = %v, want %v", p.ID(), tt.id)
            }
            if p.maxQueueSize != tt.wantSize {
                t.Errorf("maxQueueSize = %v, want %v", p.maxQueueSize, tt.wantSize)
            }
            if p.State() != ProcessorIdle {
                t.Errorf("Initial state = %v, want Idle", p.State())
            }
            if p.globalQueue != globalQueue {
                t.Error("Global queue not properly set")
            }
        })
    }
}

func TestLocalQueueOperations(t *testing.T) {
    setup := newTestSetup(t, 1)
    p := setup.processors[0]

    t.Run("Push Operations", func(t *testing.T) {
        // Test successful push
        g := NewGoroutine(10*time.Millisecond, false)
        if !p.Push(g) {
            t.Error("First push failed")
        }

        // Fill queue
        for i := 0; i < p.maxQueueSize-1; i++ {
            if !p.Push(NewGoroutine(10*time.Millisecond, false)) {
                t.Errorf("Push %d failed unexpectedly", i)
            }
        }

        // Test queue full
        if p.Push(NewGoroutine(10*time.Millisecond, false)) {
            t.Error("Push succeeded when queue should be full")
        }

        // Test nil push
        if p.Push(nil) {
            t.Error("Nil push should fail")
        }
    })

    t.Run("Pop Operations", func(t *testing.T) {
        if g := p.Pop(); g == nil {
            t.Error("Pop should return goroutine")
        }

        // Pop until empty
        for p.Pop() != nil {}

        // Test empty queue
        if g := p.Pop(); g != nil {
            t.Error("Pop from empty queue should return nil")
        }
    })
}

func TestWorkStealing(t *testing.T) {
    setup := newTestSetup(t, 3)
    victim := setup.processors[0]
    thief := setup.processors[1]

    // Add tasks to victim
    numTasks := 6
    for i := 0; i < numTasks; i++ {
        victim.Push(NewGoroutine(10*time.Millisecond, false))
    }

    // Test local queue stealing
    t.Run("Local Queue Stealing", func(t *testing.T) {
        stolen := thief.tryStealFromProcessors()
        if len(stolen) == 0 {
            t.Error("Failed to steal from local queue")
        }

        stats := thief.GetStats()
        if stats.LocalSteals == 0 {
            t.Error("Local steal not recorded in metrics")
        }
    })

    // Test global queue stealing
    t.Run("Global Queue Stealing", func(t *testing.T) {
        // Add tasks to global queue
        setup.globalQueue.Submit(NewGoroutine(10*time.Millisecond, false))
        setup.globalQueue.Submit(NewGoroutine(10*time.Millisecond, false))

        stolen := thief.tryStealFromGlobalQueue()
        if len(stolen) == 0 {
            t.Error("Failed to steal from global queue")
        }

        stats := thief.GetStats()
        if stats.GlobalSteals == 0 {
            t.Error("Global steal not recorded in metrics")
        }
    })
}

func TestFindWork(t *testing.T) {
    setup := newTestSetup(t, 2)
    p := setup.processors[0]

    t.Run("Local Queue Priority", func(t *testing.T) {
        // Add task to local queue
        g := NewGoroutine(10*time.Millisecond, false)
        p.Push(g)

        found := p.FindWork()
        if found != g {
            t.Error("Should find work from local queue first")
        }
    })

    t.Run("Work Stealing Order", func(t *testing.T) {
        // Add tasks to global queue
        setup.globalQueue.Submit(NewGoroutine(10*time.Millisecond, false))

        found := p.FindWork()
        if found == nil {
            t.Error("Should find work from global queue")
        }
    })
}

func TestProcessorExecution(t *testing.T) {
    setup := newTestSetup(t, 1)
    p := setup.processors[0]

    g := NewGoroutine(50*time.Millisecond, false)

    start := time.Now()
    p.Execute(g)
    duration := time.Since(start)

    if duration < g.Workload() {
        t.Errorf("Execution time %v shorter than workload %v", duration, g.Workload())
    }

    stats := p.GetStats()
    if stats.TasksExecuted != 1 {
        t.Error("Task execution not recorded in metrics")
    }
    if p.State() != ProcessorIdle {
        t.Error("Processor should return to idle state")
    }
    if p.CurrentGoroutine() != nil {
        t.Error("Current goroutine should be nil after execution")
    }
}

func TestConcurrentOperations(t *testing.T) {
    setup := newTestSetup(t, 4)

    const numGoroutines = 100
    const opsPerGoroutine = 10

    var wg sync.WaitGroup
    wg.Add(numGoroutines)

    // Start concurrent operations
    for i := 0; i < numGoroutines; i++ {
        go func(id int) {
            defer wg.Done()
            p := setup.processors[id%len(setup.processors)]

            for j := 0; j < opsPerGoroutine; j++ {
                switch j % 3 {
                case 0:
                    p.Push(NewGoroutine(time.Millisecond, false))
                case 1:
                    if g := p.FindWork(); g != nil {
                        p.Execute(g)
                    }
                case 2:
                    p.tryStealFromGlobalQueue()
                }
            }
        }(i)
    }

    // Add some work to global queue
    for i := 0; i < numGoroutines/2; i++ {
        setup.globalQueue.Submit(NewGoroutine(time.Millisecond, false))
    }

    // Wait with timeout
    done := make(chan bool)
    go func() {
        wg.Wait()
        done <- true
    }()

    select {
    case <-done:
        // Success
    case <-time.After(5 * time.Second):
        t.Fatal("Concurrent operations test timed out")
    }

    // Verify operations completed
    var totalExecuted uint64
    for _, p := range setup.processors {
        stats := p.GetStats()
        totalExecuted += stats.TasksExecuted
    }

    if totalExecuted == 0 {
        t.Error("No tasks were executed")
    }
}

func TestProcessorMetrics(t *testing.T) {
    setup := newTestSetup(t, 1)
    p := setup.processors[0]

    // Execute some tasks
    for i := 0; i < 3; i++ {
        g := NewGoroutine(10*time.Millisecond, false)
        p.Push(g)
        if g := p.Pop(); g != nil {
            p.Execute(g)
        }
    }

    stats := p.GetStats()
    if stats.TasksExecuted != 3 {
        t.Errorf("Expected 3 tasks executed, got %d", stats.TasksExecuted)
    }
    if stats.RunningTime == 0 {
        t.Error("Running time not recorded")
    }
    if stats.IdleTime == 0 {
        t.Error("Idle time not recorded")
    }
}

func TestProcessorStateTransitions(t *testing.T) {
    setup := newTestSetup(t, 1)
    p := setup.processors[0]

    states := []struct {
        state    ProcessorState
        expected string
    }{
        {ProcessorRunning, "running"},
        {ProcessorStealing, "stealing"},
        {ProcessorIdle, "idle"},
    }

    for _, s := range states {
        t.Run(s.expected, func(t *testing.T) {
            p.SetState(s.state)

            if p.State() != s.state {
                t.Errorf("State = %v, want %v", p.State(), s.state)
            }
            if s.state.String() != s.expected {
                t.Errorf("State string = %v, want %v", s.state.String(), s.expected)
            }
        })
    }
}
