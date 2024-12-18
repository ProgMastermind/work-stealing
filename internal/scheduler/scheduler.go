package scheduler

import (
	"context"
	"math/rand"
	"sync"
	"sync/atomic"
	"time"
	"workstealing/internal/core"
	"workstealing/internal/poller"
)

type SchedulerState int32

const (
    SchedulerStopped SchedulerState = iota
    SchedulerRunning
    SchedulerStopping
)

// SchedulerStats holds runtime statistics
type SchedulerStats struct {
    TasksScheduled    uint64
    TasksCompleted    uint64
    TotalSteals       uint64
    GlobalQueueSteals uint64
    LocalQueueSteals  uint64
    RunningTime       time.Duration
    ProcessorMetrics  []core.ProcessorStats
    GlobalQueueStats  core.GlobalQueueStats
    PollerMetrics     poller.PollerMetrics
}

// Scheduler manages task distribution and execution
type Scheduler struct {
    state         atomic.Int32
    globalQueue   *core.GlobalQueue
    processors    []*core.Processor
    networkPoller *poller.NetworkPoller

    // Task tracking
    activeTasksCount atomic.Int32
    blockingTasks    sync.Map // tracks blocking tasks by ID

    metrics struct {
        tasksScheduled    atomic.Uint64
        tasksCompleted    atomic.Uint64
        totalSteals       atomic.Uint64
        globalQueueSteals atomic.Uint64
        localQueueSteals  atomic.Uint64
        startTime         time.Time
    }

    mu      sync.RWMutex
    wg      sync.WaitGroup
    ctx     context.Context
    cancel  context.CancelFunc
    rand    *rand.Rand
}

func NewScheduler(numProcessors int, globalQueueSize int32) *Scheduler {
    if numProcessors <= 0 {
        numProcessors = 1
    }

    ctx, cancel := context.WithCancel(context.Background())

    globalQueue := core.NewGlobalQueue(globalQueueSize)
    if globalQueue == nil {
        cancel()
        return nil
    }

    s := &Scheduler{
        globalQueue: globalQueue,
        processors: make([]*core.Processor, numProcessors),
        ctx:        ctx,
        cancel:     cancel,
        rand:       rand.New(rand.NewSource(time.Now().UnixNano())),
    }

    // Initialize processors
    for i := 0; i < numProcessors; i++ {
        processor := core.NewProcessor(uint32(i), 100, globalQueue)
        if processor == nil {
            cancel()
            return nil
        }
        s.processors[i] = processor
    }

    // Set up processor relationships
    for _, p := range s.processors {
        p.SetProcessors(s.processors)
    }

    // Initialize network poller
    s.networkPoller = poller.NewNetworkPoller(s.processors)
    if s.networkPoller == nil {
        cancel()
        return nil
    }

    s.state.Store(int32(SchedulerStopped))
    return s
}

func (s *Scheduler) Start() error {
    if !s.state.CompareAndSwap(int32(SchedulerStopped), int32(SchedulerRunning)) {
        return nil
    }

    s.metrics.startTime = time.Now()
    s.networkPoller.Start()

    for i := range s.processors {
        s.wg.Add(1)
        go s.runProcessor(s.processors[i])
    }

    return nil
}

func (s *Scheduler) Stop() {
    if !s.state.CompareAndSwap(int32(SchedulerRunning), int32(SchedulerStopping)) {
        return
    }

    s.cancel()
    s.networkPoller.Stop()
    s.wg.Wait()
    s.state.Store(int32(SchedulerStopped))
}

func (s *Scheduler) Submit(g *core.Goroutine) bool {
    if s.state.Load() != int32(SchedulerRunning) || g == nil {
        return false
    }

    // Track task
    s.activeTasksCount.Add(1)
    s.metrics.tasksScheduled.Add(1)

    // Try direct processor assignment (80% probability)
    if s.rand.Float64() < 0.8 {
        processor := s.processors[s.rand.Intn(len(s.processors))]
        if processor.Push(g) {
            return true
        }
    }

    // Fall back to global queue
    return s.globalQueue.Submit(g)
}

func (s *Scheduler) runProcessor(p *core.Processor) {
    defer s.wg.Done()

    for {
        select {
        case <-s.ctx.Done():
            return
        default:
            if g := p.FindWork(); g != nil {
                s.processTask(p, g)
            } else {
                time.Sleep(time.Millisecond)
            }
        }
    }
}

func (s *Scheduler) processTask(p *core.Processor, g *core.Goroutine) {
    // Track steal metrics first
    if g.Source() == core.SourceStolen {
        s.metrics.totalSteals.Add(1)
        s.metrics.localQueueSteals.Add(1)
    } else if g.Source() == core.SourceGlobalQueue {
        s.metrics.globalQueueSteals.Add(1)
    }

    if g.IsBlocking() {
        // Check if task is already being handled
        if _, exists := s.blockingTasks.LoadOrStore(g.ID(), true); !exists {
            // Register with poller only if not already registered
            deadline := time.Now().Add(g.Workload())
            s.networkPoller.Register(g, poller.EventRead, deadline, p.ID())
        }
    } else {
        // Execute non-blocking task
        p.Execute(g)
        s.metrics.tasksCompleted.Add(1)
        s.activeTasksCount.Add(-1)
    }
}

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
        ProcessorMetrics:  make([]core.ProcessorStats, len(s.processors)),
        GlobalQueueStats:  s.globalQueue.Stats(),
        PollerMetrics:     s.networkPoller.GetMetrics(),
    }

    for i, p := range s.processors {
        stats.ProcessorMetrics[i] = p.GetStats()
    }

    return stats
}

func (s *Scheduler) GetProcessors() []*core.Processor {
    s.mu.RLock()
    defer s.mu.RUnlock()
    return s.processors
}

func (s *Scheduler) State() SchedulerState {
    return SchedulerState(s.state.Load())
}

func (s SchedulerState) String() string {
    switch s {
    case SchedulerStopped:
        return "stopped"
    case SchedulerRunning:
        return "running"
    case SchedulerStopping:
        return "stopping"
    default:
        return "unknown"
    }
}
