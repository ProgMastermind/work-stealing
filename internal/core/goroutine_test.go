package core

import (
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestGoroutineCreation(t *testing.T) {
    workload := 100 * time.Millisecond
    g := NewGoroutine(workload, false)

    // Test basic properties
    if g.ID() == 0 {
        t.Fatal("Expected non-zero ID")
    }

    if g.State() != GoroutineCreated {
        t.Errorf("Expected initial state Created, got %v", g.State())
    }

    if g.Workload() != workload {
        t.Errorf("Expected workload %v, got %v", workload, g.Workload())
    }

    if g.IsBlocking() {
        t.Error("Expected non-blocking goroutine")
    }

    // Test initial transition
    transitions := g.GetTransitions()
    if len(transitions) != 1 {
        t.Fatalf("Expected 1 initial transition, got %d", len(transitions))
    }

    initialTransition := transitions[0]
    if initialTransition.From != "created" || initialTransition.To != "ready" {
        t.Errorf("Unexpected initial transition: %s -> %s",
            initialTransition.From, initialTransition.To)
    }

    // Test zero workload case
    g2 := NewGoroutine(0, false)
    if g2.Workload() <= 0 {
        t.Error("Expected minimum workload for zero input")
    }
}

func TestGoroutineLifecycle(t *testing.T) {
    testDone := make(chan bool)

    go func() {
        defer func() { testDone <- true }()

        g := NewGoroutine(50*time.Millisecond, false)

        // Define and test state transitions
        transitions := []struct {
            name      string
            operation func()
            expected  GoroutineState
        }{
            {"To Runnable", func() { g.SetState(GoroutineRunnable) }, GoroutineRunnable},
            {"Start", func() { g.Start() }, GoroutineRunning},
            {"Finish", func() { g.Finish(nil, nil) }, GoroutineFinished},
        }

        for _, tr := range transitions {
            tr.operation()
            if state := g.State(); state != tr.expected {
                t.Errorf("%s: Expected state %v, got %v", tr.name, tr.expected, state)
            }
        }

        // Verify execution time
        execTime := g.ExecutionTime()
        if execTime <= 0 {
            t.Error("Expected positive execution time, got", execTime)
        }
    }()

    // Add timeout to prevent test hanging
    select {
    case <-testDone:
        // Test completed successfully
    case <-time.After(1 * time.Second):
        t.Fatal("Test timed out after 1 second")
    }
}

func TestGoroutineTransitions(t *testing.T) {
    g := NewGoroutine(100*time.Millisecond, false)

    expectedTransitions := []struct {
        from   string
        to     string
        reason string
    }{
        {"ready", "running", "test_transition_1"},
        {"running", "blocked", "test_transition_2"},
        {"blocked", "runnable", "test_transition_3"},
    }

    // Add transitions with small delays to ensure measurable durations
    for _, tr := range expectedTransitions {
        time.Sleep(10 * time.Millisecond)
        g.AddTransition(tr.from, tr.to, tr.reason)
    }

    // Verify transitions
    transitions := g.GetTransitions()
    if len(transitions) != len(expectedTransitions)+1 { // +1 for initial transition
        t.Fatalf("Expected %d transitions, got %d",
            len(expectedTransitions)+1, len(transitions))
    }

    // Skip initial transition in verification
    for i, expected := range expectedTransitions {
        actual := transitions[i+1]
        if actual.From != expected.from ||
           actual.To != expected.to ||
           actual.Reason != expected.reason {
            t.Errorf("Transition %d mismatch: expected %v->%v(%s), got %v->%v(%s)",
                i, expected.from, expected.to, expected.reason,
                actual.From, actual.To, actual.Reason)
        }
        if actual.Duration <= 0 {
            t.Errorf("Transition %d: expected positive duration, got %v",
                i, actual.Duration)
        }
    }
}

func TestConcurrentTransitions(t *testing.T) {
    g := NewGoroutine(100*time.Millisecond, false)
    const numGoroutines = 100

    var wg sync.WaitGroup
    wg.Add(numGoroutines)

    // Launch concurrent goroutines adding transitions
    for i := 0; i < numGoroutines; i++ {
        go func(id int) {
            defer wg.Done()
            g.AddTransition(
                fmt.Sprintf("state_%d", id),
                fmt.Sprintf("state_%d", id+1),
                fmt.Sprintf("transition_%d", id),
            )
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
    case <-time.After(2 * time.Second):
        t.Fatal("Concurrent transitions test timed out")
    }

    // Verify transitions
    transitions := g.GetTransitions()
    if len(transitions) != numGoroutines+1 { // +1 for initial transition
        t.Errorf("Expected %d transitions, got %d",
            numGoroutines+1, len(transitions))
    }
}

func TestSourceChanges(t *testing.T) {
    g := NewGoroutine(100*time.Millisecond, false)

    changes := []struct {
        source     TaskSource
        stolenFrom uint32
        expectStr  string
    }{
        {SourceGlobalQueue, 0, "global_queue"},
        {SourceStolen, 1, "stolen"},
        {SourceNetworkPoller, 2, "network_poller"},
        {SourceLocalQueue, 3, "local_queue"},
    }

    for i, change := range changes {
        g.SetSource(change.source, change.stolenFrom)

        if g.Source() != change.source {
            t.Errorf("Case %d: Expected source %v, got %v",
                i, change.source, g.Source())
        }

        if g.StolenFrom() != change.stolenFrom {
            t.Errorf("Case %d: Expected stolenFrom %v, got %v",
                i, change.stolenFrom, g.StolenFrom())
        }

        if g.Source().String() != change.expectStr {
            t.Errorf("Case %d: Expected source string %v, got %v",
                i, change.expectStr, g.Source().String())
        }
    }
}

func TestStateStrings(t *testing.T) {
    tests := []struct {
        state    GoroutineState
        expected string
    }{
        {GoroutineCreated, "created"},
        {GoroutineRunnable, "runnable"},
        {GoroutineRunning, "running"},
        {GoroutineBlocked, "blocked"},
        {GoroutineFinished, "finished"},
        {GoroutineState(99), "unknown"},
    }

    for _, test := range tests {
        if got := test.state.String(); got != test.expected {
            t.Errorf("State %v: expected %q, got %q",
                test.state, test.expected, got)
        }
    }
}

func TestTaskSourceStrings(t *testing.T) {
    tests := []struct {
        source   TaskSource
        expected string
    }{
        {SourceGlobalQueue, "global_queue"},
        {SourceLocalQueue, "local_queue"},
        {SourceStolen, "stolen"},
        {SourceNetworkPoller, "network_poller"},
        {TaskSource(99), "unknown"},
    }

    for _, test := range tests {
        if got := test.source.String(); got != test.expected {
            t.Errorf("Source %v: expected %q, got %q",
                test.source, test.expected, got)
        }
    }
}

func TestGetHistory(t *testing.T) {
    g := NewGoroutine(100*time.Millisecond, true)

    // Create a sequence of transitions
    transitions := []struct {
        from   string
        to     string
        reason string
    }{
        {"ready", "running", "start"},
        {"running", "blocked", "io_wait"},
        {"blocked", "runnable", "io_complete"},
        {"runnable", "finished", "completion"},
    }

    for _, tr := range transitions {
        time.Sleep(10 * time.Millisecond) // Ensure measurable durations
        g.AddTransition(tr.from, tr.to, tr.reason)
    }

    history := g.GetHistory()

    // Verify history contains all transitions
    for _, tr := range transitions {
        if !strings.Contains(history, tr.from) ||
           !strings.Contains(history, tr.to) ||
           !strings.Contains(history, tr.reason) {
            t.Errorf("History missing transition: %s->%s(%s)",
                tr.from, tr.to, tr.reason)
        }
    }

    // Verify format
    if !strings.Contains(history, fmt.Sprintf("G%d History:", g.ID())) {
        t.Error("History missing header")
    }

    if !strings.Contains(history, "duration:") {
        t.Error("History missing duration information")
    }
}
