package core

import (
	"sync"
	"testing"
	"time"
)

func TestNewProcessor(t *testing.T) {
    p := NewProcessor(1, 100)

    if p.ID() != 1 {
        t.Errorf("Expected processor ID 1, got %d", p.ID())
    }

    if p.State() != ProcessorIdle {
        t.Errorf("Expected initial state ProcessorIdle, got %v", p.State())
    }

    if p.maxQueueSize != 100 {
        t.Errorf("Expected queue size 100, got %d", p.maxQueueSize)
    }
}

func TestProcessorStateTransitions(t *testing.T) {
    p := NewProcessor(1, 100)

    states := []ProcessorState{
        ProcessorRunning,
        ProcessorStealing,
        ProcessorIdle,
    }

    for _, state := range states {
        p.SetState(state)
        if p.State() != state {
            t.Errorf("Expected state %v, got %v", state, p.State())
        }
        time.Sleep(10 * time.Millisecond) // Allow for metrics collection
    }

    metrics := p.GetMetrics()
    if metrics.TotalIdleTime == 0 && metrics.TotalRunningTime == 0 {
        t.Error("Expected non-zero time metrics")
    }
}

func TestProcessorTaskSourceTracking(t *testing.T) {
    p := NewProcessor(1, 100)

    // Test different task sources
    sources := []struct {
        source TaskSource
        count  int
    }{
        {SourceGlobalQueue, 3},
        {SourceLocalQueue, 2},
        {SourceNetworkPoller, 1},
    }

    for _, s := range sources {
        for i := 0; i < s.count; i++ {
            g := NewGoroutine(time.Millisecond, false)
            g.SetSource(s.source, 0)
            p.Push(g)
        }
    }

    metrics := p.GetMetrics()

    // Verify metrics for each source
    if metrics.GlobalQueueSteals != 3 {
        t.Errorf("Expected 3 global queue tasks, got %d", metrics.GlobalQueueSteals)
    }
    if metrics.LocalQueueSteals != 2 {
        t.Errorf("Expected 2 local queue tasks, got %d", metrics.LocalQueueSteals)
    }
    if metrics.TasksFromPoller != 1 {
        t.Errorf("Expected 1 poller task, got %d", metrics.TasksFromPoller)
    }
}

func TestProcessorQueueOperations(t *testing.T) {
    p := NewProcessor(1, 3)

    // Create test goroutines
    g1 := NewGoroutine(time.Millisecond, false)
    g2 := NewGoroutine(time.Millisecond, false)
    g3 := NewGoroutine(time.Millisecond, false)
    g4 := NewGoroutine(time.Millisecond, false)

    // Test pushing
    if !p.Push(g1) {
        t.Error("Failed to push g1")
    }
    if !p.Push(g2) {
        t.Error("Failed to push g2")
    }
    if !p.Push(g3) {
        t.Error("Failed to push g3")
    }
    if p.Push(g4) {
        t.Error("Should not be able to push g4 (queue full)")
    }

    // Test popping
    if g := p.Pop(); g != g1 {
        t.Error("Expected g1 from Pop()")
    }
    if g := p.Pop(); g != g2 {
        t.Error("Expected g2 from Pop()")
    }
    if g := p.Pop(); g != g3 {
        t.Error("Expected g3 from Pop()")
    }
    if g := p.Pop(); g != nil {
        t.Error("Expected nil from empty queue")
    }
}

func TestProcessorWorkStealing(t *testing.T) {
    p1 := NewProcessor(1, 10)
    p2 := NewProcessor(2, 10)

    // Add goroutines to p1
    for i := 0; i < 6; i++ {
        g := NewGoroutine(time.Millisecond, false)
        g.SetSource(SourceLocalQueue, 0)
        if !p1.Push(g) {
            t.Errorf("Failed to push goroutine %d", i)
        }
    }

    // Test stealing
    stolen := p2.Steal(p1)
    expectedSteal := 3 // Should steal half

    if len(stolen) != expectedSteal {
        t.Errorf("Expected to steal %d goroutines, got %d", expectedSteal, len(stolen))
    }

    // Verify source marking
    for _, g := range stolen {
        if g.Source() != SourceStolen {
            t.Errorf("Expected stolen task to have SourceStolen, got %v", g.Source())
        }
        if g.StolenFrom() != p1.ID() {
            t.Errorf("Expected stolenFrom to be %d, got %d", p1.ID(), g.StolenFrom())
        }
    }

    // Verify metrics
    metrics := p2.GetMetrics()
    if metrics.StealsAttempted != 1 {
        t.Errorf("Expected 1 steal attempt, got %d", metrics.StealsAttempted)
    }
    if metrics.StealsSuccessful != 1 {
        t.Errorf("Expected 1 successful steal, got %d", metrics.StealsSuccessful)
    }
    if metrics.LocalQueueSteals != 1 {
        t.Errorf("Expected 1 local queue steal, got %d", metrics.LocalQueueSteals)
    }
}

func TestProcessorVictimSelection(t *testing.T) {
    processors := make([]*Processor, 5)
    for i := range processors {
        processors[i] = NewProcessor(uint32(i), 100)
    }

    // Add some tasks to processors
    for i := 1; i < len(processors); i++ {
        g := NewGoroutine(time.Millisecond, false)
        processors[i].Push(g)
    }

    // Test victim selection
    p := processors[0]
    victim := p.selectVictim(processors)

    if victim == nil {
        t.Error("Expected to find a victim")
    }

    if victim.ID() == p.ID() {
        t.Error("Processor selected itself as victim")
    }
}

func TestProcessorConcurrentOperations(t *testing.T) {
    p := NewProcessor(1, 100)
    var wg sync.WaitGroup

    // Test concurrent pushes and pops
    for i := 0; i < 100; i++ {
        wg.Add(2)

        go func() {
            defer wg.Done()
            g := NewGoroutine(time.Millisecond, false)
            p.Push(g)
        }()

        go func() {
            defer wg.Done()
            p.Pop()
        }()
    }

    wg.Wait()
}

func TestProcessorMetrics(t *testing.T) {
    p := NewProcessor(1, 100)

    // Test initial metrics
    initial := p.GetMetrics()
    if initial.TasksExecuted != 0 {
        t.Error("Expected initial tasks executed to be 0")
    }

    // Test metrics after operations
    p.SetState(ProcessorRunning)
    time.Sleep(50 * time.Millisecond)
    p.SetState(ProcessorIdle)

    metrics := p.GetMetrics()
    if metrics.TotalRunningTime < 50*time.Millisecond {
        t.Error("Expected running time to be at least 50ms")
    }
}

func TestProcessorStatus(t *testing.T) {
    p := NewProcessor(1, 100)
    g := NewGoroutine(time.Millisecond, false)

    p.Push(g)
    status := p.GetStatus()

    if status.QueueSize != 1 {
        t.Errorf("Expected queue size 1, got %d", status.QueueSize)
    }

    if status.State != ProcessorIdle {
        t.Errorf("Expected ProcessorIdle state, got %v", status.State)
    }
}

func TestProcessorEdgeCases(t *testing.T) {
    p := NewProcessor(1, 1)

    // Test stealing from self
    if stolen := p.Steal(p); stolen != nil {
        t.Error("Should not be able to steal from self")
    }

    // Test stealing from nil processor
    if stolen := p.Steal(nil); stolen != nil {
        t.Error("Should not be able to steal from nil processor")
    }

    // Test queue overflow
    g1 := NewGoroutine(time.Millisecond, false)
    g2 := NewGoroutine(time.Millisecond, false)

    p.Push(g1)
    if p.Push(g2) {
        t.Error("Should not be able to push to full queue")
    }
}

func BenchmarkProcessorPushPop(b *testing.B) {
    p := NewProcessor(1, b.N)
    g := NewGoroutine(time.Millisecond, false)

    b.ResetTimer()
    for i := 0; i < b.N; i++ {
        p.Push(g)
        p.Pop()
    }
}

func BenchmarkProcessorSteal(b *testing.B) {
    p1 := NewProcessor(1, b.N)
    p2 := NewProcessor(2, b.N)

    // Fill p1's queue
    for i := 0; i < b.N; i++ {
        p1.Push(NewGoroutine(time.Millisecond, false))
    }

    b.ResetTimer()
    for i := 0; i < b.N; i++ {
        p2.Steal(p1)
    }
}

func TestProcessorConcurrentStateTransitions(t *testing.T) {
    p := NewProcessor(1, 100)
    var wg sync.WaitGroup

    for i := 0; i < 100; i++ {
        wg.Add(1)
        go func(state ProcessorState) {
            defer wg.Done()
            p.SetState(state)
            _ = p.State()
            _ = p.GetStatus()
            _ = p.GetMetrics()
        }(ProcessorState(i % 3))
    }

    wg.Wait()
}

func TestProcessorQueueConcurrency(t *testing.T) {
    p := NewProcessor(1, 1000)
    var wg sync.WaitGroup

    // Concurrent pushes
    for i := 0; i < 100; i++ {
        wg.Add(1)
        go func() {
            defer wg.Done()
            g := NewGoroutine(time.Millisecond, false)
            p.Push(g)
        }()
    }

    // Concurrent steals and pops
    for i := 0; i < 50; i++ {
        wg.Add(2)
        go func() {
            defer wg.Done()
            p.Pop()
        }()
        go func() {
            defer wg.Done()
            victim := NewProcessor(2, 1000)
            p.Steal(victim)
        }()
    }

    wg.Wait()
}
