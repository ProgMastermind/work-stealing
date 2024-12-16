package core

import (
	"sync/atomic"
	"time"
)

// Global variable for ID generation
var globalGID uint64

// GoroutineState represents the current state of a goroutine
type GoroutineState int32

const (
    GoroutineCreated GoroutineState = iota
    GoroutineRunnable
    GoroutineRunning
    GoroutineBlocked
    GoroutineFinished
)

// TaskSource represents where the goroutine came from
type TaskSource int

const (
    SourceGlobalQueue TaskSource = iota
    SourceLocalQueue
    SourceStolen
    SourceNetworkPoller
)

// Goroutine represents a lightweight unit of execution
type Goroutine struct {
    id         uint64
    state      atomic.Int32
    workload   time.Duration
    startTime  time.Time
    endTime    time.Time
    blocking   bool
    result     interface{}
    err        error
    source     TaskSource
    stolenFrom uint32 // ID of processor it was stolen from
}

// NewGoroutine creates a new goroutine instance
func NewGoroutine(workload time.Duration, blocking bool) *Goroutine {
    g := &Goroutine{
        id:       atomic.AddUint64(&globalGID, 1),
        workload: workload,
        blocking: blocking,
        source:   SourceLocalQueue, // Default source
    }
    g.state.Store(int32(GoroutineCreated))
    return g
}

// ID returns the goroutine's unique identifier
func (g *Goroutine) ID() uint64 {
    return g.id
}

// State returns the current state of the goroutine
func (g *Goroutine) State() GoroutineState {
    return GoroutineState(g.state.Load())
}

// SetState atomically updates the goroutine's state
func (g *Goroutine) SetState(state GoroutineState) {
    g.state.Store(int32(state))
}

// IsBlocking returns whether this goroutine performs blocking operations
func (g *Goroutine) IsBlocking() bool {
    return g.blocking
}

// Start marks the goroutine as running and records start time
func (g *Goroutine) Start() {
    g.startTime = time.Now()
    g.SetState(GoroutineRunning)
}

// Finish marks the goroutine as finished and records end time
func (g *Goroutine) Finish(result interface{}, err error) {
    g.endTime = time.Now()
    g.result = result
    g.err = err
    g.SetState(GoroutineFinished)
}

// ExecutionTime returns the total execution time of the goroutine
func (g *Goroutine) ExecutionTime() time.Duration {
    if g.State() != GoroutineFinished {
        return time.Since(g.startTime)
    }
    return g.endTime.Sub(g.startTime)
}

// Workload returns the expected execution time of the goroutine
func (g *Goroutine) Workload() time.Duration {
    return g.workload
}

// Source returns the source of the goroutine
func (g *Goroutine) Source() TaskSource {
    return g.source
}

// SetSource sets the source and stolen information of the goroutine
func (g *Goroutine) SetSource(source TaskSource, stolenFrom uint32) {
    g.source = source
    g.stolenFrom = stolenFrom
}

// StolenFrom returns the ID of the processor this goroutine was stolen from
func (g *Goroutine) StolenFrom() uint32 {
    return g.stolenFrom
}
