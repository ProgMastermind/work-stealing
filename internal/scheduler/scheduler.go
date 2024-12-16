package scheduler

import (
	"context"
	"math/rand"
	"sync"
	"sync/atomic"
	"time"
	"workstealing/internal/core"
	"workstealing/internal/queue"
)

// SchedulerState represents the current state of the scheduler
type SchedulerState int32

const (
	SchedulerStopped SchedulerState = iota
	SchedulerRunning
	SchedulerStopping
)

// SchedulerStats holds the current statistics of the scheduler
type SchedulerStats struct {
	TasksScheduled    uint64
	TasksCompleted    uint64
	TotalSteals       uint64
	GlobalQueueSteals uint64
	LocalQueueSteals  uint64
	RunningTime       time.Duration
	ProcessorMetrics  []core.ProcessorMetricsData
	GlobalQueueStats  queue.QueueStats
}

// Scheduler manages the distribution and execution of tasks
type Scheduler struct {
	state         atomic.Int32
	globalQueue   *queue.GlobalQueue
	processors    []*core.Processor
	numProcessors int32

	// Stealing strategy
	stealAttempts  int           // Number of attempts before giving up
	stealInterval  time.Duration // Time between steal attempts
	stealThreshold float64       // Load threshold that triggers stealing

	// Random source for work distribution
	rand *rand.Rand

	// Metrics
	metrics struct {
		tasksScheduled    atomic.Uint64
		tasksCompleted    atomic.Uint64
		totalSteals       atomic.Uint64
		globalQueueSteals atomic.Uint64
		localQueueSteals  atomic.Uint64
		startTime         time.Time
	}

	// Synchronization
	mu     sync.RWMutex
	wg     sync.WaitGroup
	ctx    context.Context
	cancel context.CancelFunc
}

// NewScheduler creates a new scheduler instance
func NewScheduler(numProcessors int32, globalQueueSize int32) *Scheduler {
	ctx, cancel := context.WithCancel(context.Background())

	s := &Scheduler{
		globalQueue:    queue.NewGlobalQueue(globalQueueSize),
		processors:     make([]*core.Processor, numProcessors),
		numProcessors:  numProcessors,
		ctx:            ctx,
		cancel:         cancel,
		stealAttempts:  3,
		stealInterval:  time.Millisecond,
		stealThreshold: 0.3, // Steal when local queue is less than 30% utilized
		rand:           rand.New(rand.NewSource(time.Now().UnixNano())),
	}

	// Initialize processors
	for i := int32(0); i < numProcessors; i++ {
		s.processors[i] = core.NewProcessor(uint32(i), 100)
	}

	s.state.Store(int32(SchedulerStopped))
	return s
}

// Start begins the scheduling process
func (s *Scheduler) Start() error {
	if !s.state.CompareAndSwap(int32(SchedulerStopped), int32(SchedulerRunning)) {
		return nil // Already running
	}

	s.metrics.startTime = time.Now()

	// Start processor workers
	for i := range s.processors {
		s.wg.Add(1)
		go s.runProcessor(s.processors[i])
	}

	// Start global queue distributor
	s.wg.Add(1)
	go s.distributeWork()

	return nil
}

// Stop gracefully stops the scheduler
func (s *Scheduler) Stop() {
	if !s.state.CompareAndSwap(int32(SchedulerRunning), int32(SchedulerStopping)) {
		return
	}

	s.cancel()
	s.wg.Wait()
	s.state.Store(int32(SchedulerStopped))
}

// Submit adds a new task to be scheduled
func (s *Scheduler) Submit(g *core.Goroutine) bool {
	if s.state.Load() != int32(SchedulerRunning) {
		return false
	}

	// Track submission success
	submitted := false

	// Randomly choose between direct processor assignment and global queue
	if s.rand.Float64() < 0.3 { // 30% chance of direct assignment
		processor := s.processors[s.rand.Intn(len(s.processors))]
		g.SetSource(core.SourceLocalQueue, 0)
		if processor.Push(g) {
			submitted = true
		}
	}

	// Try global queue if direct assignment fails or wasn't chosen
	if !submitted {
		g.SetSource(core.SourceGlobalQueue, 0)
		submitted = s.globalQueue.Submit(g)
	}

	if submitted {
		s.metrics.tasksScheduled.Add(1)
		return true
	}

	return false
}

// distributeWork handles global queue distribution
func (s *Scheduler) distributeWork() {
	defer s.wg.Done()

	ticker := time.NewTicker(s.stealInterval)
	defer ticker.Stop()

	for {
		select {
		case <-s.ctx.Done():
			return
		case <-ticker.C:
			s.balanceLoad()
		}
	}
}

// balanceLoad distributes work from global queue
func (s *Scheduler) balanceLoad() {
	if s.globalQueue.IsEmpty() {
		return
	}

	// Pick a random processor
	procIndex := s.rand.Intn(len(s.processors))
	processor := s.processors[procIndex]

	if tasks := s.globalQueue.Fetch(5); len(tasks) > 0 {
		for _, task := range tasks {
			if processor.Push(task) {
				s.metrics.globalQueueSteals.Add(1)
			}
		}
	}
}

// runProcessor manages execution of tasks on a processor
func (s *Scheduler) runProcessor(p *core.Processor) {
	defer s.wg.Done()

	for {
		select {
		case <-s.ctx.Done():
			return
		default:
			s.processWork(p)
		}
	}
}

// processWork handles task execution and work stealing
func (s *Scheduler) processWork(p *core.Processor) {
	// Try local queue first
	if g := p.Pop(); g != nil {
		s.executeTask(p, g)
		return
	}

	// Try stealing if local queue is empty
	if stolen := s.trySteal(p); len(stolen) > 0 {
		for _, g := range stolen {
			p.Push(g)
		}
		return
	}

	// If no work found, sleep briefly
	time.Sleep(s.stealInterval)
}

// trySteal attempts to steal work for a processor
func (s *Scheduler) trySteal(thief *core.Processor) []*core.Goroutine {
	// First try global queue
	if tasks := s.globalQueue.TrySteal(3); len(tasks) > 0 {
		s.metrics.globalQueueSteals.Add(1)
		s.metrics.totalSteals.Add(1)
		return tasks
	}

	// Then try stealing from other processors
	attempts := s.stealAttempts
	indices := s.randomProcessorIndices(thief.ID())

	for i := 0; i < attempts && i < len(indices); i++ {
		victim := s.processors[indices[i]]
		if stolen := thief.Steal(victim); len(stolen) > 0 {
			s.metrics.localQueueSteals.Add(1)
			s.metrics.totalSteals.Add(1)
			return stolen
		}
	}

	return nil
}

// randomProcessorIndices returns a shuffled slice of processor indices excluding the thief
func (s *Scheduler) randomProcessorIndices(thiefID uint32) []int {
	indices := make([]int, len(s.processors)-1)
	idx := 0
	for i := range s.processors {
		if uint32(i) != thiefID {
			indices[idx] = i
			idx++
		}
	}

	// Fisher-Yates shuffle
	for i := len(indices) - 1; i > 0; i-- {
		j := s.rand.Intn(i + 1)
		indices[i], indices[j] = indices[j], indices[i]
	}

	return indices
}

// executeTask runs a goroutine
func (s *Scheduler) executeTask(p *core.Processor, g *core.Goroutine) {
	p.SetState(core.ProcessorRunning)
	g.Start()

	// Execute the task
	g.Finish(nil, nil)

	p.SetState(core.ProcessorIdle)
	p.IncrementTasksExecuted()
	s.metrics.tasksCompleted.Add(1)
}

// GetStats returns current scheduler statistics
func (s *Scheduler) GetStats() SchedulerStats {
	s.mu.RLock()
	defer s.mu.RUnlock()

	stats := SchedulerStats{
		TasksScheduled:    s.metrics.tasksScheduled.Load(),
		TasksCompleted:    s.metrics.tasksCompleted.Load(),
		TotalSteals:       s.metrics.totalSteals.Load(),
		GlobalQueueSteals: s.metrics.globalQueueSteals.Load(),
		LocalQueueSteals:  s.metrics.localQueueSteals.Load(),
		RunningTime:       time.Since(s.metrics.startTime),
		ProcessorMetrics:  make([]core.ProcessorMetricsData, len(s.processors)),
		GlobalQueueStats:  s.globalQueue.Stats(),
	}

	for i, p := range s.processors {
		stats.ProcessorMetrics[i] = p.GetMetrics()
	}

	return stats
}
