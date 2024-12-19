package core

import (
	"math/rand"
	"sync"
	"sync/atomic"
	"time"
)

type ProcessorState int32

const (
	ProcessorIdle ProcessorState = iota
	ProcessorRunning
	ProcessorStealing
)

// ProcessorMetrics tracks runtime statistics
type ProcessorMetrics struct {
	tasksExecuted     atomic.Uint64
	stealsAttempted   atomic.Uint64
	stealsSuccessful  atomic.Uint64
	totalIdleTime     atomic.Int64
	totalRunningTime  atomic.Int64
	globalQueueSteals atomic.Uint64
	localQueueSteals  atomic.Uint64
}

// ProcessorStats represents externally visible metrics
type ProcessorStats struct {
	ID               uint32
	State            ProcessorState
	CurrentTask      *Goroutine
	QueueSize        int
	TasksExecuted    uint64
	StealsAttempted  uint64
	StealsSuccessful uint64
	IdleTime         time.Duration
	RunningTime      time.Duration
	GlobalSteals     uint64
	LocalSteals      uint64
	LastStateChange  time.Time
}

// Processor represents a logical processor (P)
type Processor struct {
	id            uint32
	state         atomic.Int32
	maxQueueSize  int
	currentG      atomic.Pointer[Goroutine]
	metrics       ProcessorMetrics
	lastStateTime time.Time

	mu          sync.RWMutex
	localQueue  *LocalRunQueue
	globalQueue *GlobalQueue // Reference to global queue in same package
	rand        *rand.Rand

	// List of other processors for work stealing
	processors []*Processor
}

// LocalRunQueue manages the processor's local task queue
type LocalRunQueue struct {
	tasks []*Goroutine
	size  int
	count int
	mu    sync.RWMutex
}

// NewProcessor creates a new processor instance
func NewProcessor(id uint32, queueSize int, globalQueue *GlobalQueue) *Processor {
	if queueSize <= 0 {
		queueSize = 256
	}

	return &Processor{
		id:            id,
		maxQueueSize:  queueSize,
		localQueue:    newLocalRunQueue(queueSize),
		globalQueue:   globalQueue,
		lastStateTime: time.Now(),
		rand:          rand.New(rand.NewSource(time.Now().UnixNano())),
	}
}

// SetProcessors sets the list of processors for work stealing
func (p *Processor) SetProcessors(processors []*Processor) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.processors = processors
}

func newLocalRunQueue(size int) *LocalRunQueue {
	return &LocalRunQueue{
		tasks: make([]*Goroutine, 0, size),
		size:  size,
	}
}

// ID returns the processor's identifier
func (p *Processor) ID() uint32 {
	return p.id
}

// State returns current processor state
func (p *Processor) State() ProcessorState {
	return ProcessorState(p.state.Load())
}

// SetState updates processor state with metrics
func (p *Processor) SetState(newState ProcessorState) {
	p.mu.Lock()
	defer p.mu.Unlock()

	oldState := p.State()
	now := time.Now()
	duration := now.Sub(p.lastStateTime)

	switch oldState {
	case ProcessorIdle:
		p.metrics.totalIdleTime.Add(int64(duration))
	case ProcessorRunning:
		p.metrics.totalRunningTime.Add(int64(duration))
	}

	p.state.Store(int32(newState))
	p.lastStateTime = now
}

// Push adds a goroutine to local queue
func (p *Processor) Push(g *Goroutine) bool {
	if g == nil {
		return false
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	if p.localQueue.count >= p.maxQueueSize {
		return false
	}

	return p.localQueue.push(g)
}

// Pop removes and returns a goroutine from local queue
func (p *Processor) Pop() *Goroutine {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.localQueue.pop()
}

// FindWork attempts to get work from various sources
func (p *Processor) FindWork() *Goroutine {
	// First try local queue
	if g := p.Pop(); g != nil {
		return g
	}

	// Then try stealing from other processors
	if stolen := p.tryStealFromProcessors(); len(stolen) > 0 {
		// Push extra stolen tasks to local queue
		for i := 1; i < len(stolen); i++ {
			p.Push(stolen[i])
		}
		return stolen[0]
	}

	// Finally try global queue
	if stolen := p.tryStealFromGlobalQueue(); len(stolen) > 0 {
		// Push extra stolen tasks to local queue
		for i := 1; i < len(stolen); i++ {
			p.Push(stolen[i])
		}
		return stolen[0]
	}

	return nil
}

// selectVictim randomly selects a processor to steal from
func (p *Processor) selectVictim() *Processor {
	p.mu.RLock()
	defer p.mu.RUnlock()

	if len(p.processors) <= 1 {
		return nil
	}

	// Try up to 3 random processors
	for i := 0; i < 3; i++ {
		idx := p.rand.Intn(len(p.processors))
		victim := p.processors[idx]
		if victim.ID() != p.ID() && victim.QueueSize() > 0 {
			return victim
		}
	}
	return nil
	// var maxLoad = 0
	// var target *Processor

	// for _, p := range p.processors {
	// 	if load := p.QueueSize(); load > maxLoad {
	// 		maxLoad = load
	// 		target = p
	// 	}
	// }
	// return target
}

// tryStealFromProcessors attempts to steal from other processors
func (p *Processor) tryStealFromProcessors() []*Goroutine {
	p.SetState(ProcessorStealing)
	defer p.SetState(ProcessorIdle)

	if victim := p.selectVictim(); victim != nil {
		p.metrics.stealsAttempted.Add(1)
		stolen := victim.localQueue.steal((victim.localQueue.count + 1) / 2)
		if len(stolen) > 0 {
			p.metrics.stealsSuccessful.Add(1)
			p.metrics.localQueueSteals.Add(uint64(len(stolen)))
			// Mark tasks as stolen
			for _, g := range stolen {
				g.SetSource(SourceStolen, victim.ID())
			}
			return stolen
		}
	}
	return nil
}

// tryStealFromGlobalQueue attempts to steal from global queue
func (p *Processor) tryStealFromGlobalQueue() []*Goroutine {
	if p.globalQueue == nil {
		return nil
	}

	stolen := p.globalQueue.TrySteal(p.maxQueueSize / 2)
	if len(stolen) > 0 {
		p.metrics.globalQueueSteals.Add(uint64(len(stolen)))
		return stolen
	}
	return nil
}

// Execute runs a goroutine
func (p *Processor) Execute(g *Goroutine) {
	if g == nil {
		return
	}

	p.currentG.Store(g)
	defer p.currentG.Store(nil)

	p.SetState(ProcessorRunning)

	g.Start()
	time.Sleep(g.Workload()) // Simulate execution
	g.Finish(nil, nil)

	p.metrics.tasksExecuted.Add(1)

	p.SetState(ProcessorIdle)
}

// LocalRunQueue methods
func (lrq *LocalRunQueue) push(g *Goroutine) bool {
	lrq.mu.Lock()
	defer lrq.mu.Unlock()

	if lrq.count >= lrq.size {
		return false
	}

	lrq.tasks = append(lrq.tasks, g)
	lrq.count++
	return true
}

func (lrq *LocalRunQueue) pop() *Goroutine {
	lrq.mu.Lock()
	defer lrq.mu.Unlock()

	if lrq.count == 0 {
		return nil
	}

	g := lrq.tasks[0]
	lrq.tasks = lrq.tasks[1:]
	lrq.count--
	return g
}

func (lrq *LocalRunQueue) steal(n int) []*Goroutine {
	lrq.mu.Lock()
	defer lrq.mu.Unlock()

	if n <= 0 || lrq.count == 0 {
		return nil
	}

	if n > lrq.count {
		n = lrq.count
	}

	stolen := make([]*Goroutine, n)
	stealIndex := lrq.count - n
	copy(stolen, lrq.tasks[stealIndex:])
	lrq.tasks = lrq.tasks[:stealIndex]
	lrq.count = stealIndex

	return stolen
}

// GetStats returns current processor statistics
func (p *Processor) GetStats() ProcessorStats {
	p.mu.RLock()
	defer p.mu.RUnlock()

	return ProcessorStats{
		ID:               p.id,
		State:            p.State(),
		CurrentTask:      p.currentG.Load(),
		QueueSize:        p.localQueue.count,
		TasksExecuted:    p.metrics.tasksExecuted.Load(),
		StealsAttempted:  p.metrics.stealsAttempted.Load(),
		StealsSuccessful: p.metrics.stealsSuccessful.Load(),
		IdleTime:         time.Duration(p.metrics.totalIdleTime.Load()),
		RunningTime:      time.Duration(p.metrics.totalRunningTime.Load()),
		GlobalSteals:     p.metrics.globalQueueSteals.Load(),
		LocalSteals:      p.metrics.localQueueSteals.Load(),
		LastStateChange:  p.lastStateTime,
	}
}

// QueueSize returns current local queue size
func (p *Processor) QueueSize() int {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.localQueue.count
}

func (p *Processor) CurrentGoroutine() *Goroutine {
	return p.currentG.Load()
}

func (s ProcessorState) String() string {
	switch s {
	case ProcessorIdle:
		return "idle"
	case ProcessorRunning:
		return "running"
	case ProcessorStealing:
		return "stealing"
	default:
		return "unknown"
	}
}
