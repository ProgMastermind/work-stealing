package core

import (
	"math/rand"
	"sync"
	"sync/atomic"
	"time"
)

const (
	defaultCapacity = 10000
	minStealSize    = 1
)

// GlobalQueueStats represents current queue statistics
type GlobalQueueStats struct {
	CurrentSize   int32
	Capacity      int32
	Submitted     uint64
	Executed      uint64
	Rejected      uint64
	Stolen        uint64
	Utilization   float64
	LastStealTime time.Time
}

// GlobalQueue manages the central task pool
type GlobalQueue struct {
	tasks    []*Goroutine
	capacity int32
	size     atomic.Int32

	metrics struct {
		submitted atomic.Uint64
		executed  atomic.Uint64
		rejected  atomic.Uint64
		stolen    atomic.Uint64
	}

	mu            sync.RWMutex
	rand          *rand.Rand
	lastStealTime time.Time
}

// NewGlobalQueue creates a new global queue instance
func NewGlobalQueue(capacity int32) *GlobalQueue {
	if capacity <= 0 {
		capacity = defaultCapacity
	}

	return &GlobalQueue{
		tasks:         make([]*Goroutine, 0, capacity),
		capacity:      capacity,
		rand:          rand.New(rand.NewSource(time.Now().UnixNano())),
		lastStealTime: time.Now(),
	}
}

// Submit adds a new goroutine to the queue
func (gq *GlobalQueue) Submit(g *Goroutine) bool {
	if g == nil {
		return false
	}

	gq.mu.Lock()
	defer gq.mu.Unlock()

	if int32(len(gq.tasks)) >= gq.capacity {
		gq.metrics.rejected.Add(1)
		return false
	}

	g.SetSource(SourceGlobalQueue, 0)
	gq.tasks = append(gq.tasks, g)
	gq.size.Add(1)
	gq.metrics.submitted.Add(1)
	return true
}

// TrySteal attempts to steal tasks from the global queue
func (gq *GlobalQueue) TrySteal(maxTasks int) []*Goroutine {
	if maxTasks < minStealSize {
		return nil
	}

	gq.mu.Lock()
	defer gq.mu.Unlock()

	currentSize := len(gq.tasks)
	if currentSize == 0 {
		return nil
	}

	// Steal up to half of available tasks
	stealCount := min(maxTasks, (currentSize+1)/2)
	stolen := make([]*Goroutine, 0, stealCount)

	for i := 0; i < stealCount; i++ {
		idx := gq.rand.Intn(len(gq.tasks))
		g := gq.tasks[idx]

		// Remove the stolen task
		lastIdx := len(gq.tasks) - 1
		gq.tasks[idx] = gq.tasks[lastIdx]
		gq.tasks = gq.tasks[:lastIdx]

		stolen = append(stolen, g)
	}

	if len(stolen) > 0 {
		gq.size.Add(int32(-len(stolen)))
		gq.metrics.stolen.Add(uint64(len(stolen)))
		gq.lastStealTime = time.Now()
	}

	return stolen
}

// Take removes and returns a specific number of tasks
func (gq *GlobalQueue) Take(count int) []*Goroutine {
	if count <= 0 {
		return nil
	}

	gq.mu.Lock()
	defer gq.mu.Unlock()

	if len(gq.tasks) == 0 {
		return nil
	}

	n := min(count, len(gq.tasks))
	tasks := make([]*Goroutine, n)

	for i := 0; i < n; i++ {
		tasks[i] = gq.tasks[i]
	}

	// Remove taken tasks
	gq.tasks = gq.tasks[n:]
	gq.size.Add(int32(-n))
	gq.metrics.executed.Add(uint64(n))

	return tasks
}

// Size returns the current number of tasks
func (gq *GlobalQueue) Size() int32 {
	return gq.size.Load()
}

// IsEmpty returns whether the queue is empty
func (gq *GlobalQueue) IsEmpty() bool {
	return gq.Size() == 0
}

// IsFull returns whether the queue is at capacity
func (gq *GlobalQueue) IsFull() bool {
	return gq.Size() >= gq.capacity
}

// Stats returns current queue statistics
func (gq *GlobalQueue) Stats() GlobalQueueStats {
	gq.mu.RLock()
	defer gq.mu.RUnlock()

	currentSize := int32(len(gq.tasks))
	return GlobalQueueStats{
		CurrentSize:   currentSize,
		Capacity:      gq.capacity,
		Submitted:     gq.metrics.submitted.Load(),
		Executed:      gq.metrics.executed.Load(),
		Rejected:      gq.metrics.rejected.Load(),
		Stolen:        gq.metrics.stolen.Load(),
		Utilization:   float64(currentSize) / float64(gq.capacity),
		LastStealTime: gq.lastStealTime,
	}
}

// Clear removes all tasks and resets the queue
func (gq *GlobalQueue) Clear() {
	gq.mu.Lock()
	defer gq.mu.Unlock()

	removed := len(gq.tasks)
	gq.tasks = gq.tasks[:0]
	gq.size.Store(0)
	gq.metrics.executed.Add(uint64(removed))
}

// ResetMetrics resets all metrics counters
func (gq *GlobalQueue) ResetMetrics() {
	gq.metrics.submitted.Store(0)
	gq.metrics.executed.Store(0)
	gq.metrics.rejected.Store(0)
	gq.metrics.stolen.Store(0)
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
