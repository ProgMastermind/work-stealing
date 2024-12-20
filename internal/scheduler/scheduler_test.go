package scheduler

import (
	"math/rand"
	"sync"
	"sync/atomic"
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

func waitForTaskCompletion(t *testing.T, g *core.Goroutine, timeout time.Duration) bool {
	t.Helper()
	deadline := time.After(timeout)
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-deadline:
			t.Logf("Task %d timed out in state: %v", g.ID(), g.State())
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
			for _, g := range tasks {
				t.Logf("Task %d state: %v", g.ID(), g.State())
			}
			return false
		case <-ticker.C:
			completed := 0
			for _, g := range tasks {
				state := g.State()
				if state == core.GoroutineFinished {
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
			queueSize:     0,
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
				state := g.State()
				if state == core.GoroutineFinished || state == core.GoroutineRunnable {
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

		deadline := time.After(500 * time.Millisecond)
		ticker := time.NewTicker(5 * time.Millisecond)
		defer ticker.Stop()

		for {
			select {
			case <-deadline:
				t.Errorf("Blocking task did not complete in time, state: %v", g.State())
				return
			case <-ticker.C:
				state := g.State()
				if state == core.GoroutineFinished {
					return // Success
				}
				t.Logf("Current state: %v", state)
			}
		}
	})

	t.Run("Nil Task", func(t *testing.T) {
		if setup.scheduler.Submit(nil) {
			t.Error("Nil submission should fail")
		}
	})
}

// Add new test for load distribution
func TestSchedulerLoadDistribution(t *testing.T) {
	setup := newTestSetup(t, 4)
	defer setup.cleanup()

	if err := setup.scheduler.Start(); err != nil {
		t.Fatal(err)
	}

	const tasksPerProcessor = 10
	totalTasks := len(setup.scheduler.processors) * tasksPerProcessor
	tasks := make([]*core.Goroutine, totalTasks)

	// Submit tasks with varying durations
	for i := 0; i < totalTasks; i++ {
		duration := time.Duration(20+i*5) * time.Millisecond
		tasks[i] = core.NewGoroutine(duration, i%2 == 0)
		if !setup.scheduler.Submit(tasks[i]) {
			t.Fatalf("Failed to submit task %d", i)
		}
	}

	if !waitForTasks(t, tasks, 5*time.Second) {
		t.Fatal("Tasks did not complete in time")
	}

	// Verify load distribution
	stats := setup.scheduler.GetStats()
	// Verify load distribution across processors
	processorLoads := make([]uint64, len(setup.scheduler.processors))
	for i := range setup.scheduler.processors {
		processorLoads[i] = stats.ProcessorMetrics[i].TasksExecuted
	}

	// Check for reasonable load distribution
	var minLoad, maxLoad uint64
	minLoad = ^uint64(0) // Set to max possible value
	maxLoad = 0

	for _, load := range processorLoads {
		if load < minLoad {
			minLoad = load
		}
		if load > maxLoad {
			maxLoad = load
		}
	}

	// Verify that load difference is not too extreme
	if minLoad < uint64(tasksPerProcessor/2) {
		t.Errorf("Some processors underutilized. Min load: %d, expected at least: %d",
			minLoad, tasksPerProcessor/2)
	}

	loadDiff := maxLoad - minLoad
	if loadDiff > uint64(tasksPerProcessor) {
		t.Errorf("Load imbalance too high. Difference between max and min: %d", loadDiff)
	}
}

// Add test for work stealing
func TestSchedulerWorkStealing(t *testing.T) {
	setup := newTestSetup(t, 4)
	defer setup.cleanup()

	if err := setup.scheduler.Start(); err != nil {
		t.Fatal(err)
	}

	// Create imbalanced load
	longTasks := make([]*core.Goroutine, 5)
	shortTasks := make([]*core.Goroutine, 15)

	// Submit long tasks to first processor
	for i := range longTasks {
		longTasks[i] = core.NewGoroutine(100*time.Millisecond, true)
		setup.scheduler.processors[0].Push(longTasks[i]) // Direct push to processor
	}

	// Submit short tasks
	for i := range shortTasks {
		shortTasks[i] = core.NewGoroutine(20*time.Millisecond, false)
		setup.scheduler.Submit(shortTasks[i])
	}

	allTasks := append(longTasks, shortTasks...)
	if !waitForTasks(t, allTasks, 5*time.Second) {
		t.Fatal("Work stealing didn't complete tasks in time")
	}
}

// Add test for processor state transitions
func TestProcessorStateTransitions(t *testing.T) {
	setup := newTestSetup(t, 2)
	defer setup.cleanup()

	// Test transitions before start
	for i := range setup.scheduler.processors {
		if setup.scheduler.processors[i].State() != core.ProcessorIdle { // Changed from ProcessorIdle to core.ProcessorIdle
			t.Errorf("Processor %d should be idle before start", i)
		}
	}

	if err := setup.scheduler.Start(); err != nil {
		t.Fatal(err)
	}

	// Test transitions during operation
	task := core.NewGoroutine(50*time.Millisecond, true)
	if !setup.scheduler.Submit(task) {
		t.Fatal("Failed to submit task")
	}

	time.Sleep(50 * time.Millisecond) // Give more time for state transition

	// Check processor states multiple times
	for attempt := 0; attempt < 5; attempt++ {
		running := false
		for _, p := range setup.scheduler.processors {
			state := p.State()
			if state == core.ProcessorRunning {
				running = true
				break
			}
			t.Logf("Processor state: %v", state)
		}
		if running {
			return // Success
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Error("No processor transitioned to running state")
}

// Add new test for processor interaction
func TestProcessorInteraction(t *testing.T) {
	setup := newTestSetup(t, 4)
	defer setup.cleanup()

	if err := setup.scheduler.Start(); err != nil {
		t.Fatal(err)
	}

	// Test processor-to-processor stealing
	t.Run("Processor Work Stealing", func(t *testing.T) {
		// Load one processor heavily
		heavyProc := setup.scheduler.processors[0]
		for i := 0; i < 10; i++ {
			g := core.NewGoroutine(50*time.Millisecond, false)
			heavyProc.Push(g)
		}

		// Submit some quick tasks to other processors
		for i := 1; i < len(setup.scheduler.processors); i++ {
			g := core.NewGoroutine(10*time.Millisecond, false)
			setup.scheduler.processors[i].Push(g)
		}

		// Allow time for work stealing to occur
		time.Sleep(100 * time.Millisecond)

		// Verify work distribution
		maxTasks := 0
		minTasks := int(^uint(0) >> 1)

		for _, p := range setup.scheduler.processors {
			tasks := p.QueueSize()
			if tasks > maxTasks {
				maxTasks = tasks
			}
			if tasks < minTasks {
				minTasks = tasks
			}
		}

		// Check if work was reasonably balanced
		if maxTasks-minTasks > 5 {
			t.Errorf("Work not balanced: max=%d, min=%d", maxTasks, minTasks)
		}
	})
}

// Add test for blocking task handling
func TestBlockingTaskHandling(t *testing.T) {
	setup := newTestSetup(t, 2)
	defer setup.cleanup()

	if err := setup.scheduler.Start(); err != nil {
		t.Fatal(err)
	}

	t.Run("Network Poller Integration", func(t *testing.T) {
		const numTasks = 5
		tasks := make([]*core.Goroutine, numTasks)

		// Submit blocking tasks
		for i := 0; i < numTasks; i++ {
			tasks[i] = core.NewGoroutine(30*time.Millisecond, true)
			if !setup.scheduler.Submit(tasks[i]) {
				t.Fatalf("Failed to submit blocking task %d", i)
			}
		}

		// Wait for poller to handle tasks
		time.Sleep(100 * time.Millisecond)

		// Verify poller metrics
		stats := setup.scheduler.GetStats()
		pollerMetrics := stats.PollerMetrics

		// Check both currently blocked and total events
		if pollerMetrics.CurrentlyBlocked == 0 && pollerMetrics.TotalEvents == 0 {
			t.Error("No tasks registered with poller")
		}

		// Increased timeout for completion
		if !waitForTasks(t, tasks, 5*time.Second) {
			var states []string
			for _, task := range tasks {
				states = append(states, task.State().String())
			}
			t.Fatalf("Blocking tasks not completed in time. States: %v", states)
		}
	})
}

// Add test for global queue operations
func TestGlobalQueueOperations(t *testing.T) {
	setup := newTestSetup(t, 2)
	defer setup.cleanup()

	if err := setup.scheduler.Start(); err != nil {
		t.Fatal(err)
	}

	t.Run("Global Queue Overflow Prevention", func(t *testing.T) {
		submitted := 0
		rejected := 0

		// Try to overflow global queue
		for i := 0; i < 2000; i++ {
			g := core.NewGoroutine(10*time.Millisecond, false)
			if setup.scheduler.Submit(g) {
				submitted++
			} else {
				rejected++
			}
		}

		stats := setup.scheduler.GetStats()
		queueStats := stats.GlobalQueueStats

		if queueStats.Rejected == 0 {
			t.Error("Queue overflow prevention not working")
		}

		t.Logf("Submitted: %d, Rejected: %d, Queue Size: %d",
			submitted, rejected, queueStats.CurrentSize)
	})
}

// Add stress test for scheduler
func TestSchedulerStress(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping stress test in short mode")
	}

	setup := newTestSetup(t, 8)
	defer setup.cleanup()

	if err := setup.scheduler.Start(); err != nil {
		t.Fatal(err)
	}

	const (
		numWorkers     = 10
		tasksPerWorker = 100
		totalDuration  = 5 * time.Second
	)

	var (
		wg           sync.WaitGroup
		successCount atomic.Int32
		failureCount atomic.Int32
	)

	deadline := time.After(totalDuration)
	start := time.Now()

	wg.Add(numWorkers)
	for i := 0; i < numWorkers; i++ {
		go func(workerID int) {
			defer wg.Done()
			for j := 0; j < tasksPerWorker; j++ {
				select {
				case <-deadline:
					return
				default:
					duration := time.Duration(rand.Intn(50)+10) * time.Millisecond
					g := core.NewGoroutine(duration, rand.Float32() < 0.3)
					if setup.scheduler.Submit(g) {
						successCount.Add(1)
					} else {
						failureCount.Add(1)
					}
					time.Sleep(time.Duration(rand.Intn(10)) * time.Millisecond)
				}
			}
		}(i)
	}

	wg.Wait()

	stats := setup.scheduler.GetStats()
	t.Logf("Stress test results (duration: %v):", time.Since(start))
	t.Logf("  Tasks submitted: %d", successCount.Load())
	t.Logf("  Tasks rejected: %d", failureCount.Load())
	t.Logf("  Tasks completed: %d", stats.TasksCompleted)
	t.Logf("  Work stealing attempts: %d", stats.TotalSteals)
}
