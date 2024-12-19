package poller

import (
	"context"
	"sync"
	"sync/atomic"
	"time"
	"workstealing/internal/core"
)

// EventType represents different types of blocking events
type EventType int32

const (
	EventRead EventType = iota
	EventWrite
	EventTimeout
	EventError
)

const (
	defaultEventBufferSize = 1000
	timeoutCheckInterval   = 10 * time.Millisecond
)

// Event represents a blocking operation
type Event struct {
	ID          uint64
	Type        EventType
	Goroutine   *core.Goroutine
	Result      interface{}
	Error       error
	Deadline    time.Time
	Created     time.Time
	ProcessorID uint32
	done        chan struct{}
}

// BlockedGoroutineInfo tracks details of blocked goroutines
type BlockedGoroutineInfo struct {
	StartTime   time.Time
	EventType   EventType
	ProcessorID uint32
	Deadline    time.Time
}

// PollerMetrics represents runtime statistics
type PollerMetrics struct {
	TotalEvents      uint64
	CompletedEvents  uint64
	Timeouts         uint64
	Errors           uint64
	CurrentlyBlocked int32
	AverageBlockTime time.Duration
	ActiveEvents     int
}

// NetworkPoller manages blocking operations
type NetworkPoller struct {
	events     map[uint64]*Event // Active events
	processors []*core.Processor // Available processors

	metrics struct {
		totalEvents     atomic.Uint64
		completedEvents atomic.Uint64
		timeouts        atomic.Uint64
		errors          atomic.Uint64
		avgBlockTime    atomic.Int64 // nanoseconds
		currentBlocked  atomic.Int32
	}

	blockedGoroutines map[uint64]*BlockedGoroutineInfo

	eventCh chan *Event // Channel for new events
	doneCh  chan uint64 // Channel for completed events

	ctx     context.Context
	cancel  context.CancelFunc
	wg      sync.WaitGroup
	running atomic.Bool

	mu sync.RWMutex // Protects maps and internal state
}

// NewNetworkPoller creates a new poller instance
func NewNetworkPoller(processors []*core.Processor) *NetworkPoller {
	if len(processors) == 0 {
		return nil
	}

	ctx, cancel := context.WithCancel(context.Background())

	return &NetworkPoller{
		events:            make(map[uint64]*Event),
		processors:        processors,
		blockedGoroutines: make(map[uint64]*BlockedGoroutineInfo),
		eventCh:           make(chan *Event, defaultEventBufferSize),
		doneCh:            make(chan uint64, defaultEventBufferSize),
		ctx:               ctx,
		cancel:            cancel,
	}
}

// Start begins the polling operation
func (np *NetworkPoller) Start() {
	if !np.running.CompareAndSwap(false, true) {
		return
	}

	np.wg.Add(2)
	go np.eventLoop()
	go np.timeoutChecker()
}

// Stop gracefully stops the poller
func (np *NetworkPoller) Stop() {
	if !np.running.CompareAndSwap(true, false) {
		return
	}

	np.cancel()
	np.wg.Wait()

	// Clean up remaining events
	np.mu.Lock()
	defer np.mu.Unlock()

	for id, event := range np.events {
		np.handleTimeout(id, event)
	}

	// Clear channels
	for len(np.eventCh) > 0 {
		<-np.eventCh
	}
	for len(np.doneCh) > 0 {
		<-np.doneCh
	}
}

// Register adds a new blocking operation
func (np *NetworkPoller) Register(g *core.Goroutine, eventType EventType, deadline time.Time, processorID uint32, done chan struct{}) {
	if g == nil || !np.running.Load() {
		return
	}

	event := &Event{
		ID:          g.ID(),
		Type:        eventType,
		Goroutine:   g,
		Deadline:    deadline,
		Created:     time.Now(),
		ProcessorID: processorID,
		done:        done,
	}

	np.mu.Lock()
	np.events[g.ID()] = event
	np.blockedGoroutines[g.ID()] = &BlockedGoroutineInfo{
		StartTime:   event.Created,
		EventType:   eventType,
		ProcessorID: processorID,
		Deadline:    deadline,
	}
	np.mu.Unlock()

	np.metrics.totalEvents.Add(1)
	np.metrics.currentBlocked.Add(1)
	g.SetState(core.GoroutineBlocked)

	select {
	case np.eventCh <- event:
	default:
		// If channel is full, handle as timeout
		np.handleTimeout(g.ID(), event)
	}
}

// eventLoop processes events
func (np *NetworkPoller) eventLoop() {
	defer np.wg.Done()

	for {
		select {
		case <-np.ctx.Done():
			return

		case event := <-np.eventCh:
			go np.processEvent(event)

		case gid := <-np.doneCh:
			np.completeEvent(gid)
		}
	}
}

// processEvent simulates I/O operation
func (np *NetworkPoller) processEvent(event *Event) {
	if event == nil {
		return
	}

	simulatedWork := time.Duration(float64(event.Goroutine.Workload()) * 0.8)

	select {
	case <-np.ctx.Done():
		return
	case <-time.After(simulatedWork):
		np.doneCh <- event.ID
	}
}

// completeEvent handles event completion
func (np *NetworkPoller) completeEvent(gid uint64) {
	np.mu.Lock()
	event, exists := np.events[gid]
	if !exists {
		np.mu.Unlock()
		return
	}

	delete(np.events, gid)
	delete(np.blockedGoroutines, gid)
	np.mu.Unlock()

	np.metrics.completedEvents.Add(1)
	np.metrics.currentBlocked.Add(-1)

	blockTime := time.Since(event.Created)
	np.updateAverageBlockTime(blockTime)

	event.Goroutine.SetState(core.GoroutineRunnable)
	event.Goroutine.SetSource(core.SourceNetworkPoller, event.ProcessorID)

	if processor := np.findLeastLoadedProcessor(); processor != nil {
		// processor.Push(event.Goroutine)
		event.done <- struct{}{}
	}
}

// timeoutChecker monitors for deadline expiration
func (np *NetworkPoller) timeoutChecker() {
	defer np.wg.Done()

	ticker := time.NewTicker(timeoutCheckInterval)
	defer ticker.Stop()

	for {
		select {
		case <-np.ctx.Done():
			return
		case <-ticker.C:
			np.checkTimeouts()
		}
	}
}

// checkTimeouts verifies deadlines
func (np *NetworkPoller) checkTimeouts() {
	np.mu.Lock()
	defer np.mu.Unlock()

	now := time.Now()
	for id, event := range np.events {
		if now.After(event.Deadline) {
			np.handleTimeout(id, event)
		}
	}
}

// handleTimeout processes timeout events
func (np *NetworkPoller) handleTimeout(id uint64, event *Event) {
	delete(np.events, id)
	delete(np.blockedGoroutines, id)

	np.metrics.timeouts.Add(1)
	np.metrics.currentBlocked.Add(-1)

	event.Type = EventTimeout
	event.Error = context.DeadlineExceeded
	event.Goroutine.SetState(core.GoroutineRunnable)
	event.Goroutine.SetSource(core.SourceNetworkPoller, 0)

	if processor := np.findLeastLoadedProcessor(); processor != nil {
		processor.Push(event.Goroutine)
	}
}

// findLeastLoadedProcessor returns processor with minimum queue size
func (np *NetworkPoller) findLeastLoadedProcessor() *core.Processor {
	var minLoad = int(^uint(0) >> 1)
	var target *core.Processor

	for _, p := range np.processors {
		if size := p.QueueSize(); size < minLoad {
			minLoad = size
			target = p
		}
	}

	return target
}

// updateAverageBlockTime updates running average of block times
func (np *NetworkPoller) updateAverageBlockTime(blockTime time.Duration) {
	current := time.Duration(np.metrics.avgBlockTime.Load())
	completed := np.metrics.completedEvents.Load()

	if completed == 1 {
		np.metrics.avgBlockTime.Store(int64(blockTime))
		return
	}

	newAvg := (current*time.Duration(completed-1) + blockTime) / time.Duration(completed)
	np.metrics.avgBlockTime.Store(int64(newAvg))
}

// GetMetrics returns current statistics
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

// GetBlockedGoroutineInfo returns information about a blocked goroutine
func (np *NetworkPoller) GetBlockedGoroutineInfo(id uint64) *BlockedGoroutineInfo {
	np.mu.RLock()
	defer np.mu.RUnlock()
	return np.blockedGoroutines[id]
}

func (et EventType) String() string {
	switch et {
	case EventRead:
		return "read"
	case EventWrite:
		return "write"
	case EventTimeout:
		return "timeout"
	case EventError:
		return "error"
	default:
		return "unknown"
	}
}
