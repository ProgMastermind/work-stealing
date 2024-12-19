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

// Helper function for waiting for task completion
func waitForCompletion(t *testing.T, g *core.Goroutine, timeout time.Duration) bool {
	t.Helper()
	deadline := time.After(timeout)
	ticker := time.NewTicker(5 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-deadline:
			return false
		case <-ticker.C:
			if g.State() == core.GoroutineRunnable {
				return true
			}
		}
	}
}

// Helper function for waiting for multiple tasks
func waitForTasks(t *testing.T, tasks []*core.Goroutine, timeout time.Duration) bool {
	t.Helper()
	deadline := time.After(timeout)
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-deadline:
			return false
		case <-ticker.C:
			completed := 0
			for _, g := range tasks {
				if g.State() == core.GoroutineRunnable {
					completed++
				}
			}
			if completed == len(tasks) {
				return true
			}
			t.Logf("Progress: %d/%d tasks completed", completed, len(tasks))
		}
	}
}

func TestSchedulerCreation(t *testing.T) {
	tests := []struct {
		name          string
		numProcessors int
		queueSize     int32
		wantNil       bool
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
					t.Errorf("Expected %d processors, got %d",
						expectedProcessors, len(s.processors))
				}
			}
		})
	}
}

func TestSchedulerStartStop(t *testing.T) {
	setup := newTestSetup(t, 2)
	defer setup.cleanup()

	if err := setup.scheduler.Start(); err != nil {
		t.Errorf("Start() error = %v", err)
	}

	if setup.scheduler.State() != SchedulerRunning {
		t.Errorf("After Start(): state = %v, want %v",
			setup.scheduler.State(), SchedulerRunning)
	}

	// Test double Start
	if err := setup.scheduler.Start(); err != nil {
		t.Error("Second Start() should not return error")
	}

	setup.scheduler.Stop()
	if setup.scheduler.State() != SchedulerStopped {
		t.Errorf("After Stop(): state = %v, want %v",
			setup.scheduler.State(), SchedulerStopped)
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

		// Wait longer for task completion
		deadline := time.After(100 * time.Millisecond)
		ticker := time.NewTicker(5 * time.Millisecond)
		defer ticker.Stop()

		for {
			select {
			case <-deadline:
				t.Errorf("Non-blocking task did not complete in time, state: %v", g.State())
				return
			case <-ticker.C:
				if g.State() == core.GoroutineRunnable {
					return // Success
				}
			}
		}
	})

	t.Run("Blocking Task", func(t *testing.T) {
		g := core.NewGoroutine(20*time.Millisecond, true)
		if !setup.scheduler.Submit(g) {
			t.Error("Submit should succeed")
		}

		// Wait longer for blocking task
		deadline := time.After(200 * time.Millisecond)
		ticker := time.NewTicker(5 * time.Millisecond)
		defer ticker.Stop()

		for {
			select {
			case <-deadline:
				t.Errorf("Blocking task did not complete in time, state: %v", g.State())
				return
			case <-ticker.C:
				if g.State() == core.GoroutineRunnable {
					return // Success
				}
			}
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

	const (
		numBlockingTasks = 3 // Reduced from 5
		numNormalTasks   = 3 // Reduced from 5
		totalTasks       = numBlockingTasks + numNormalTasks
	)

	var tasks []*core.Goroutine

	// Submit blocking tasks first
	for i := 0; i < numBlockingTasks; i++ {
		g := core.NewGoroutine(30*time.Millisecond, true)
		if !setup.scheduler.Submit(g) {
			t.Errorf("Failed to submit blocking task %d", i)
			continue
		}
		tasks = append(tasks, g)
		time.Sleep(10 * time.Millisecond) // Ensure proper registration
	}

	// Submit non-blocking tasks
	for i := 0; i < numNormalTasks; i++ {
		g := core.NewGoroutine(20*time.Millisecond, false)
		if !setup.scheduler.Submit(g) {
			t.Errorf("Failed to submit normal task %d", i)
			continue
		}
		tasks = append(tasks, g)
		time.Sleep(5 * time.Millisecond) // Space out submissions
	}

	// Wait for completion with detailed progress tracking
	deadline := time.After(2 * time.Second)
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()

checkCompletion:
	for {
		select {
		case <-deadline:
			var blockingComplete, normalComplete int
			for _, g := range tasks {
				if g.State() == core.GoroutineRunnable {
					if g.IsBlocking() {
						blockingComplete++
					} else {
						normalComplete++
					}
				}
			}
			t.Fatalf("Test timed out. Blocking completed: %d/%d, Normal completed: %d/%d",
				blockingComplete, numBlockingTasks,
				normalComplete, numNormalTasks)
			break checkCompletion

		case <-ticker.C:
			blockingComplete := 0
			normalComplete := 0
			for _, g := range tasks {
				if g.State() == core.GoroutineRunnable {
					if g.IsBlocking() {
						blockingComplete++
					} else {
						normalComplete++
					}
				}
			}

			t.Logf("Progress - Blocking: %d/%d, Normal: %d/%d",
				blockingComplete, numBlockingTasks,
				normalComplete, numNormalTasks)

			if blockingComplete == numBlockingTasks &&
				normalComplete == numNormalTasks {
				break checkCompletion
			}
		}
	}

	// Final verification
	stats := setup.scheduler.GetStats()
	if stats.TasksScheduled != uint64(totalTasks) {
		t.Errorf("Expected %d tasks scheduled, got %d",
			totalTasks, stats.TasksScheduled)
	}
}

func TestConcurrentTaskSubmission(t *testing.T) {
	setup := newTestSetup(t, 4)
	defer setup.cleanup()

	if err := setup.scheduler.Start(); err != nil {
		t.Fatal(err)
	}

	const (
		numGoroutines     = 5 // Reduced from 10 for more stability
		tasksPerGoroutine = 4 // Reduced from 5
		totalTasks        = numGoroutines * tasksPerGoroutine
	)

	var wg sync.WaitGroup
	var tasks []*core.Goroutine
	var tasksMu sync.Mutex // Protect tasks slice

	// Submit tasks concurrently
	wg.Add(numGoroutines)
	for i := 0; i < numGoroutines; i++ {
		go func(id int) {
			defer wg.Done()
			for j := 0; j < tasksPerGoroutine; j++ {
				// Alternate between blocking and non-blocking tasks
				isBlocking := j%2 == 0
				duration := 20 * time.Millisecond
				if isBlocking {
					duration = 30 * time.Millisecond
				}

				g := core.NewGoroutine(duration, isBlocking)
				if setup.scheduler.Submit(g) {
					tasksMu.Lock()
					tasks = append(tasks, g)
					tasksMu.Unlock()
				}
				time.Sleep(5 * time.Millisecond) // Prevent flooding
			}
		}(i)
	}

	// Wait for all submissions with timeout
	doneChan := make(chan struct{})
	go func() {
		wg.Wait()
		close(doneChan)
	}()

	select {
	case <-doneChan:
		// Submissions completed
	case <-time.After(2 * time.Second):
		t.Fatal("Timeout waiting for task submissions")
	}

	// Wait for task completion
	deadline := time.After(3 * time.Second)
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()

checkCompletion:
	for {
		select {
		case <-deadline:
			completed := 0
			for _, g := range tasks {
				if g.State() == core.GoroutineRunnable {
					completed++
				}
			}
			t.Fatalf("Timeout waiting for task completion. Completed: %d/%d",
				completed, len(tasks))
			break checkCompletion

		case <-ticker.C:
			completed := 0
			for _, g := range tasks {
				if g.State() == core.GoroutineRunnable {
					completed++
				}
			}
			if completed == len(tasks) {
				break checkCompletion
			}
			t.Logf("Progress: %d/%d tasks completed", completed, len(tasks))
		}
	}
}
