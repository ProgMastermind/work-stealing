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
		poller:      poller,
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
	done := make(chan struct{})

	// Register goroutine
	setup.poller.Register(g, EventRead, deadline, setup.processors[0].ID(), done)

	// Start a goroutine to wait for completion signal
	completed := make(chan bool)
	go func() {
		<-done
		completed <- true
	}()

	// Wait for completion or timeout
	select {
	case <-completed:
		// Verify goroutine state after completion
		if g.State() != core.GoroutineRunnable {
			t.Errorf("Expected goroutine state Runnable, got %v", g.State())
		}
	case <-time.After(300 * time.Millisecond):
		t.Fatal("Registration test timed out")
	}

	metrics := setup.poller.GetMetrics()
	if metrics.CompletedEvents != 1 {
		t.Errorf("Expected 1 completed event, got %d", metrics.CompletedEvents)
	}
}

func TestPollerTimeout(t *testing.T) {
	setup := newTestSetup(t)
	setup.poller.Start()
	defer setup.poller.Stop()

	g := core.NewGoroutine(100*time.Millisecond, true)
	deadline := time.Now().Add(50 * time.Millisecond)

	done := make(chan struct{})
	setup.poller.Register(g, EventRead, deadline, setup.processors[0].ID(), done)

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
	done := make(chan struct{})

	setup.poller.Register(g, EventRead, deadline, setup.processors[0].ID(), done)

	// Wait for completion signal
	select {
	case <-done:
		// Success case
		if g.State() != core.GoroutineRunnable {
			t.Errorf("Expected goroutine state Runnable, got %v", g.State())
		}
	case <-time.After(250 * time.Millisecond):
		t.Fatal("Event completion test timed out")
	}

	metrics := setup.poller.GetMetrics()
	if metrics.CompletedEvents != 1 {
		t.Errorf("Expected 1 completed event, got %d", metrics.CompletedEvents)
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
			done := make(chan struct{})

			setup.poller.Register(g, EventRead, deadline, setup.processors[0].ID(), done)

			// Wait for completion signal
			select {
			case <-done:
				// Verify goroutine is in correct state
				if g.State() != core.GoroutineRunnable {
					t.Errorf("Expected goroutine state Runnable, got %v", g.State())
				}
			case <-time.After(150 * time.Millisecond):
				t.Errorf("Operation timed out for goroutine")
			}
		}()
	}

	// Wait for all goroutines to complete
	waitChan := make(chan bool)
	go func() {
		wg.Wait()
		waitChan <- true
	}()

	select {
	case <-waitChan:
		// All goroutines completed successfully
	case <-time.After(2 * time.Second):
		t.Fatal("Concurrent operations test timed out")
	}

	// Optional: verify no goroutines are left in blocked state
	time.Sleep(50 * time.Millisecond) // Small buffer for cleanup
	if len(setup.poller.events) > 0 {
		t.Errorf("Expected all events to be cleared, found %d remaining", len(setup.poller.events))
	}
}

func TestMultipleGoroutineHandling(t *testing.T) {
	setup := newTestSetup(t)
	setup.poller.Start()
	defer setup.poller.Stop()

	g1 := core.NewGoroutine(30*time.Millisecond, true)
	g2 := core.NewGoroutine(30*time.Millisecond, true)

	done1 := make(chan struct{})
	done2 := make(chan struct{})

	// Register both goroutines
	setup.poller.Register(g1, EventRead, time.Now().Add(100*time.Millisecond), setup.processors[0].ID(), done1)
	setup.poller.Register(g2, EventRead, time.Now().Add(100*time.Millisecond), setup.processors[0].ID(), done2)

	// Use WaitGroup to track completions
	var wg sync.WaitGroup
	wg.Add(2)

	// Handle completion for g1
	go func() {
		defer wg.Done()
		select {
		case <-done1:
			if g1.State() != core.GoroutineRunnable {
				t.Errorf("G1: Expected state Runnable, got %v", g1.State())
			}
		case <-time.After(500 * time.Millisecond):
			t.Error("Timeout waiting for g1")
		}
	}()

	// Handle completion for g2
	go func() {
		defer wg.Done()
		select {
		case <-done2:
			if g2.State() != core.GoroutineRunnable {
				t.Errorf("G2: Expected state Runnable, got %v", g2.State())
			}
		case <-time.After(500 * time.Millisecond):
			t.Error("Timeout waiting for g2")
		}
	}()

	// Wait for both completions with timeout
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		// Success case
	case <-time.After(1 * time.Second):
		t.Fatal("Test timed out waiting for goroutine completion")
	}

	// Give some time for cleanup
	time.Sleep(50 * time.Millisecond)

	// Verify cleanup
	if len(setup.poller.events) != 0 {
		t.Errorf("Expected all events to be cleared, found %d remaining",
			len(setup.poller.events))
	}

	if len(setup.poller.blockedGoroutines) != 0 {
		t.Errorf("Expected no blocked goroutines, found %d remaining",
			len(setup.poller.blockedGoroutines))
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
