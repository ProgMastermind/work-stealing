package scheduler

import (
	"sync"
	"testing"
	"time"
	"workstealing/internal/core"
)

func TestNewScheduler(t *testing.T) {
	s := NewScheduler(4, 1000)

	if s.numProcessors != 4 {
		t.Errorf("Expected 4 processors, got %d", s.numProcessors)
	}

	if len(s.processors) != 4 {
		t.Errorf("Expected 4 processors initialized, got %d", len(s.processors))
	}

	if s.state.Load() != int32(SchedulerStopped) {
		t.Error("Expected scheduler to be in stopped state initially")
	}

	if s.rand == nil {
		t.Error("Random source should be initialized")
	}
}

func TestSchedulerStartStop(t *testing.T) {
	s := NewScheduler(4, 1000)

	// Test starting
	if err := s.Start(); err != nil {
		t.Errorf("Failed to start scheduler: %v", err)
	}

	if s.state.Load() != int32(SchedulerRunning) {
		t.Error("Expected scheduler to be in running state")
	}

	// Test double start
	if err := s.Start(); err != nil {
		t.Error("Second start should not return error")
	}

	// Test stopping
	s.Stop()

	if s.state.Load() != int32(SchedulerStopped) {
		t.Error("Expected scheduler to be in stopped state after stop")
	}
}

func TestTaskSubmissionAndExecution(t *testing.T) {
	s := NewScheduler(2, 1000)
	s.Start()
	defer s.Stop()

	numTasks := 100
	var wg sync.WaitGroup
	wg.Add(numTasks)

	// Submit tasks
	for i := 0; i < numTasks; i++ {
		g := core.NewGoroutine(10*time.Millisecond, false)
		if !s.Submit(g) {
			t.Errorf("Failed to submit task %d", i)
		}
	}

	// Allow time for execution
	time.Sleep(200 * time.Millisecond)

	// Check metrics
	stats := s.GetStats()
	if stats.TasksScheduled != uint64(numTasks) {
		t.Errorf("Expected %d tasks scheduled, got %d", numTasks, stats.TasksScheduled)
	}

	completedTasks := stats.TasksCompleted
	if completedTasks == 0 {
		t.Error("No tasks completed")
	}
}

func TestWorkStealing(t *testing.T) {
	s := NewScheduler(2, 1000)
	s.Start()
	defer s.Stop()

	// Create imbalance by submitting tasks
	numTasks := 20
	for i := 0; i < numTasks; i++ {
		g := core.NewGoroutine(5*time.Millisecond, false)
		s.Submit(g)
	}

	// Allow time for work stealing to occur
	time.Sleep(100 * time.Millisecond)

	// Verify steals occurred
	stats := s.GetStats()
	if stats.TotalSteals == 0 {
		t.Error("Expected some work stealing to occur")
	}

	t.Logf("Work stealing stats: Total=%d, Global=%d, Local=%d",
		stats.TotalSteals, stats.GlobalQueueSteals, stats.LocalQueueSteals)
}

func TestRandomDistribution(t *testing.T) {
	s := NewScheduler(4, 1000)
	s.Start()
	defer s.Stop()

	// Submit a large number of tasks
	numTasks := 1000
	for i := 0; i < numTasks; i++ {
		g := core.NewGoroutine(time.Millisecond, false)
		s.Submit(g)
	}

	// Allow time for distribution
	time.Sleep(50 * time.Millisecond)

	// Check distribution across processors
	processorLoads := make([]int, len(s.processors))
	for i, p := range s.processors {
		status := p.GetStatus()
		processorLoads[i] = status.QueueSize
	}

	// Calculate load variance
	var totalLoad int
	for _, load := range processorLoads {
		totalLoad += load
	}
	avgLoad := float64(totalLoad) / float64(len(processorLoads))

	// Check if load is reasonably distributed
	for i, load := range processorLoads {
		diff := float64(load) - avgLoad
		if diff > float64(avgLoad)*0.5 { // Allow 50% deviation
			t.Errorf("Processor %d load too high: %d (avg: %.2f)", i, load, avgLoad)
		}
	}
}

func TestSchedulerConcurrency(t *testing.T) {
	s := NewScheduler(4, 1000)
	s.Start()
	defer s.Stop()

	var wg sync.WaitGroup
	numOperations := 1000

	// Concurrent submissions
	for i := 0; i < numOperations; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			g := core.NewGoroutine(time.Millisecond, false)
			s.Submit(g)
		}()
	}

	// Concurrent stats checking
	for i := 0; i < numOperations/10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s.GetStats()
		}()
	}

	wg.Wait()
}

func TestSchedulerStress(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping stress test in short mode")
	}

	s := NewScheduler(8, 10000)
	s.Start()
	defer s.Stop()

	var wg sync.WaitGroup
	numTasks := 10000

	// Submit many tasks with varying workloads
	for i := 0; i < numTasks; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			workload := time.Duration(id%10) * time.Millisecond
			g := core.NewGoroutine(workload, false)
			s.Submit(g)
		}(i)
	}

	wg.Wait()
	time.Sleep(100 * time.Millisecond) // Allow for processing

	stats := s.GetStats()
	t.Logf("Stress test results: Scheduled=%d, Completed=%d, Steals=%d",
		stats.TasksScheduled, stats.TasksCompleted, stats.TotalSteals)
}

func TestSchedulerEdgeCases(t *testing.T) {
	s := NewScheduler(1, 10)

	// Test submission before start
	g := core.NewGoroutine(time.Millisecond, false)
	if s.Submit(g) {
		t.Error("Should not accept tasks before starting")
	}

	s.Start()

	// Test submission to full queue
	for i := 0; i < 15; i++ {
		g := core.NewGoroutine(time.Millisecond, false)
		s.Submit(g)
	}

	// Test stop while tasks are pending
	s.Stop()

	if s.Submit(g) {
		t.Error("Should not accept tasks after stopping")
	}
}

func BenchmarkSchedulerSubmission(b *testing.B) {
	s := NewScheduler(4, int32(b.N))
	s.Start()
	defer s.Stop()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		g := core.NewGoroutine(time.Microsecond, false)
		s.Submit(g)
	}
}

func BenchmarkSchedulerWorkStealing(b *testing.B) {
	s := NewScheduler(4, int32(b.N))
	s.Start()
	defer s.Stop()

	// Submit tasks to create stealing opportunities
	for i := 0; i < b.N; i++ {
		g := core.NewGoroutine(time.Microsecond, false)
		s.Submit(g)
	}

	b.ResetTimer()
	time.Sleep(time.Duration(b.N) * time.Microsecond)
}

func TestProcessorUtilization(t *testing.T) {
	s := NewScheduler(4, 1000)
	s.Start()
	defer s.Stop()

	// Submit a steady stream of tasks
	for i := 0; i < 100; i++ {
		g := core.NewGoroutine(time.Millisecond, false)
		s.Submit(g)
		time.Sleep(time.Millisecond)
	}

	// Check processor metrics
	stats := s.GetStats()
	for i, metrics := range stats.ProcessorMetrics {
		if metrics.TasksExecuted == 0 {
			t.Errorf("Processor %d has not executed any tasks", i)
		}
		t.Logf("Processor %d: Tasks=%d, Steals=%d, IdleTime=%v",
			i, metrics.TasksExecuted, metrics.StealsSuccessful, metrics.TotalIdleTime)
	}
}

func TestSchedulerMetricsAccuracy(t *testing.T) {
	s := NewScheduler(2, 1000)
	s.Start()
	defer s.Stop()

	numTasks := 50
	tasksSubmitted := 0
	for i := 0; i < numTasks; i++ {
		g := core.NewGoroutine(time.Millisecond, false)
		if s.Submit(g) {
			tasksSubmitted++
		}
	}

	time.Sleep(100 * time.Millisecond)
	stats := s.GetStats()

	// Verify metrics consistency
	totalExecuted := uint64(0)
	for _, pm := range stats.ProcessorMetrics {
		totalExecuted += pm.TasksExecuted
	}

	if totalExecuted != stats.TasksCompleted {
		t.Errorf("Inconsistent completion metrics: processor total=%d, scheduler total=%d",
			totalExecuted, stats.TasksCompleted)
	}

	// Check if total tasks accounted for matches submitted tasks
	directAssignments := stats.TasksScheduled - uint64(stats.GlobalQueueStats.Submitted)

	if uint64(tasksSubmitted) != stats.TasksScheduled {
		t.Errorf("Inconsistent task scheduling: submitted %d, scheduled %d",
			tasksSubmitted, stats.TasksScheduled)
	}

	t.Logf("Task Distribution:\n"+
		"Total Scheduled: %d\n"+
		"Global Queue Submissions: %d\n"+
		"Direct Assignments: %d\n"+
		"Rejected Tasks: %d",
		stats.TasksScheduled,
		stats.GlobalQueueStats.Submitted,
		directAssignments,
		stats.GlobalQueueStats.Rejected)
}
