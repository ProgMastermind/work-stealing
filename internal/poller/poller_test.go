package poller

import (
	"sync"
	"testing"
	"time"
	"workstealing/internal/core"
)

type TestSetup struct {
    processors  []*core.Processor
    poller      *NetworkPoller
    globalQueue *core.GlobalQueue
}

func newTestSetup(t *testing.T) *TestSetup {
    globalQueue := core.NewGlobalQueue(1000)
    if globalQueue == nil {
        t.Fatal("Failed to create global queue")
    }

    processors := []*core.Processor{
        core.NewProcessor(1, 100, globalQueue),
        core.NewProcessor(2, 100, globalQueue),
    }

    // Set processor list for work stealing
    for _, p := range processors {
        p.SetProcessors(processors)
    }

    poller := NewNetworkPoller(processors)
    if poller == nil {
        t.Fatal("Failed to create network poller")
    }

    return &TestSetup{
        processors:  processors,
        poller:     poller,
        globalQueue: globalQueue,
    }
}

func TestPollerInitialization(t *testing.T) {
    tests := []struct {
        name       string
        processors []*core.Processor
        wantNil    bool
    }{
        {
            name:       "Valid initialization",
            processors: []*core.Processor{core.NewProcessor(1, 100, core.NewGlobalQueue(1000))},
            wantNil:    false,
        },
        {
            name:       "Empty processors",
            processors: []*core.Processor{},
            wantNil:    true,
        },
        {
            name:       "Nil processors",
            processors: nil,
            wantNil:    true,
        },
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            poller := NewNetworkPoller(tt.processors)
            if (poller == nil) != tt.wantNil {
                t.Errorf("NewNetworkPoller() nil = %v, want %v", poller == nil, tt.wantNil)
            }

            if poller != nil {
                if poller.events == nil {
                    t.Error("Events map not initialized")
                }
                if poller.blockedGoroutines == nil {
                    t.Error("BlockedGoroutines map not initialized")
                }
            }
        })
    }
}

func TestPollerStartStop(t *testing.T) {
    setup := newTestSetup(t)

    // Test Start
    setup.poller.Start()
    if !setup.poller.running.Load() {
        t.Error("Poller should be running after Start")
    }

    // Test double Start
    setup.poller.Start() // Should not panic
    if !setup.poller.running.Load() {
        t.Error("Poller should remain running after second Start")
    }

    // Test Stop
    setup.poller.Stop()
    if setup.poller.running.Load() {
        t.Error("Poller should not be running after Stop")
    }
}

func TestPollerRegistration(t *testing.T) {
    setup := newTestSetup(t)
    setup.poller.Start()
    defer setup.poller.Stop()

    g := core.NewGoroutine(100*time.Millisecond, true)
    deadline := time.Now().Add(200 * time.Millisecond)

    setup.poller.Register(g, EventRead, deadline, setup.processors[0].ID())

    // Verify registration
    if g.State() != core.GoroutineBlocked {
        t.Errorf("Expected goroutine state Blocked, got %v", g.State())
    }

    info := setup.poller.GetBlockedGoroutineInfo(g.ID())
    if info == nil {
        t.Fatal("BlockedGoroutineInfo not found")
    }

    if info.EventType != EventRead {
        t.Errorf("Expected event type Read, got %v", info.EventType)
    }

    metrics := setup.poller.GetMetrics()
    if metrics.TotalEvents != 1 {
        t.Errorf("Expected 1 total event, got %d", metrics.TotalEvents)
    }
    if metrics.CurrentlyBlocked != 1 {
        t.Errorf("Expected 1 currently blocked, got %d", metrics.CurrentlyBlocked)
    }
}

func TestPollerTimeout(t *testing.T) {
    setup := newTestSetup(t)
    setup.poller.Start()
    defer setup.poller.Stop()

    g := core.NewGoroutine(100*time.Millisecond, true)
    deadline := time.Now().Add(50 * time.Millisecond)

    setup.poller.Register(g, EventRead, deadline, setup.processors[0].ID())

    // Wait for timeout with buffer
    time.Sleep(75 * time.Millisecond)

    metrics := setup.poller.GetMetrics()
    if metrics.Timeouts != 1 {
        t.Errorf("Expected 1 timeout, got %d", metrics.Timeouts)
    }

    if metrics.CurrentlyBlocked != 0 {
        t.Errorf("Expected 0 currently blocked, got %d", metrics.CurrentlyBlocked)
    }

    if info := setup.poller.GetBlockedGoroutineInfo(g.ID()); info != nil {
        t.Error("Goroutine still marked as blocked after timeout")
    }
}

func TestPollerEventCompletion(t *testing.T) {
    setup := newTestSetup(t)
    setup.poller.Start()
    defer setup.poller.Stop()

    g := core.NewGoroutine(50*time.Millisecond, true)
    deadline := time.Now().Add(200 * time.Millisecond)

    setup.poller.Register(g, EventRead, deadline, setup.processors[0].ID())

    // Wait for completion with buffer
    time.Sleep(100 * time.Millisecond)

    metrics := setup.poller.GetMetrics()
    if metrics.CompletedEvents != 1 {
        t.Errorf("Expected 1 completed event, got %d", metrics.CompletedEvents)
    }

    if metrics.CurrentlyBlocked != 0 {
        t.Errorf("Expected 0 currently blocked, got %d", metrics.CurrentlyBlocked)
    }
}

func TestConcurrentOperations(t *testing.T) {
    setup := newTestSetup(t)
    setup.poller.Start()
    defer setup.poller.Stop()

    const numGoroutines = 50
    var wg sync.WaitGroup
    wg.Add(numGoroutines)

    for i := 0; i < numGoroutines; i++ {
        go func() {
            defer wg.Done()
            g := core.NewGoroutine(20*time.Millisecond, true)
            deadline := time.Now().Add(100 * time.Millisecond)
            setup.poller.Register(g, EventRead, deadline, setup.processors[0].ID())
        }()
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
    case <-time.After(2 * time.Second):
        t.Fatal("Concurrent operations test timed out")
    }

    time.Sleep(150 * time.Millisecond) // Allow events to complete

    metrics := setup.poller.GetMetrics()
    total := metrics.CompletedEvents + metrics.Timeouts
    if total != uint64(numGoroutines) {
        t.Errorf("Expected %d total completed/timeout events, got %d",
            numGoroutines, total)
    }
}

func TestPollerMetricsAccuracy(t *testing.T) {
    setup := newTestSetup(t)
    setup.poller.Start()
    defer setup.poller.Stop()

    g1 := core.NewGoroutine(30*time.Millisecond, true)
    g2 := core.NewGoroutine(30*time.Millisecond, true)

    setup.poller.Register(g1, EventRead, time.Now().Add(100*time.Millisecond), setup.processors[0].ID())
    setup.poller.Register(g2, EventRead, time.Now().Add(20*time.Millisecond), setup.processors[0].ID())

    time.Sleep(150 * time.Millisecond)

    metrics := setup.poller.GetMetrics()
    if metrics.TotalEvents != 2 {
        t.Errorf("Expected 2 total events, got %d", metrics.TotalEvents)
    }

    if metrics.CompletedEvents + metrics.Timeouts != 2 {
        t.Error("Mismatch in completed + timeout events")
    }

    if metrics.CurrentlyBlocked != 0 {
        t.Errorf("Expected 0 currently blocked, got %d", metrics.CurrentlyBlocked)
    }
}

func TestEventTypeString(t *testing.T) {
    tests := []struct {
        eventType EventType
        want      string
    }{
        {EventRead, "read"},
        {EventWrite, "write"},
        {EventTimeout, "timeout"},
        {EventError, "error"},
        {EventType(99), "unknown"},
    }

    for _, tt := range tests {
        t.Run(tt.want, func(t *testing.T) {
            if got := tt.eventType.String(); got != tt.want {
                t.Errorf("EventType(%d).String() = %v, want %v",
                    tt.eventType, got, tt.want)
            }
        })
    }
}
