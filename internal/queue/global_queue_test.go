package queue

import (
	"sync"
	"testing"
	"time"
	"workstealing/internal/core"
)

func TestNewGlobalQueue(t *testing.T) {
	capacity := int32(100)
	gq := NewGlobalQueue(capacity)

	if gq.capacity != capacity {
		t.Errorf("Expected capacity %d, got %d", capacity, gq.capacity)
	}

	if gq.Size() != 0 {
		t.Errorf("Expected initial size 0, got %d", gq.Size())
	}

	if gq.rand == nil {
		t.Error("Random source should be initialized")
	}
}

func TestGlobalQueueSubmit(t *testing.T) {
	gq := NewGlobalQueue(5)

	// Test successful submissions
	for i := 0; i < 5; i++ {
		g := core.NewGoroutine(time.Millisecond, false)
		if !gq.Submit(g) {
			t.Errorf("Failed to submit task %d", i)
		}
	}

	// Test submission to full queue
	g := core.NewGoroutine(time.Millisecond, false)
	if gq.Submit(g) {
		t.Error("Should not be able to submit to full queue")
	}

	stats := gq.Stats()
	if stats.Submitted != 5 {
		t.Errorf("Expected 5 submitted tasks, got %d", stats.Submitted)
	}
	if stats.Rejected != 1 {
		t.Errorf("Expected 1 rejected task, got %d", stats.Rejected)
	}
}

func TestGlobalQueueFetch(t *testing.T) {
	gq := NewGlobalQueue(10)

	// Submit tasks
	for i := 0; i < 5; i++ {
		g := core.NewGoroutine(time.Millisecond, false)
		gq.Submit(g)
	}

	// Test fetching
	tasks := gq.Fetch(3)
	if len(tasks) != 3 {
		t.Errorf("Expected to fetch 3 tasks, got %d", len(tasks))
	}

	// Verify task sources
	for _, task := range tasks {
		if task.Source() != core.SourceGlobalQueue {
			t.Errorf("Expected source GlobalQueue, got %v", task.Source())
		}
	}

	// Verify remaining size
	if gq.Size() != 2 {
		t.Errorf("Expected 2 tasks remaining, got %d", gq.Size())
	}
}

func TestGlobalQueueTrySteal(t *testing.T) {
	gq := NewGlobalQueue(10)

	// Submit tasks
	for i := 0; i < 6; i++ {
		g := core.NewGoroutine(time.Millisecond, false)
		gq.Submit(g)
	}

	// Test stealing
	stolen := gq.TrySteal(2)
	if len(stolen) != 2 {
		t.Errorf("Expected to steal 2 tasks, got %d", len(stolen))
	}

	stats := gq.Stats()
	if stats.Stolen != 2 {
		t.Errorf("Expected 2 stolen tasks, got %d", stats.Stolen)
	}
}

func TestGlobalQueueConcurrency(t *testing.T) {
	gq := NewGlobalQueue(1000)
	var wg sync.WaitGroup

	// Concurrent submissions and fetches
	for i := 0; i < 100; i++ {
		wg.Add(2)

		// Submission goroutine
		go func() {
			defer wg.Done()
			g := core.NewGoroutine(time.Millisecond, false)
			gq.Submit(g)
		}()

		// Fetch goroutine
		go func() {
			defer wg.Done()
			gq.Fetch(1)
		}()
	}

	wg.Wait()
}

func TestGlobalQueueStats(t *testing.T) {
	gq := NewGlobalQueue(10)

	// Submit some tasks
	for i := 0; i < 5; i++ {
		g := core.NewGoroutine(time.Millisecond, false)
		gq.Submit(g)
	}

	// Fetch some tasks
	gq.Fetch(2)
	gq.TrySteal(1)

	stats := gq.Stats()
	if stats.Submitted != 5 {
		t.Errorf("Expected 5 submitted tasks, got %d", stats.Submitted)
	}
	if stats.Executed != 2 {
		t.Errorf("Expected 2 executed tasks, got %d", stats.Executed)
	}
	if stats.Stolen != 1 {
		t.Errorf("Expected 1 stolen task, got %d", stats.Stolen)
	}

	expectedUtilization := float64(2) / float64(10) // 2 remaining tasks out of 10 capacity
	if stats.Utilization != expectedUtilization {
		t.Errorf("Expected utilization %f, got %f", expectedUtilization, stats.Utilization)
	}
}

func TestGlobalQueueClearAndReset(t *testing.T) {
	gq := NewGlobalQueue(10)

	// Submit tasks
	for i := 0; i < 5; i++ {
		g := core.NewGoroutine(time.Millisecond, false)
		gq.Submit(g)
	}

	// Test Clear
	gq.Clear()
	if !gq.IsEmpty() {
		t.Error("Queue should be empty after Clear")
	}

	// Test ResetMetrics
	gq.ResetMetrics()
	stats := gq.Stats()
	if stats.Submitted != 0 || stats.Executed != 0 || stats.Rejected != 0 || stats.Stolen != 0 {
		t.Error("Metrics should be reset to 0")
	}
}

func TestGlobalQueueEdgeCases(t *testing.T) {
	gq := NewGlobalQueue(1)

	// Test stealing from empty queue
	if stolen := gq.TrySteal(1); stolen != nil {
		t.Error("Should not be able to steal from empty queue")
	}

	// Test fetching from empty queue
	if tasks := gq.Fetch(1); tasks != nil {
		t.Error("Should not be able to fetch from empty queue")
	}

	// Test queue state checks
	if !gq.IsEmpty() {
		t.Error("Queue should be empty")
	}

	g := core.NewGoroutine(time.Millisecond, false)
	gq.Submit(g)

	if !gq.IsFull() {
		t.Error("Queue should be full")
	}
}

func BenchmarkGlobalQueueOperations(b *testing.B) {
	gq := NewGlobalQueue(int32(b.N))

	b.Run("Submit", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			g := core.NewGoroutine(time.Microsecond, false)
			gq.Submit(g)
		}
	})

	b.Run("Fetch", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			gq.Fetch(1)
		}
	})

	b.Run("TrySteal", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			gq.TrySteal(1)
		}
	})
}

func TestGlobalQueueRandomDistribution(t *testing.T) {
	gq := NewGlobalQueue(100)
	taskMap := make(map[uint64]bool)
	var mu sync.Mutex

	// Submit tasks with unique IDs
	for i := 0; i < 50; i++ {
		g := core.NewGoroutine(time.Millisecond, false)
		gq.Submit(g)
		mu.Lock()
		taskMap[g.ID()] = false
		mu.Unlock()
	}

	// Fetch tasks multiple times
	for i := 0; i < 5; i++ {
		tasks := gq.Fetch(5)
		for _, task := range tasks {
			mu.Lock()
			if seen := taskMap[task.ID()]; seen {
				t.Errorf("Task %d fetched multiple times", task.ID())
			}
			taskMap[task.ID()] = true
			mu.Unlock()
		}
	}
}

func TestGlobalQueueConcurrentStress(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping stress test in short mode")
	}

	gq := NewGlobalQueue(1000)
	var wg sync.WaitGroup
	operations := 1000

	// Multiple concurrent operations
	for i := 0; i < operations; i++ {
		wg.Add(3)

		// Submission
		go func() {
			defer wg.Done()
			g := core.NewGoroutine(time.Millisecond, false)
			gq.Submit(g)
		}()

		// Fetching
		go func() {
			defer wg.Done()
			gq.Fetch(1)
		}()

		// Stealing
		go func() {
			defer wg.Done()
			gq.TrySteal(1)
		}()
	}

	wg.Wait()

	// Verify final state
	stats := gq.Stats()
	total := stats.Executed + stats.Stolen
	if total > int64(operations) {
		t.Error("More tasks processed than submitted")
	}
}
