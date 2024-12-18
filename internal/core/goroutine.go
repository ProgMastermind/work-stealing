package core

import (
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Global variable for ID generation with atomic operations
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
type TaskSource int32

const (
    SourceGlobalQueue TaskSource = iota
    SourceLocalQueue
    SourceStolen
    SourceNetworkPoller
)

// GoroutineTransition represents a state or location change of a goroutine
type GoroutineTransition struct {
    From      string        // Source state/location
    To        string        // Destination state/location
    Timestamp time.Time     // When the transition occurred
    Reason    string        // Why the transition happened
    Duration  time.Duration // Time spent in 'From' state
}

// Goroutine represents a lightweight unit of execution
type Goroutine struct {
    id               uint64
    state            atomic.Int32    // Current state
    workload         time.Duration   // Expected execution time
    startTime        time.Time       // Execution start time
    endTime          time.Time       // Execution end time
    blocking         bool            // Whether this performs blocking operations
    result           interface{}     // Execution result
    err             error           // Any error that occurred
    source           TaskSource      // Where this goroutine came from
    stolenFrom       uint32         // ID of processor it was stolen from

    // Protected by mutex
    mu               sync.RWMutex
    transitions      []GoroutineTransition
    lastTransitionTime time.Time
}

// NewGoroutine creates a new goroutine instance
func NewGoroutine(workload time.Duration, blocking bool) *Goroutine {
    if workload <= 0 {
        workload = time.Millisecond // Minimum workload
    }

    now := time.Now()
    g := &Goroutine{
        id:                atomic.AddUint64(&globalGID, 1),
        workload:         workload,
        blocking:         blocking,
        source:           SourceLocalQueue,
        transitions:      make([]GoroutineTransition, 0, 10), // Pre-allocate space
        lastTransitionTime: now,
    }
    g.state.Store(int32(GoroutineCreated))

    // Record initial transition
    g.addTransitionLocked("created", "ready", "initialization", now)
    return g
}

// Internal method for adding transitions without locking
func (g *Goroutine) addTransitionLocked(from, to, reason string, timestamp time.Time) {
    if from == "" || to == "" {
        return // Prevent invalid transitions
    }

    duration := timestamp.Sub(g.lastTransitionTime)
    g.transitions = append(g.transitions, GoroutineTransition{
        From:      from,
        To:        to,
        Timestamp: timestamp,
        Reason:    reason,
        Duration:  duration,
    })
    g.lastTransitionTime = timestamp
}

// AddTransition adds a new transition with proper locking
func (g *Goroutine) AddTransition(from, to, reason string) {
    g.mu.Lock()
    defer g.mu.Unlock()
    g.addTransitionLocked(from, to, reason, time.Now())
}

// GetTransitions returns a copy of transition history
func (g *Goroutine) GetTransitions() []GoroutineTransition {
    g.mu.RLock()
    defer g.mu.RUnlock()

    result := make([]GoroutineTransition, len(g.transitions))
    copy(result, g.transitions)
    return result
}

// ID returns the goroutine's unique identifier
func (g *Goroutine) ID() uint64 {
    return g.id
}

// State returns the current state
func (g *Goroutine) State() GoroutineState {
    return GoroutineState(g.state.Load())
}

// SetState atomically updates the goroutine's state
func (g *Goroutine) SetState(state GoroutineState) {
    oldState := g.State()
    g.state.Store(int32(state))

    g.mu.Lock()
    defer g.mu.Unlock()

    g.addTransitionLocked(
        oldState.String(),
        state.String(),
        "state_change",
        time.Now(),
    )
}

// IsBlocking returns whether this goroutine performs blocking operations
func (g *Goroutine) IsBlocking() bool {
    return g.blocking
}

// Start marks the goroutine as running
func (g *Goroutine) Start() {
    now := time.Now()
    g.mu.Lock()
    defer g.mu.Unlock()

    g.startTime = now
    g.state.Store(int32(GoroutineRunning))
    g.addTransitionLocked("ready", "running", "execution_started", now)
}

// Finish marks the goroutine as completed
func (g *Goroutine) Finish(result interface{}, err error) {
    now := time.Now()
    g.mu.Lock()
    defer g.mu.Unlock()

    g.endTime = now
    g.result = result
    g.err = err
    g.state.Store(int32(GoroutineFinished))
    g.addTransitionLocked("running", "finished", "execution_completed", now)
}

// ExecutionTime returns the total execution time
func (g *Goroutine) ExecutionTime() time.Duration {
    g.mu.RLock()
    defer g.mu.RUnlock()

    if g.State() != GoroutineFinished {
        return time.Since(g.startTime)
    }
    return g.endTime.Sub(g.startTime)
}

// Workload returns the expected execution time
func (g *Goroutine) Workload() time.Duration {
    return g.workload
}

// Source returns where the goroutine came from
func (g *Goroutine) Source() TaskSource {
    return g.source
}

// SetSource updates source information
func (g *Goroutine) SetSource(source TaskSource, stolenFrom uint32) {
    g.mu.Lock()
    defer g.mu.Unlock()

    oldSource := g.source
    g.source = source
    g.stolenFrom = stolenFrom

    g.addTransitionLocked(
        oldSource.String(),
        source.String(),
        fmt.Sprintf("source_change_stolen_from_P%d", stolenFrom),
        time.Now(),
    )
}

// StolenFrom returns the processor ID this was stolen from
func (g *Goroutine) StolenFrom() uint32 {
    return g.stolenFrom
}

// String representations
func (s GoroutineState) String() string {
    switch s {
    case GoroutineCreated:
        return "created"
    case GoroutineRunnable:
        return "runnable"
    case GoroutineRunning:
        return "running"
    case GoroutineBlocked:
        return "blocked"
    case GoroutineFinished:
        return "finished"
    default:
        return "unknown"
    }
}

func (s TaskSource) String() string {
    switch s {
    case SourceGlobalQueue:
        return "global_queue"
    case SourceLocalQueue:
        return "local_queue"
    case SourceStolen:
        return "stolen"
    case SourceNetworkPoller:
        return "network_poller"
    default:
        return "unknown"
    }
}

// GetHistory returns formatted transition history
func (g *Goroutine) GetHistory() string {
    g.mu.RLock()
    defer g.mu.RUnlock()

    var history strings.Builder
    history.WriteString(fmt.Sprintf("G%d History:\n", g.id))

    for i, t := range g.transitions {
        history.WriteString(fmt.Sprintf("%d. %s -> %s (%s) [duration: %v]\n",
            i+1, t.From, t.To, t.Reason, t.Duration))
    }

    return history.String()
}
