package poller

import (
	"context"
	"sync"
	"sync/atomic"
	"time"
	"workstealing/internal/core"
)

type EventType int

const (
    EventRead EventType = iota
    EventWrite
    EventTimeout
    EventError
)

type Event struct {
    Type      EventType
    Goroutine *core.Goroutine
    Result    interface{}
    Error     error
    Deadline  time.Time
    Created   time.Time
    ProcessorID uint32  // ID of the processor that submitted this task
}

type NetworkPoller struct {
    events     map[uint64]*Event
    processors []*core.Processor
    mu         sync.RWMutex

    metrics struct {
        totalEvents     atomic.Uint64
        completedEvents atomic.Uint64
        timeouts        atomic.Uint64
        errors          atomic.Uint64
        avgBlockTime    atomic.Int64    // Average blocking time in nanoseconds
        currentBlocked  atomic.Int32    // Number of currently blocked goroutines
    }

    // Track blocked goroutines and their details
    blockedGoroutines map[uint64]*BlockedGoroutineInfo

    wg      sync.WaitGroup
    ctx     context.Context
    cancel  context.CancelFunc
    running atomic.Bool
}

type BlockedGoroutineInfo struct {
    StartTime   time.Time
    EventType   EventType
    ProcessorID uint32
    Deadline    time.Time
}

type PollerMetrics struct {
    TotalEvents       uint64
    CompletedEvents   uint64
    Timeouts          uint64
    Errors           uint64
    CurrentlyBlocked int32
    AverageBlockTime time.Duration
    ActiveEvents     int
}

func NewNetworkPoller(processors []*core.Processor) *NetworkPoller {
    ctx, cancel := context.WithCancel(context.Background())
    return &NetworkPoller{
        events:            make(map[uint64]*Event),
        processors:        processors,
        ctx:              ctx,
        cancel:           cancel,
        blockedGoroutines: make(map[uint64]*BlockedGoroutineInfo),
    }
}

func (np *NetworkPoller) Start() {
    if !np.running.CompareAndSwap(false, true) {
        return
    }

    np.wg.Add(1)
    go np.pollEvents()
}

func (np *NetworkPoller) Stop() {
    if !np.running.CompareAndSwap(true, false) {
        return
    }

    np.cancel()
    np.wg.Wait()
}

func (np *NetworkPoller) Register(g *core.Goroutine, eventType EventType, deadline time.Time, processorID uint32) {
    np.mu.Lock()
    defer np.mu.Unlock()

    np.metrics.totalEvents.Add(1)
    np.metrics.currentBlocked.Add(1)

    np.events[g.ID()] = &Event{
        Type:        eventType,
        Goroutine:   g,
        Deadline:    deadline,
        Created:     time.Now(),
        ProcessorID: processorID,
    }

    np.blockedGoroutines[g.ID()] = &BlockedGoroutineInfo{
        StartTime:   time.Now(),
        EventType:   eventType,
        ProcessorID: processorID,
        Deadline:    deadline,
    }

    g.SetState(core.GoroutineBlocked)
}

func (np *NetworkPoller) pollEvents() {
    defer np.wg.Done()

    ticker := time.NewTicker(time.Millisecond)
    defer ticker.Stop()

    for {
        select {
        case <-np.ctx.Done():
            return
        case <-ticker.C:
            np.checkEvents()
        }
    }
}

func (np *NetworkPoller) checkEvents() {
    np.mu.Lock()
    defer np.mu.Unlock()

    now := time.Now()
    for id, event := range np.events {
        // First check for timeout
        if now.After(event.Deadline) {
            np.handleTimeout(id, event)
            continue
        }

        // Then check for completion
        if np.shouldComplete(event, now) {
            np.handleCompletion(id, event)
        }
    }
}

func (np *NetworkPoller) shouldComplete(event *Event, now time.Time) bool {
    return now.Sub(event.Created) >= 50*time.Millisecond &&
        now.Before(event.Deadline)
}

func (np *NetworkPoller) handleCompletion(id uint64, event *Event) {
    delete(np.events, id)
    np.metrics.completedEvents.Add(1)
    np.metrics.currentBlocked.Add(-1)

    // Calculate and update average block time
    blockTime := time.Since(event.Created)
    np.updateAverageBlockTime(blockTime)

    event.Result = struct{}{} // Simulate result
    event.Goroutine.SetState(core.GoroutineRunnable)
    event.Goroutine.SetSource(core.SourceNetworkPoller, 0)

    if processor := np.findLeastLoadedProcessor(); processor != nil {
        processor.Push(event.Goroutine)
    }

    delete(np.blockedGoroutines, id)
}

func (np *NetworkPoller) handleTimeout(id uint64, event *Event) {
    delete(np.events, id)
    np.metrics.timeouts.Add(1)
    np.metrics.currentBlocked.Add(-1)

    event.Type = EventTimeout
    event.Error = context.DeadlineExceeded
    event.Goroutine.SetState(core.GoroutineRunnable)
    event.Goroutine.SetSource(core.SourceNetworkPoller, 0)

    if processor := np.findLeastLoadedProcessor(); processor != nil {
        processor.Push(event.Goroutine)
    }

    delete(np.blockedGoroutines, id)
}

func (np *NetworkPoller) findLeastLoadedProcessor() *core.Processor {
    var minLoad = int(^uint(0) >> 1)
    var target *core.Processor

    for _, p := range np.processors {
        status := p.GetStatus()
        if status.QueueSize < minLoad {
            minLoad = status.QueueSize
            target = p
        }
    }

    return target
}

func (np *NetworkPoller) updateAverageBlockTime(blockTime time.Duration) {
    current := time.Duration(np.metrics.avgBlockTime.Load())
    completed := np.metrics.completedEvents.Load()

    // Weighted average calculation
    if completed == 1 {
        np.metrics.avgBlockTime.Store(int64(blockTime))
    } else {
        newAvg := (current*time.Duration(completed-1) + blockTime) / time.Duration(completed)
        np.metrics.avgBlockTime.Store(int64(newAvg))
    }
}

func (np *NetworkPoller) GetMetrics() PollerMetrics {
    np.mu.RLock()
    activeEvents := len(np.events)
    np.mu.RUnlock()

    return PollerMetrics{
        TotalEvents:      np.metrics.totalEvents.Load(),
        CompletedEvents:  np.metrics.completedEvents.Load(),
        Timeouts:         np.metrics.timeouts.Load(),
        Errors:           np.metrics.errors.Load(),
        CurrentlyBlocked: np.metrics.currentBlocked.Load(),
        AverageBlockTime: time.Duration(np.metrics.avgBlockTime.Load()),
        ActiveEvents:     activeEvents,
    }
}

func (np *NetworkPoller) GetBlockedGoroutineInfo(id uint64) *BlockedGoroutineInfo {
    np.mu.RLock()
    defer np.mu.RUnlock()
    return np.blockedGoroutines[id]
}
