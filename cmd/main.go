package main

import (
	"fmt"
	"log"
	"time"
	"workstealing/internal/core"
	"workstealing/internal/scheduler"
	"workstealing/internal/visualization"
)

func main() {
	// Create scheduler with 4 processors
	s := scheduler.NewScheduler(4, 1000)
	if s == nil {
		log.Fatal("Failed to create scheduler")
	}

	// Create visualizer
	viz, err := visualization.NewTerminalVisualizer(s, 100*time.Millisecond)
	if err != nil {
		log.Fatalf("Failed to create visualizer: %v", err)
	}

	// Start components
	if err := s.Start(); err != nil {
		log.Fatalf("Failed to start scheduler: %v", err)
	}
	defer s.Stop()

	// Start visualization
	go viz.Start()
	defer viz.Stop()

	// Create 20 goroutines of different types:
	// - 10 CPU-bound tasks (non-blocking)
	// - 5 I/O-bound tasks (blocking)
	// - 5 mixed tasks (varying workloads)

	// CPU-bound tasks
	for i := 0; i < 500; i++ {
		g := core.NewGoroutine(
			100*time.Millisecond, // CPU work
			false,                // non-blocking
		)
		if !s.Submit(g) {
			log.Printf("Failed to submit CPU task %d", i)
		}
		viz.AddEvent(fmt.Sprintf("Submitted CPU task G%d", g.ID()))
		time.Sleep(10 * time.Millisecond) // Space out submissions
	}

	// I/O-bound tasks
	for i := 0; i < 250; i++ {
		g := core.NewGoroutine(
			150*time.Millisecond, // I/O operation time
			true,                 // blocking
		)
		if !s.Submit(g) {
			log.Printf("Failed to submit I/O task %d", i)
		}
		viz.AddEvent(fmt.Sprintf("Submitted I/O task G%d", g.ID()))
		time.Sleep(20 * time.Millisecond)
	}

	// Mixed workload tasks
	workloads := []struct {
		duration time.Duration
		blocking bool
	}{
		{50 * time.Millisecond, false},
		{200 * time.Millisecond, true},
		{75 * time.Millisecond, false},
		{125 * time.Millisecond, true},
		{150 * time.Millisecond, false},
	}

	for i, w := range workloads {
		g := core.NewGoroutine(w.duration, w.blocking)
		if !s.Submit(g) {
			log.Printf("Failed to submit mixed task %d", i)
		}
		taskType := "CPU/IO"
		if w.blocking {
			taskType = "I/O"
		} else {
			taskType = "CPU"
		}
		viz.AddEvent(fmt.Sprintf("Submitted %s task G%d (%v)", taskType, g.ID(), w.duration))
		time.Sleep(15 * time.Millisecond)
	}

	// Keep the visualization running
	fmt.Println("Press 'q' to exit")
	time.Sleep(10 * time.Second) // Allow time to observe system behavior
}
