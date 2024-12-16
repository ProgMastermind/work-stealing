package core

import (
	"sync"
	"testing"
	"time"
)

func TestNewGoroutine(t *testing.T) {
    tests := []struct {
        name     string
        workload time.Duration
        blocking bool
    }{
        {
            name:     "Non-blocking goroutine",
            workload: 100 * time.Millisecond,
            blocking: false,
        },
        {
            name:     "Blocking goroutine",
            workload: 200 * time.Millisecond,
            blocking: true,
        },
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            g := NewGoroutine(tt.workload, tt.blocking)

            if g.ID() == 0 {
                t.Error("Expected non-zero ID")
            }

            if g.State() != GoroutineCreated {
                t.Errorf("Expected state Created, got %v", g.State())
            }

            if g.IsBlocking() != tt.blocking {
                t.Errorf("Expected blocking %v, got %v", tt.blocking, g.IsBlocking())
            }

            if g.Workload() != tt.workload {
                t.Errorf("Expected workload %v, got %v", tt.workload, g.Workload())
            }

            if g.Source() != SourceLocalQueue {
                t.Errorf("Expected default source SourceLocalQueue, got %v", g.Source())
            }
        })
    }
}

func TestGoroutineSource(t *testing.T) {
    tests := []struct {
        name       string
        source     TaskSource
        stolenFrom uint32
    }{
        {
            name:       "Local Queue Source",
            source:     SourceLocalQueue,
            stolenFrom: 0,
        },
        {
            name:       "Global Queue Source",
            source:     SourceGlobalQueue,
            stolenFrom: 0,
        },
        {
            name:       "Stolen Source",
            source:     SourceStolen,
            stolenFrom: 1,
        },
        {
            name:       "Network Poller Source",
            source:     SourceNetworkPoller,
            stolenFrom: 0,
        },
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            g := NewGoroutine(100*time.Millisecond, false)
            g.SetSource(tt.source, tt.stolenFrom)

            if g.Source() != tt.source {
                t.Errorf("Expected source %v, got %v", tt.source, g.Source())
            }

            if g.stolenFrom != tt.stolenFrom {
                t.Errorf("Expected stolenFrom %v, got %v", tt.stolenFrom, g.stolenFrom)
            }
        })
    }
}

func TestGoroutineStateTransitions(t *testing.T) {
    g := NewGoroutine(100*time.Millisecond, false)

    states := []struct {
        state    GoroutineState
        expected string
    }{
        {GoroutineRunnable, "Runnable"},
        {GoroutineRunning, "Running"},
        {GoroutineBlocked, "Blocked"},
        {GoroutineRunnable, "Runnable"},
        {GoroutineRunning, "Running"},
        {GoroutineFinished, "Finished"},
    }

    for _, s := range states {
        g.SetState(s.state)
        if g.State() != s.state {
            t.Errorf("Expected state %v, got %v", s.expected, g.State())
        }
    }
}

func TestGoroutineExecution(t *testing.T) {
    g := NewGoroutine(100*time.Millisecond, false)

    // Test start time recording
    g.Start()
    startTime := g.startTime
    if startTime.IsZero() {
        t.Error("Start time should be set")
    }

    // Small sleep to simulate work
    time.Sleep(50 * time.Millisecond)

    // Test execution time while running
    executionTime := g.ExecutionTime()
    if executionTime < 50*time.Millisecond {
        t.Errorf("Expected execution time >= 50ms, got %v", executionTime)
    }

    // Test finish
    result := "test result"
    g.Finish(result, nil)

    if g.State() != GoroutineFinished {
        t.Error("Goroutine should be in finished state")
    }

    if g.result != result {
        t.Error("Result not properly stored")
    }

    finalTime := g.ExecutionTime()
    if finalTime < executionTime {
        t.Errorf("Final time %v should be >= execution time %v", finalTime, executionTime)
    }
}

func TestGoroutineConcurrency(t *testing.T) {
    const numGoroutines = 1000
    var wg sync.WaitGroup
    goroutines := make([]*Goroutine, numGoroutines)

    // Test concurrent creation
    for i := 0; i < numGoroutines; i++ {
        wg.Add(1)
        go func(i int) {
            defer wg.Done()
            goroutines[i] = NewGoroutine(time.Millisecond, false)
        }(i)
    }

    wg.Wait()

    // Verify unique IDs
    idMap := make(map[uint64]bool)
    for _, g := range goroutines {
        if g == nil {
            t.Fatal("Goroutine creation failed")
        }
        if idMap[g.ID()] {
            t.Error("Duplicate goroutine ID found")
        }
        idMap[g.ID()] = true
    }
}

func TestGoroutineBlocking(t *testing.T) {
    g := NewGoroutine(100*time.Millisecond, true)
    if !g.IsBlocking() {
        t.Error("Expected goroutine to be blocking")
    }
}

func BenchmarkGoroutineCreation(b *testing.B) {
    b.ResetTimer()
    for i := 0; i < b.N; i++ {
        _ = NewGoroutine(time.Millisecond, false)
    }
}

func BenchmarkGoroutineStateTransitions(b *testing.B) {
    g := NewGoroutine(time.Millisecond, false)
    b.ResetTimer()

    for i := 0; i < b.N; i++ {
        g.SetState(GoroutineState(i % 5))
    }
}

// Helper function to run all tests with race detector
func TestWithRaceDetector(t *testing.T) {
    const numConcurrent = 100
    var wg sync.WaitGroup

    for i := 0; i < numConcurrent; i++ {
        wg.Add(1)
        go func() {
            defer wg.Done()
            g := NewGoroutine(time.Millisecond, false)
            g.Start()
            g.SetState(GoroutineRunning)
            g.ExecutionTime()
            g.Finish(nil, nil)
        }()
    }

    wg.Wait()
}
