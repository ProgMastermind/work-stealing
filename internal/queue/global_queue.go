package queue

import (
	"math/rand"
	"sync"
	"sync/atomic"
	"time"
	"workstealing/internal/core"
)

type GlobalQueue struct {
    tasks     []*core.Goroutine
    mu        sync.RWMutex
    size      int32
    capacity  int32

    metrics struct {
        submitted    atomic.Int64  // total number of tasks submitted
        executed     atomic.Int64  // total number of tasks executed
        rejected     atomic.Int64  // tasks rejected due to capacity
        stolen      atomic.Int64  // tasks stolen by processors
    }

    rand *rand.Rand // For random task distribution
}

type QueueStats struct {
    CurrentSize    int32
    Capacity      int32
    Submitted     int64
    Executed      int64
    Rejected      int64
    Stolen        int64
    Utilization   float64
}

func NewGlobalQueue(capacity int32) *GlobalQueue {
    return &GlobalQueue{
        tasks:    make([]*core.Goroutine, 0, capacity),
        capacity: capacity,
        rand:     rand.New(rand.NewSource(time.Now().UnixNano())),
    }
}

// Submit adds a new goroutine to the global queue
func (gq *GlobalQueue) Submit(g *core.Goroutine) bool {
    gq.mu.Lock()
    defer gq.mu.Unlock()

    if atomic.LoadInt32(&gq.size) >= gq.capacity {
        gq.metrics.rejected.Add(1)
        return false
    }

    g.SetSource(core.SourceGlobalQueue, 0)
    gq.tasks = append(gq.tasks, g)
    atomic.AddInt32(&gq.size, 1)
    gq.metrics.submitted.Add(1)
    return true
}

// Fetch retrieves multiple goroutines for a processor
func (gq *GlobalQueue) Fetch(max int) []*core.Goroutine {
    gq.mu.Lock()
    defer gq.mu.Unlock()

    if len(gq.tasks) == 0 {
        return nil
    }

    // Calculate how many tasks to fetch
    n := min(max, len(gq.tasks))
    tasks := make([]*core.Goroutine, n)

    // Randomly select tasks to maintain good distribution
    for i := 0; i < n; i++ {
        idx := gq.rand.Intn(len(gq.tasks))
        tasks[i] = gq.tasks[idx]
        // Remove the selected task
        gq.tasks[idx] = gq.tasks[len(gq.tasks)-1]
        gq.tasks = gq.tasks[:len(gq.tasks)-1]
    }

    atomic.AddInt32(&gq.size, int32(-n))
    gq.metrics.executed.Add(int64(n))
    return tasks
}

// TrySteal attempts to steal tasks directly from the global queue
func (gq *GlobalQueue) TrySteal(max int) []*core.Goroutine {
    gq.mu.Lock()
    defer gq.mu.Unlock()

    if len(gq.tasks) == 0 {
        return nil
    }

    n := min(max, len(gq.tasks)/2) // Steal up to half of available tasks
    if n == 0 {
        n = 1 // Steal at least one if available
    }

    stolen := make([]*core.Goroutine, n)
    for i := 0; i < n; i++ {
        idx := gq.rand.Intn(len(gq.tasks))
        stolen[i] = gq.tasks[idx]
        // Remove the stolen task
        gq.tasks[idx] = gq.tasks[len(gq.tasks)-1]
        gq.tasks = gq.tasks[:len(gq.tasks)-1]
    }

    atomic.AddInt32(&gq.size, int32(-n))
    gq.metrics.stolen.Add(int64(n))
    return stolen
}

// Size returns the current number of goroutines in the queue
func (gq *GlobalQueue) Size() int32 {
    return atomic.LoadInt32(&gq.size)
}

// Stats returns queue statistics
func (gq *GlobalQueue) Stats() QueueStats {
    gq.mu.RLock()
    defer gq.mu.RUnlock()

    return QueueStats{
        CurrentSize:  atomic.LoadInt32(&gq.size),
        Capacity:     gq.capacity,
        Submitted:    gq.metrics.submitted.Load(),
        Executed:     gq.metrics.executed.Load(),
        Rejected:     gq.metrics.rejected.Load(),
        Stolen:       gq.metrics.stolen.Load(),
        Utilization:  float64(atomic.LoadInt32(&gq.size)) / float64(gq.capacity),
    }
}

// Clear removes all tasks from the queue
func (gq *GlobalQueue) Clear() {
    gq.mu.Lock()
    defer gq.mu.Unlock()

    removed := len(gq.tasks)
    gq.tasks = gq.tasks[:0]
    atomic.StoreInt32(&gq.size, 0)
    gq.metrics.executed.Add(int64(removed))
}

// IsFull returns whether the queue is at capacity
func (gq *GlobalQueue) IsFull() bool {
    return atomic.LoadInt32(&gq.size) >= gq.capacity
}

// IsEmpty returns whether the queue is empty
func (gq *GlobalQueue) IsEmpty() bool {
    return atomic.LoadInt32(&gq.size) == 0
}

func min(a, b int) int {
    if a < b {
        return a
    }
    return b
}

// ResetMetrics resets all metrics counters
func (gq *GlobalQueue) ResetMetrics() {
    gq.metrics.submitted.Store(0)
    gq.metrics.executed.Store(0)
    gq.metrics.rejected.Store(0)
    gq.metrics.stolen.Store(0)
}
