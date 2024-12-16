package core

import (
	"math/rand"
	"sync"
	"sync/atomic"
	"time"
)

// ProcessorState represents the current state of a processor
type ProcessorState int32

const (
    ProcessorIdle ProcessorState = iota
    ProcessorRunning
    ProcessorStealing
)

// ProcessorStatus holds current processor metrics
type ProcessorStatus struct {
    CurrentGoroutine *Goroutine
    QueueSize        int
    State            ProcessorState
    LastStateChange  time.Time
}

// ProcessorMetricsData represents the metrics data for external consumption
type ProcessorMetricsData struct {
    TasksExecuted      uint64
    StealsAttempted    uint64
    StealsSuccessful   uint64
    TotalIdleTime      time.Duration
    TotalRunningTime   time.Duration
    LastMetricsReset   time.Time
    GlobalQueueSteals  uint64
    LocalQueueSteals   uint64
    TasksFromPoller    uint64
}

// Processor represents a logical processor (P)
type Processor struct {
    id            uint32
    state         atomic.Int32
    localQueue    *Queue
    currentG      atomic.Pointer[Goroutine]
    maxQueueSize  int
    mu            sync.RWMutex
    metrics       *processorMetrics
    lastStateTime time.Time
    rand          *rand.Rand
}

// processorMetrics tracks processor performance using atomic operations
type processorMetrics struct {
    tasksExecuted     atomic.Uint64
    stealsAttempted   atomic.Uint64
    stealsSuccessful  atomic.Uint64
    totalIdleTime     atomic.Int64
    totalRunningTime  atomic.Int64
    globalQueueSteals atomic.Uint64
    localQueueSteals  atomic.Uint64
    tasksFromPoller   atomic.Uint64
    lastMetricsReset  time.Time
}

// Queue represents the Local Run Queue
type Queue struct {
    items []*Goroutine
    mu    sync.RWMutex
    size  int
}

// NewProcessor creates a new processor instance
func NewProcessor(id uint32, queueSize int) *Processor {
    p := &Processor{
        id:           id,
        localQueue:   NewQueue(queueSize),
        maxQueueSize: queueSize,
        metrics: &processorMetrics{
            lastMetricsReset: time.Now(),
        },
        rand: rand.New(rand.NewSource(time.Now().UnixNano())),
    }
    p.state.Store(int32(ProcessorIdle))
    p.lastStateTime = time.Now()
    return p
}

// NewQueue creates a new local run queue
func NewQueue(size int) *Queue {
    return &Queue{
        items: make([]*Goroutine, 0, size),
        size:  size,
    }
}

// ID returns the processor's identifier
func (p *Processor) ID() uint32 {
    return p.id
}

// State returns the current processor state
func (p *Processor) State() ProcessorState {
    return ProcessorState(p.state.Load())
}

// SetState updates the processor state and records timing
func (p *Processor) SetState(state ProcessorState) {
    p.mu.Lock()
    defer p.mu.Unlock()

    oldState := p.State()
    now := time.Now()

    // Update metrics based on state transition
    p.updateStateMetrics(oldState, now.Sub(p.lastStateTime))

    p.state.Store(int32(state))
    p.lastStateTime = now
}

// Push adds a goroutine to the local queue
func (p *Processor) Push(g *Goroutine) bool {
    p.mu.Lock()
    defer p.mu.Unlock()

    if p.localQueue.Push(g) {
        p.recordTaskSource(g.Source())
        return true
    }
    return false
}

// Pop removes and returns a goroutine from the local queue
func (p *Processor) Pop() *Goroutine {
    p.mu.Lock()
    defer p.mu.Unlock()
    return p.localQueue.Pop()
}

// Steal attempts to steal goroutines from another processor
func (p *Processor) Steal(victim *Processor) []*Goroutine {
    if victim == nil || victim.ID() == p.ID() {
        return nil
    }

    p.metrics.stealsAttempted.Add(1)

    victim.mu.Lock()
    defer victim.mu.Unlock()

    stolen := victim.localQueue.Steal(p.maxQueueSize / 2)
    if len(stolen) > 0 {
        p.metrics.stealsSuccessful.Add(1)
        p.metrics.localQueueSteals.Add(1)

        // Mark stolen tasks
        for _, g := range stolen {
            g.SetSource(SourceStolen, victim.ID())
        }
    }

    return stolen
}

// Queue methods
func (q *Queue) Push(g *Goroutine) bool {
    q.mu.Lock()
    defer q.mu.Unlock()

    if len(q.items) >= q.size {
        return false
    }

    q.items = append(q.items, g)
    return true
}

func (q *Queue) Pop() *Goroutine {
    q.mu.Lock()
    defer q.mu.Unlock()

    if len(q.items) == 0 {
        return nil
    }

    g := q.items[0]
    q.items = q.items[1:]
    return g
}

func (q *Queue) Size() int {
    q.mu.RLock()
    defer q.mu.RUnlock()
    return len(q.items)
}

func (q *Queue) Steal(n int) []*Goroutine {
    q.mu.Lock()
    defer q.mu.Unlock()

    if len(q.items) == 0 {
        return nil
    }

    stealCount := len(q.items) / 2
    if stealCount == 0 {
        stealCount = 1
    }
    if stealCount > n {
        stealCount = n
    }

    splitIndex := len(q.items) - stealCount
    stolen := make([]*Goroutine, stealCount)
    copy(stolen, q.items[splitIndex:])
    q.items = q.items[:splitIndex]

    return stolen
}

// GetStatus returns current processor status
func (p *Processor) GetStatus() ProcessorStatus {
    p.mu.RLock()
    defer p.mu.RUnlock()

    return ProcessorStatus{
        CurrentGoroutine: p.currentG.Load(),
        QueueSize:        p.localQueue.Size(),
        State:            p.State(),
        LastStateChange:  p.lastStateTime,
    }
}

// updateStateMetrics updates timing metrics for different states
func (p *Processor) updateStateMetrics(oldState ProcessorState, duration time.Duration) {
    switch oldState {
    case ProcessorIdle:
        p.metrics.totalIdleTime.Add(int64(duration))
    case ProcessorRunning:
        p.metrics.totalRunningTime.Add(int64(duration))
    }
}

// GetMetrics returns current processor metrics
func (p *Processor) GetMetrics() ProcessorMetricsData {
    return ProcessorMetricsData{
        TasksExecuted:     p.metrics.tasksExecuted.Load(),
        StealsAttempted:   p.metrics.stealsAttempted.Load(),
        StealsSuccessful:  p.metrics.stealsSuccessful.Load(),
        TotalIdleTime:     time.Duration(p.metrics.totalIdleTime.Load()),
        TotalRunningTime:  time.Duration(p.metrics.totalRunningTime.Load()),
        LastMetricsReset:  p.metrics.lastMetricsReset,
        GlobalQueueSteals: p.metrics.globalQueueSteals.Load(),
        LocalQueueSteals:  p.metrics.localQueueSteals.Load(),
        TasksFromPoller:   p.metrics.tasksFromPoller.Load(),
    }
}

// IncrementTasksExecuted increments the number of tasks executed
func (p *Processor) IncrementTasksExecuted() {
    p.metrics.tasksExecuted.Add(1)
}

// recordTaskSource updates metrics based on task source
func (p *Processor) recordTaskSource(source TaskSource) {
    switch source {
    case SourceGlobalQueue:
        p.metrics.globalQueueSteals.Add(1)
    case SourceLocalQueue:
        p.metrics.localQueueSteals.Add(1)
    case SourceNetworkPoller:
        p.metrics.tasksFromPoller.Add(1)
    }
}

// selectVictim randomly selects a processor to steal from
func (p *Processor) selectVictim(processors []*Processor) *Processor {
    if len(processors) <= 1 {
        return nil
    }

    // Try up to 3 random victims
    for i := 0; i < 3; i++ {
        victimIndex := p.rand.Intn(len(processors))
        victim := processors[victimIndex]

        if victim.ID() != p.ID() && victim.localQueue.Size() > 0 {
            return victim
        }
    }
    return nil
}
