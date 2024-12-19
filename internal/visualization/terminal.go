package visualization

import (
	"fmt"
	"sync"
	"time"
	"workstealing/internal/scheduler"

	ui "github.com/gizak/termui/v3"
	"github.com/gizak/termui/v3/widgets"
)

type TerminalVisualizer struct {
	scheduler      *scheduler.Scheduler
	updateInterval time.Duration
	mu             sync.RWMutex
	stop           chan struct{}

	// UI Components
	header        *widgets.Paragraph
	globalQueue   *widgets.Gauge
	processorGrid *widgets.Table
	pollerStats   *widgets.Paragraph
	eventLog      *widgets.List

	// Event tracking
	events         []string
	maxEvents      int
	lastTaskCount  uint64
	lastStealCount uint64
}

func NewTerminalVisualizer(s *scheduler.Scheduler, interval time.Duration) (*TerminalVisualizer, error) {
	if err := ui.Init(); err != nil {
		return nil, fmt.Errorf("failed to initialize UI: %v", err)
	}

	tv := &TerminalVisualizer{
		scheduler:      s,
		updateInterval: interval,
		stop:           make(chan struct{}),
		maxEvents:      20,
		events:         make([]string, 0, 20),
	}

	if err := tv.initComponents(); err != nil {
		ui.Close()
		return nil, err
	}

	return tv, nil
}

func (tv *TerminalVisualizer) initComponents() error {
	termWidth, termHeight := ui.TerminalDimensions()

	// Header with system info
	tv.header = widgets.NewParagraph()
	tv.header.Title = "Work Stealing Scheduler Monitor"
	tv.header.SetRect(0, 0, termWidth, 3)
	tv.header.BorderStyle = ui.NewStyle(ColorScheme.HeaderText)

	// Global Queue gauge
	tv.globalQueue = widgets.NewGauge()
	tv.globalQueue.Title = "Global Queue"
	tv.globalQueue.SetRect(0, 3, termWidth, 6)
	tv.globalQueue.BarColor = ColorScheme.QueueLow
	tv.globalQueue.BorderStyle = ui.NewStyle(ColorScheme.Border)

	// Processor status grid
	tv.processorGrid = widgets.NewTable()
	tv.processorGrid.Title = "Processors"
	tv.processorGrid.SetRect(0, 6, termWidth, 15)
	tv.processorGrid.BorderStyle = ui.NewStyle(ColorScheme.Border)
	tv.processorGrid.RowSeparator = true
	tv.processorGrid.FillRow = true
	tv.processorGrid.Rows = [][]string{
		{
			"ID",
			"State",
			"Current Task",
			"Queue Size",
			"Tasks Done",
			"Steals (L/G)",
			"Idle Time",
		},
	}

	// Network Poller stats
	tv.pollerStats = widgets.NewParagraph()
	tv.pollerStats.Title = "Network Poller"
	tv.pollerStats.SetRect(0, 15, termWidth, 19)
	tv.pollerStats.BorderStyle = ui.NewStyle(ColorScheme.Border)

	// Event log
	tv.eventLog = widgets.NewList()
	tv.eventLog.Title = "Event Log"
	tv.eventLog.SetRect(0, 19, termWidth, termHeight-1)
	tv.eventLog.BorderStyle = ui.NewStyle(ColorScheme.Border)

	return nil
}

func (tv *TerminalVisualizer) Start() {
	go tv.updateLoop()

	uiEvents := ui.PollEvents()
	for {
		select {
		case e := <-uiEvents:
			switch e.ID {
			case "q", "<C-c>":
				tv.Stop()
				return
			case "<Resize>":
				payload := e.Payload.(ui.Resize)
				tv.handleResize(payload.Width, payload.Height)
			}
		case <-tv.stop:
			return
		}
	}
}

func (tv *TerminalVisualizer) updateLoop() {
	ticker := time.NewTicker(tv.updateInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			tv.update()
		case <-tv.stop:
			return
		}
	}
}

func (tv *TerminalVisualizer) update() {
	tv.mu.Lock()
	defer tv.mu.Unlock()

	stats := tv.scheduler.GetStats()

	// Update components
	tv.updateHeader(stats)
	tv.updateGlobalQueue(stats)
	tv.updateProcessorGrid(stats)
	tv.updatePollerStats(stats)

	// Render all components
	tv.render()
}

func (tv *TerminalVisualizer) updateHeader(stats scheduler.SchedulerStats) {
	runTime := stats.RunningTime.Round(time.Second)
	taskRate := float64(stats.TasksCompleted-tv.lastTaskCount) / tv.updateInterval.Seconds()
	stealRate := float64(stats.TotalSteals-tv.lastStealCount) / tv.updateInterval.Seconds()

	tv.header.Text = fmt.Sprintf(
		" Runtime: %v | Tasks: %d/%d | Rate: %.1f/s | Steals: %.1f/s",
		runTime,
		stats.TasksCompleted,
		stats.TasksScheduled,
		taskRate,
		stealRate,
	)

	tv.lastTaskCount = stats.TasksCompleted
	tv.lastStealCount = stats.TotalSteals
}

func (tv *TerminalVisualizer) updateGlobalQueue(stats scheduler.SchedulerStats) {
	gq := stats.GlobalQueueStats
	percent := int(gq.Utilization * 100)
	tv.globalQueue.Percent = percent
	tv.globalQueue.Label = fmt.Sprintf("%d/%d (%.1f%%) | Submitted: %d | Rejected: %d",
		gq.CurrentSize, gq.Capacity, gq.Utilization*100, gq.Submitted, gq.Rejected)
	tv.globalQueue.BarColor = getQueueColor(gq.Utilization)
}

func (tv *TerminalVisualizer) updateProcessorGrid(stats scheduler.SchedulerStats) {
	rows := make([][]string, len(stats.ProcessorMetrics)+1)
	rows[0] = tv.processorGrid.Rows[0] // Keep header

	for i, p := range stats.ProcessorMetrics {
		currentTask := "Idle"
		if p.CurrentTask != nil {
			currentTask = fmt.Sprintf("G%d (%s)",
				p.CurrentTask.ID(), formatDuration(p.CurrentTask.ExecutionTime()))
		}

		rows[i+1] = []string{
			fmt.Sprintf("P%d", p.ID),
			p.State.String(),
			currentTask,
			fmt.Sprintf("%d", p.QueueSize),
			fmt.Sprintf("%d", p.TasksExecuted),
			fmt.Sprintf("%d/%d", p.LocalSteals, p.GlobalSteals),
			formatDuration(p.IdleTime),
		}
	}

	tv.processorGrid.Rows = rows
}
func (tv *TerminalVisualizer) updatePollerStats(stats scheduler.SchedulerStats) {
	pm := stats.PollerMetrics
	tv.pollerStats.Text = fmt.Sprintf(
		"Currently Blocked: %d | Completed: %d | Timeouts: %d | Errors: %d\n"+
			"Average Block Time: %v | Active Events: %d",
		pm.CurrentlyBlocked,
		pm.CompletedEvents,
		pm.Timeouts,
		pm.Errors,
		pm.AverageBlockTime.Round(time.Millisecond),
		pm.ActiveEvents,
	)
}

func (tv *TerminalVisualizer) AddEvent(msg string) {
	tv.mu.Lock()
	defer tv.mu.Unlock()

	timestamp := time.Now().Format("15:04:05")
	event := fmt.Sprintf("[%s] %s", timestamp, msg)

	tv.events = append([]string{event}, tv.events...)
	if len(tv.events) > tv.maxEvents {
		tv.events = tv.events[:tv.maxEvents]
	}

	tv.eventLog.Rows = tv.events
}

func (tv *TerminalVisualizer) Stop() {
	close(tv.stop)
	ui.Close()
}

func (tv *TerminalVisualizer) render() {
	ui.Render(
		tv.header,
		tv.globalQueue,
		tv.processorGrid,
		tv.pollerStats,
		tv.eventLog,
	)
}

func (tv *TerminalVisualizer) handleResize(width, height int) {
	tv.mu.Lock()
	defer tv.mu.Unlock()

	tv.header.SetRect(0, 0, width, 3)
	tv.globalQueue.SetRect(0, 3, width, 6)
	tv.processorGrid.SetRect(0, 6, width, 15)
	tv.pollerStats.SetRect(0, 15, width, 19)
	tv.eventLog.SetRect(0, 19, width, height-1)

	tv.render()
}

func formatDuration(d time.Duration) string {
	if d < time.Second {
		return fmt.Sprintf("%dms", d.Milliseconds())
	}
	return fmt.Sprintf("%.1fs", d.Seconds())
}

func colorize(text string, color ui.Color) string {
	return fmt.Sprintf("[%s](fg:%v)", text, color)
}
