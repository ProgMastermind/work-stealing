package scheduler

import (
	"sync"
	"testing"
	"time"
	"workstealing/internal/core"
)

type testSetup struct {
    scheduler *Scheduler
    cleanup   func()
}

func newTestSetup(t *testing.T, numProcessors int) *testSetup {
    s := NewScheduler(numProcessors, 1000)
    if s == nil {
        t.Fatal("Failed to create scheduler")
    }

    return &testSetup{
        scheduler: s,
        cleanup: func() {
            if s.State() == SchedulerRunning {
                s.Stop()
            }
        },
    }
}

func TestSchedulerCreation(t *testing.T) {
    tests := []struct {
        name          string
        numProcessors int
        queueSize     int32
        wantNil      bool
    }{
        {
            name:          "Valid creation",
            numProcessors: 4,
            queueSize:     1000,
            wantNil:       false,
        },
        {
            name:          "Zero processors defaults to 1",
            numProcessors: 0,
            queueSize:     1000,
            wantNil:       false,
        },
        {
            name:          "Negative processors defaults to 1",
            numProcessors: -1,
            queueSize:     1000,
            wantNil:       false,
        },
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            s := NewScheduler(tt.numProcessors, tt.queueSize)
            if (s == nil) != tt.wantNil {
                t.Errorf("NewScheduler() nil = %v, want %v", s == nil, tt.wantNil)
            }

            if s != nil {
                expectedProcessors := tt.numProcessors
                if expectedProcessors <= 0 {
                    expectedProcessors = 1
                }

                if len(s.processors) != expectedProcessors {
                    t.Errorf("Expected %d processors, got %d", expectedProcessors, len(s.processors))
                }

                if s.globalQueue == nil {
                    t.Error("Global queue not initialized")
                }

                if s.networkPoller == nil {
                    t.Error("Network poller not initialized")
                }

                if s.State() != SchedulerStopped {
                    t.Errorf("Initial state = %v, want %v", s.State(), SchedulerStopped)
                }
            }
        })
    }
}

func TestSchedulerStartStop(t *testing.T) {
    setup := newTestSetup(t, 2)
    defer setup.cleanup()

    // Test Start
    if err := setup.scheduler.Start(); err != nil {
        t.Errorf("Start() error = %v", err)
    }
    if setup.scheduler.State() != SchedulerRunning {
        t.Errorf("After Start(): state = %v, want %v", setup.scheduler.State(), SchedulerRunning)
    }

    // Test double Start
    if err := setup.scheduler.Start(); err != nil {
        t.Error("Second Start() should not return error")
    }

    // Test Stop
    setup.scheduler.Stop()
    if setup.scheduler.State() != SchedulerStopped {
        t.Errorf("After Stop(): state = %v, want %v", setup.scheduler.State(), SchedulerStopped)
    }

    // Test double Stop
    setup.scheduler.Stop() // Should not panic
}

func TestSchedulerTaskSubmission(t *testing.T) {
    setup := newTestSetup(t, 2)
    defer setup.cleanup()

    if err := setup.scheduler.Start(); err != nil {
        t.Fatal(err)
    }

    t.Run("Non-blocking Task", func(t *testing.T) {
        g := core.NewGoroutine(10*time.Millisecond, false)
        if !setup.scheduler.Submit(g) {
            t.Error("Submit should succeed")
        }

        time.Sleep(20 * time.Millisecond) // Allow execution
        stats := setup.scheduler.GetStats()
        if stats.TasksScheduled == 0 {
            t.Error("TasksScheduled not incremented")
        }
        if stats.TasksCompleted == 0 {
            t.Error("Task not completed")
        }
    })

    t.Run("Blocking Task", func(t *testing.T) {
        g := core.NewGoroutine(20*time.Millisecond, true)
        if !setup.scheduler.Submit(g) {
            t.Error("Submit should succeed")
        }

        time.Sleep(10 * time.Millisecond) // Allow registration
        stats := setup.scheduler.GetStats()
        if stats.PollerMetrics.TotalEvents == 0 {
            t.Error("Blocking task not registered with poller")
        }
    })

    t.Run("Nil Task", func(t *testing.T) {
        if setup.scheduler.Submit(nil) {
            t.Error("Nil submission should fail")
        }
    })
}

func TestSchedulerMetrics(t *testing.T) {
    setup := newTestSetup(t, 2)
    defer setup.cleanup()

    if err := setup.scheduler.Start(); err != nil {
        t.Fatal(err)
    }

    // Submit tasks with controlled timing
    numBlocking := 5
    numNonBlocking := 5

    // Submit blocking tasks first
    for i := 0; i < numBlocking; i++ {
        g := core.NewGoroutine(20*time.Millisecond, true)
        if !setup.scheduler.Submit(g) {
            t.Errorf("Failed to submit blocking task %d", i)
        }
        time.Sleep(5 * time.Millisecond) // Ensure proper registration
    }

    // Wait for blocking tasks to be registered
    time.Sleep(30 * time.Millisecond)

    // Check blocking task registration
    stats := setup.scheduler.GetStats()
    if stats.PollerMetrics.TotalEvents != uint64(numBlocking) {
        t.Errorf("Expected %d blocking events, got %d", numBlocking, stats.PollerMetrics.TotalEvents)
    }

    // Submit non-blocking tasks
    for i := 0; i < numNonBlocking; i++ {
        g := core.NewGoroutine(10*time.Millisecond, false)
        if !setup.scheduler.Submit(g) {
            t.Errorf("Failed to submit non-blocking task %d", i)
        }
    }

    // Wait for task completion
    time.Sleep(50 * time.Millisecond)

    // Verify final metrics
    finalStats := setup.scheduler.GetStats()
    totalTasks := uint64(numBlocking + numNonBlocking)

    if finalStats.TasksScheduled != totalTasks {
        t.Errorf("Expected %d tasks scheduled, got %d", totalTasks, finalStats.TasksScheduled)
    }

    // Non-blocking tasks should complete
    if finalStats.TasksCompleted < uint64(numNonBlocking) {
        t.Errorf("Expected at least %d completed tasks, got %d", numNonBlocking, finalStats.TasksCompleted)
    }
}

func TestConcurrentTaskSubmission(t *testing.T) {
    setup := newTestSetup(t, 4)
    defer setup.cleanup()

    if err := setup.scheduler.Start(); err != nil {
        t.Fatal(err)
    }

    const numGoroutines = 10
    const tasksPerGoroutine = 5

    var wg sync.WaitGroup
    wg.Add(numGoroutines)

    for i := 0; i < numGoroutines; i++ {
        go func(id int) {
            defer wg.Done()
            for j := 0; j < tasksPerGoroutine; j++ {
                // Alternate between blocking and non-blocking tasks
                g := core.NewGoroutine(10*time.Millisecond, j%2 == 0)
                setup.scheduler.Submit(g)
                time.Sleep(time.Millisecond) // Prevent flooding
            }
        }(i)
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
        t.Fatal("Concurrent submission test timed out")
    }

    // Allow time for task processing
    time.Sleep(100 * time.Millisecond)

    stats := setup.scheduler.GetStats()
    expectedTasks := uint64(numGoroutines * tasksPerGoroutine)
    if stats.TasksScheduled != expectedTasks {
        t.Errorf("Expected %d tasks scheduled, got %d", expectedTasks, stats.TasksScheduled)
    }
}

func TestSchedulerStateString(t *testing.T) {
    tests := []struct {
        state SchedulerState
        want  string
    }{
        {SchedulerStopped, "stopped"},
        {SchedulerRunning, "running"},
        {SchedulerStopping, "stopping"},
        {SchedulerState(99), "unknown"},
    }

    for _, tt := range tests {
        t.Run(tt.want, func(t *testing.T) {
            if got := tt.state.String(); got != tt.want {
                t.Errorf("SchedulerState(%d).String() = %v, want %v", tt.state, got, tt.want)
            }
        })
    }
}
