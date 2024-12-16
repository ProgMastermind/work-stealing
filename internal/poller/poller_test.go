package poller

import (
	"testing"
	"time"
	"workstealing/internal/core"
)

func TestNewNetworkPoller(t *testing.T) {
    processors := []*core.Processor{
        core.NewProcessor(1, 100),
        core.NewProcessor(2, 100),
    }

    np := NewNetworkPoller(processors)
    if np == nil {
        t.Fatal("Expected non-nil NetworkPoller")
    }

    if len(np.processors) != 2 {
        t.Errorf("Expected 2 processors, got %d", len(np.processors))
    }

    if np.events == nil {
        t.Error("Events map should be initialized")
    }

    if np.blockedGoroutines == nil {
        t.Error("BlockedGoroutines map should be initialized")
    }
}

func TestPollerStartStop(t *testing.T) {
    processors := []*core.Processor{core.NewProcessor(1, 100)}
    np := NewNetworkPoller(processors)

    // Test Start
    np.Start()
    if !np.running.Load() {
        t.Error("Expected poller to be running after Start")
    }

    // Test double start
    np.Start()
    if !np.running.Load() {
        t.Error("Poller should remain running after second Start")
    }

    // Test Stop
    np.Stop()
    if np.running.Load() {
        t.Error("Expected poller to not be running after Stop")
    }

    // Test double stop
    np.Stop()
    if np.running.Load() {
        t.Error("Poller should remain stopped after second Stop")
    }
}

func TestPollerRegistration(t *testing.T) {
    processors := []*core.Processor{core.NewProcessor(1, 100)}
    np := NewNetworkPoller(processors)
    np.Start()
    defer np.Stop()

    g := core.NewGoroutine(100*time.Millisecond, true)
    deadline := time.Now().Add(200 * time.Millisecond)

    np.Register(g, EventRead, deadline, processors[0].ID())

    // Verify registration
    if np.GetMetrics().TotalEvents != 1 {
        t.Error("Expected total events to be 1")
    }

    if np.GetMetrics().CurrentlyBlocked != 1 {
        t.Error("Expected currently blocked to be 1")
    }

    // Check blocked goroutine info
    info := np.GetBlockedGoroutineInfo(g.ID())
    if info == nil {
        t.Fatal("Expected non-nil BlockedGoroutineInfo")
    }

    if info.EventType != EventRead {
        t.Errorf("Expected event type %v, got %v", EventRead, info.EventType)
    }

    if info.ProcessorID != processors[0].ID() {
        t.Errorf("Expected processor ID %d, got %d", processors[0].ID(), info.ProcessorID)
    }
}

func TestPollerCompletion(t *testing.T) {
    processors := []*core.Processor{core.NewProcessor(1, 100)}
    np := NewNetworkPoller(processors)
    np.Start()
    defer np.Stop()

    g := core.NewGoroutine(100*time.Millisecond, true)
    deadline := time.Now().Add(200 * time.Millisecond)

    np.Register(g, EventRead, deadline, processors[0].ID())

    // Wait for completion
    time.Sleep(60 * time.Millisecond)

    metrics := np.GetMetrics()
    if metrics.CompletedEvents != 1 {
        t.Errorf("Expected 1 completed event, got %d", metrics.CompletedEvents)
    }

    if metrics.CurrentlyBlocked != 0 {
        t.Errorf("Expected 0 currently blocked, got %d", metrics.CurrentlyBlocked)
    }

    if g.Source() != core.SourceNetworkPoller {
        t.Errorf("Expected source to be SourceNetworkPoller, got %v", g.Source())
    }
}

func TestPollerTimeout(t *testing.T) {
    processors := []*core.Processor{core.NewProcessor(1, 100)}
    np := NewNetworkPoller(processors)
    np.Start()
    defer np.Stop()

    g := core.NewGoroutine(100*time.Millisecond, true)
    deadline := time.Now().Add(10 * time.Millisecond)

    np.Register(g, EventRead, deadline, processors[0].ID())

    // Wait for timeout
    time.Sleep(20 * time.Millisecond)

    metrics := np.GetMetrics()
    if metrics.Timeouts != 1 {
        t.Errorf("Expected 1 timeout, got %d", metrics.Timeouts)
    }

    if metrics.CurrentlyBlocked != 0 {
        t.Errorf("Expected 0 currently blocked, got %d", metrics.CurrentlyBlocked)
    }
}

func TestPollerConcurrency(t *testing.T) {
    processors := []*core.Processor{
        core.NewProcessor(1, 100),
        core.NewProcessor(2, 100),
    }
    np := NewNetworkPoller(processors)
    np.Start()
    defer np.Stop()

    const numGoroutines = 100
    done := make(chan bool)

    go func() {
        for i := 0; i < numGoroutines; i++ {
            g := core.NewGoroutine(10*time.Millisecond, true)
            deadline := time.Now().Add(50 * time.Millisecond)
            np.Register(g, EventRead, deadline, processors[i%2].ID())
        }
        done <- true
    }()

    <-done
    time.Sleep(60 * time.Millisecond)

    metrics := np.GetMetrics()
    totalHandled := metrics.CompletedEvents + metrics.Timeouts
    if totalHandled != uint64(numGoroutines) {
        t.Errorf("Expected %d total handled events, got %d", numGoroutines, totalHandled)
    }
}

func TestPollerMetrics(t *testing.T) {
    processors := []*core.Processor{core.NewProcessor(1, 100)}
    np := NewNetworkPoller(processors)
    np.Start()
    defer np.Stop()

    // Register several events with different timings
    for i := 0; i < 5; i++ {
        g := core.NewGoroutine(10*time.Millisecond, true)
        deadline := time.Now().Add(time.Duration(30+i*10) * time.Millisecond)
        np.Register(g, EventRead, deadline, processors[0].ID())
    }

    // Wait for processing
    time.Sleep(100 * time.Millisecond)

    metrics := np.GetMetrics()
    if metrics.TotalEvents != 5 {
        t.Errorf("Expected 5 total events, got %d", metrics.TotalEvents)
    }

    if metrics.AverageBlockTime == 0 {
        t.Error("Expected non-zero average block time")
    }
}

func TestPollerLoadBalancing(t *testing.T) {
    processors := []*core.Processor{
        core.NewProcessor(1, 100),
        core.NewProcessor(2, 100),
    }
    np := NewNetworkPoller(processors)
    np.Start()
    defer np.Stop()

    // Register multiple events
    const numEvents = 10
    for i := 0; i < numEvents; i++ {
        g := core.NewGoroutine(10*time.Millisecond, true)
        deadline := time.Now().Add(100 * time.Millisecond)
        np.Register(g, EventRead, deadline, processors[0].ID())
    }

    // Wait for processing
    time.Sleep(60 * time.Millisecond)

    // Check processor queue sizes
    p1Status := processors[0].GetStatus()
    p2Status := processors[1].GetStatus()

    // Verify some form of distribution
    if p1Status.QueueSize+p2Status.QueueSize != numEvents {
        t.Errorf("Expected %d total tasks in processor queues", numEvents)
    }
}

func BenchmarkPollerRegistration(b *testing.B) {
    processors := []*core.Processor{core.NewProcessor(1, b.N)}
    np := NewNetworkPoller(processors)
    np.Start()
    defer np.Stop()

    b.ResetTimer()
    for i := 0; i < b.N; i++ {
        g := core.NewGoroutine(time.Millisecond, true)
        deadline := time.Now().Add(100 * time.Millisecond)
        np.Register(g, EventRead, deadline, processors[0].ID())
    }
}

func BenchmarkPollerProcessing(b *testing.B) {
    processors := []*core.Processor{core.NewProcessor(1, b.N)}
    np := NewNetworkPoller(processors)
    np.Start()
    defer np.Stop()

    // Setup
    for i := 0; i < b.N; i++ {
        g := core.NewGoroutine(time.Microsecond, true)
        deadline := time.Now().Add(time.Millisecond)
        np.Register(g, EventRead, deadline, processors[0].ID())
    }

    b.ResetTimer()
    // Wait for processing
    for np.GetMetrics().CompletedEvents < uint64(b.N) {
        time.Sleep(time.Microsecond)
    }
}
