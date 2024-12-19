package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"runtime"
	"sync/atomic"
	"syscall"
	"time"

	"workstealing/internal/core"
	"workstealing/internal/scheduler"
	"workstealing/internal/visualization"
)

var (
	numProcessors  = flag.Int("p", runtime.NumCPU(), "Number of processors")
	queueSize      = flag.Int("q", 2000, "Global queue size")
	totalTasks     = flag.Int("t", 2500, "Total number of tasks to process")
	updateInterval = flag.Duration("i", 100*time.Millisecond, "Visualization update interval")
	batchSize      = flag.Int("b", 5, "Batch size for task submission")
)

func main() {
	flag.Parse()

	// Initialize scheduler
	s := scheduler.NewScheduler(*numProcessors, int32(*queueSize))
	if s == nil {
		log.Fatal("Failed to create scheduler")
	}

	// Initialize visualizer
	vis, err := visualization.NewTerminalVisualizer(s, *updateInterval)
	if err != nil {
		log.Fatalf("Failed to initialize visualizer: %v", err)
	}

	// Channels for completion and tracking
	done := make(chan struct{})
	var (
		completedTasks uint64
		submittedTasks uint64
	)

	// Start scheduler
	if err := s.Start(); err != nil {
		log.Fatalf("Failed to start scheduler: %v", err)
	}

	// Start visualization in background
	go vis.Start()

	// Generate workload in background
	go generateWorkload(s, *totalTasks, *batchSize, vis, &completedTasks, &submittedTasks, done)

	// Wait for interrupt or completion
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	select {
	case <-sigChan:
		// Handle interrupt
		vis.Stop()
		s.Stop()
	case <-done:
		// Work completed, show final statistics
		time.Sleep(500 * time.Millisecond) // Allow final updates
		vis.Stop()                         // Stop the UI first
		fmt.Println("\033[H\033[2J")       // Clear the screen
		showFinalStats(s)
		s.Stop()
	}
}

func generateWorkload(s *scheduler.Scheduler, totalTasks, batchSize int,
	vis *visualization.TerminalVisualizer,
	completed, submitted *uint64,
	done chan struct{}) {

	taskID := 0
	rejectedCount := 0
	startTime := time.Now()
	// blockcount := 0
	// Submit all tasks
	for taskID < totalTasks {
		for i := 0; i < batchSize && taskID < totalTasks; i++ {
			taskID++
			isBlocking := taskID%3 == 0
			// if isBlocking {
			// 	blockcount++
			// }
			workload := time.Duration(50+taskID%100) * time.Millisecond

			g := core.NewGoroutine(workload, isBlocking)
			if !s.Submit(g) {
				rejectedCount++
				vis.AddEvent(fmt.Sprintf("Task G%d rejected: Queue full", taskID))
			} else {
				atomic.AddUint64(submitted, 1)
				if isBlocking {
					vis.AddEvent(fmt.Sprintf("Submitted blocking task G%d", taskID))
				}
			}

		}
		time.Sleep(10 * time.Millisecond)
	}

	submissionTime := time.Since(startTime)
	vis.AddEvent(fmt.Sprintf("All tasks submitted in %v. Waiting for completion...",
		submissionTime.Round(time.Millisecond)))

	// Wait for all non-rejected tasks to complete
	expectedComplete := uint64(totalTasks - rejectedCount)
	lastProgress := uint64(0)

	for {
		stats := s.GetStats()
		currentCompleted := stats.TasksCompleted
		atomic.StoreUint64(completed, currentCompleted)

		// Check if all tasks are completed
		if currentCompleted >= expectedComplete {
			vis.AddEvent(fmt.Sprintf("All %d tasks completed!", expectedComplete))
			done <- struct{}{}
			break
		}

		// Show progress updates for every 100 completed tasks
		if currentCompleted/100 > lastProgress/100 {
			vis.AddEvent(fmt.Sprintf("Progress: %d/%d tasks completed (%.1f%%)",
				currentCompleted, expectedComplete,
				float64(currentCompleted)/float64(expectedComplete)*100))
			lastProgress = currentCompleted
		}

		time.Sleep(100 * time.Millisecond)
	}

	close(done)
}

func showFinalStats(s *scheduler.Scheduler) {
	stats := s.GetStats()

	fmt.Println("========================================")
	fmt.Println("          Final Statistics              ")
	fmt.Println("========================================")

	fmt.Printf("\nOverall Performance:\n")
	fmt.Printf("--------------------\n")
	fmt.Printf("Total Tasks Scheduled: %d\n", stats.TasksScheduled)
	fmt.Printf("Total Tasks Completed: %d\n", stats.TasksCompleted)
	fmt.Printf("Tasks Still In Progress: %d\n",
		stats.TasksScheduled-stats.TasksCompleted-stats.GlobalQueueStats.Rejected)
	fmt.Printf("Total Tasks Rejected: %d\n", stats.GlobalQueueStats.Rejected)
	fmt.Printf("Total Execution Time: %v\n", stats.RunningTime.Round(time.Millisecond))
	fmt.Printf("Total Steals: %d (Global: %d, Local: %d)\n\n",
		stats.TotalSteals, stats.GlobalQueueSteals, stats.LocalQueueSteals)

	fmt.Println("Processor Statistics:")
	fmt.Println("--------------------")
	for _, p := range stats.ProcessorMetrics {
		fmt.Printf("Processor P%d:\n", p.ID)
		fmt.Printf("  Tasks Executed: %d\n", p.TasksExecuted)
		fmt.Printf("  Steals: Local=%d, Global=%d\n", p.LocalSteals, p.GlobalSteals)
		fmt.Printf("  Idle Time: %v\n", p.IdleTime.Round(time.Millisecond))
		fmt.Printf("  Running Time: %v\n\n", p.RunningTime.Round(time.Millisecond))
	}

	fmt.Println("Network Poller Statistics:")
	fmt.Println("-------------------------")
	fmt.Printf("Total Events Handled: %d\n", stats.PollerMetrics.TotalEvents)
	fmt.Printf("Successfully Completed: %d\n", stats.PollerMetrics.CompletedEvents)
	fmt.Printf("Timeouts: %d\n", stats.PollerMetrics.Timeouts)
	fmt.Printf("Errors: %d\n", stats.PollerMetrics.Errors)
	fmt.Printf("Average Block Time: %v\n\n",
		stats.PollerMetrics.AverageBlockTime.Round(time.Millisecond))

	fmt.Println("Global Queue Statistics:")
	fmt.Println("----------------------")
	fmt.Printf("Total Submitted: %d\n", stats.GlobalQueueStats.Submitted)
	fmt.Printf("Total Executed: %d\n", stats.GlobalQueueStats.Executed)
	fmt.Printf("Total Rejected: %d\n", stats.GlobalQueueStats.Rejected)
	fmt.Printf("Total Stolen: %d\n\n", stats.GlobalQueueStats.Stolen)

	fmt.Printf("Performance Metrics:\n")
	fmt.Printf("-------------------\n")
	throughput := float64(stats.TasksCompleted) / stats.RunningTime.Seconds()
	fmt.Printf("Average Throughput: %.2f tasks/second\n", throughput)
	if stats.TasksScheduled > 0 {
		successRate := float64(stats.TasksCompleted) / float64(stats.TasksScheduled) * 100
		fmt.Printf("Task Success Rate: %.2f%%\n\n", successRate)
	}

	fmt.Println("========================================")
	fmt.Println("Press Enter to exit...")
	fmt.Scanln() // Wait for user input before exiting
}
