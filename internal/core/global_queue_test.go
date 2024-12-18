package core

import (
	"sync"
	"testing"
	"time"
)

func TestNewGlobalQueue(t *testing.T) {
	tests := []struct {
		name         string
		capacity     int32
		wantCapacity int32
	}{
		{
			name:         "Valid capacity",
			capacity:     100,
			wantCapacity: 100,
		},
		{
			name:         "Zero capacity",
			capacity:     0,
			wantCapacity: defaultCapacity,
		},
		{
			name:         "Negative capacity",
			capacity:     -1,
			wantCapacity: defaultCapacity,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gq := NewGlobalQueue(tt.capacity)
			if gq == nil {
				t.Fatal("NewGlobalQueue returned nil")
			}
			if gq.capacity != tt.wantCapacity {
				t.Errorf("capacity = %v, want %v", gq.capacity, tt.wantCapacity)
			}
			if gq.Size() != 0 {
				t.Errorf("initial size = %v, want 0", gq.Size())
			}
		})
	}
}

func TestGlobalQueueSubmit(t *testing.T) {
	gq := NewGlobalQueue(5)

	t.Run("Basic Submit", func(t *testing.T) {
		g := NewGoroutine(10*time.Millisecond, false)
		if !gq.Submit(g) {
			t.Error("Submit should succeed")
		}
		if gq.Size() != 1 {
			t.Errorf("size = %v, want 1", gq.Size())
		}
	})

	t.Run("Submit Nil", func(t *testing.T) {
		if gq.Submit(nil) {
			t.Error("Submit(nil) should return false")
		}
	})

	t.Run("Submit Until Full", func(t *testing.T) {
		// Fill the queue
		for i := 0; i < 4; i++ { // Already has 1 item
			g := NewGoroutine(10*time.Millisecond, false)
			if !gq.Submit(g) {
				t.Errorf("Submit %d should succeed", i)
			}
		}

		// Try to submit when full
		g := NewGoroutine(10*time.Millisecond, false)
		if gq.Submit(g) {
			t.Error("Submit should fail when queue is full")
		}

		stats := gq.Stats()
		if stats.Rejected != 1 {
			t.Errorf("rejected count = %v, want 1", stats.Rejected)
		}
	})
}

func TestGlobalQueueTrySteal(t *testing.T) {
	gq := NewGlobalQueue(10)

	// Add tasks
	for i := 0; i < 6; i++ {
		g := NewGoroutine(10*time.Millisecond, false)
		gq.Submit(g)
	}

	t.Run("Valid Steal", func(t *testing.T) {
		stolen := gq.TrySteal(2)
		if len(stolen) != 2 {
			t.Errorf("stolen count = %v, want 2", len(stolen))
		}

		// Verify stolen tasks
		for _, g := range stolen {
			if g == nil {
				t.Error("stolen task should not be nil")
			}
			if g.Source() != SourceGlobalQueue {
				t.Error("task source should be global queue")
			}
		}

		if gq.Size() != 4 {
			t.Errorf("remaining size = %v, want 4", gq.Size())
		}
	})

	t.Run("Invalid Steal Parameters", func(t *testing.T) {
		if stolen := gq.TrySteal(0); stolen != nil {
			t.Error("TrySteal(0) should return nil")
		}
		if stolen := gq.TrySteal(-1); stolen != nil {
			t.Error("TrySteal(-1) should return nil")
		}
	})
}

func TestGlobalQueueTake(t *testing.T) {
	gq := NewGlobalQueue(10)

	// Add tasks
	for i := 0; i < 5; i++ {
		g := NewGoroutine(10*time.Millisecond, false)
		gq.Submit(g)
	}

	t.Run("Valid Take", func(t *testing.T) {
		taken := gq.Take(3)
		if len(taken) != 3 {
			t.Errorf("taken count = %v, want 3", len(taken))
		}
		if gq.Size() != 2 {
			t.Errorf("remaining size = %v, want 2", gq.Size())
		}
	})

	t.Run("Take More Than Available", func(t *testing.T) {
		taken := gq.Take(5)
		if len(taken) != 2 { // Only 2 remaining
			t.Errorf("taken count = %v, want 2", len(taken))
		}
		if !gq.IsEmpty() {
			t.Error("queue should be empty after taking all tasks")
		}
	})

	t.Run("Take From Empty Queue", func(t *testing.T) {
		if taken := gq.Take(1); taken != nil {
			t.Error("Take from empty queue should return nil")
		}
	})
}

func TestGlobalQueueConcurrent(t *testing.T) {
	gq := NewGlobalQueue(1000)
	const numGoroutines = 50
	const opsPerGoroutine = 20

	var wg sync.WaitGroup
	wg.Add(numGoroutines * 2) // Producers and consumers

	// Start producers
	for i := 0; i < numGoroutines; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < opsPerGoroutine; j++ {
				g := NewGoroutine(time.Millisecond, false)
				gq.Submit(g)
			}
		}()
	}

	// Start consumers
	for i := 0; i < numGoroutines; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < opsPerGoroutine; j++ {
				if j%2 == 0 {
					gq.Take(1)
				} else {
					gq.TrySteal(1)
				}
			}
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
	case <-time.After(5 * time.Second):
		t.Fatal("concurrent test timed out")
	}

	// Verify final state
	stats := gq.Stats()
	if stats.Submitted == 0 {
		t.Error("no tasks were submitted")
	}
	if stats.Executed+stats.Stolen == 0 {
		t.Error("no tasks were processed")
	}
}

func TestGlobalQueueStats(t *testing.T) {
	gq := NewGlobalQueue(10)

	// Submit tasks
	for i := 0; i < 5; i++ {
		g := NewGoroutine(10*time.Millisecond, false)
		gq.Submit(g)
	}

	// Take some tasks
	gq.Take(2)

	// Steal some tasks
	gq.TrySteal(2)

	stats := gq.Stats()
	if stats.Submitted != 5 {
		t.Errorf("submitted count = %v, want 5", stats.Submitted)
	}
	if stats.Executed != 2 {
		t.Errorf("executed count = %v, want 2", stats.Executed)
	}
	if stats.Stolen != 2 {
		t.Errorf("stolen count = %v, want 2", stats.Stolen)
	}
	if stats.CurrentSize != 1 {
		t.Errorf("current size = %v, want 1", stats.CurrentSize)
	}
}

func TestGlobalQueueClearAndReset(t *testing.T) {
	gq := NewGlobalQueue(10)

	// Add tasks
	for i := 0; i < 5; i++ {
		g := NewGoroutine(10*time.Millisecond, false)
		gq.Submit(g)
	}

	t.Run("Clear", func(t *testing.T) {
		gq.Clear()
		if !gq.IsEmpty() {
			t.Error("queue should be empty after Clear")
		}
		if gq.Size() != 0 {
			t.Errorf("size = %v, want 0", gq.Size())
		}
	})

	t.Run("Reset Metrics", func(t *testing.T) {
		gq.ResetMetrics()
		stats := gq.Stats()
		if stats.Submitted != 0 || stats.Executed != 0 || stats.Stolen != 0 || stats.Rejected != 0 {
			t.Error("metrics should be zero after reset")
		}
	})
}
