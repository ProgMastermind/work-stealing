package visualization

import (
	"fmt"
	"sync"
	"time"
	"workstealing/internal/scheduler"

	ui "github.com/gizak/termui/v3"
	"github.com/gizak/termui/v3/widgets"
)

type Event struct {
    Timestamp time.Time
    Message   string
}

type TerminalVisualizer struct {
    scheduler      *scheduler.Scheduler
    updateInterval time.Duration
    mu            sync.RWMutex
    stop          chan struct{}

    // UI components
    globalQueueBox  *widgets.Paragraph
    processorBoxes  []*widgets.Paragraph
    pollerBox      *widgets.Paragraph
    eventBox       *widgets.List

    // Event tracking
    events         []Event
    maxEvents      int
}

func NewTerminalVisualizer(s *scheduler.Scheduler, interval time.Duration) (*TerminalVisualizer, error) {
    if err := ui.Init(); err != nil {
        return nil, fmt.Errorf("failed to initialize terminal UI: %v", err)
    }

    tv := &TerminalVisualizer{
        scheduler:      s,
        updateInterval: interval,
        stop:          make(chan struct{}),
        maxEvents:     10,
        events:        make([]Event, 0, 10),
    }

    if err := tv.initializeComponents(); err != nil {
        ui.Close()
        return nil, fmt.Errorf("failed to initialize components: %v", err)
    }

    return tv, nil
}

func (tv *TerminalVisualizer) initializeComponents() error {
    termWidth, termHeight := ui.TerminalDimensions()

    // Global Queue Box
    tv.globalQueueBox = widgets.NewParagraph()
    tv.globalQueueBox.Title = "Global Queue"
    tv.globalQueueBox.SetRect(0, 0, termWidth, 4)
    tv.globalQueueBox.BorderStyle.Fg = ui.ColorBlue

    // Processor Boxes
    stats := tv.scheduler.GetStats()
    numProcessors := len(stats.ProcessorMetrics)
    tv.processorBoxes = make([]*widgets.Paragraph, numProcessors)

    processorHeight := 5
    processorsStartY := 4

    for i := 0; i < numProcessors; i++ {
        tv.processorBoxes[i] = widgets.NewParagraph()
        tv.processorBoxes[i].Title = fmt.Sprintf("P%d", i)
        tv.processorBoxes[i].SetRect(
            0,
            processorsStartY + (i * processorHeight),
            termWidth,
            processorsStartY + ((i + 1) * processorHeight),
        )
        tv.processorBoxes[i].BorderStyle.Fg = ui.ColorGreen
    }

    // Network Poller Box
    pollerStartY := processorsStartY + (numProcessors * processorHeight)
    tv.pollerBox = widgets.NewParagraph()
    tv.pollerBox.Title = "Network Poller"
    tv.pollerBox.SetRect(0, pollerStartY, termWidth, pollerStartY + 4)
    tv.pollerBox.BorderStyle.Fg = ui.ColorYellow

    // Event Box
    eventStartY := pollerStartY + 4
    tv.eventBox = widgets.NewList()
    tv.eventBox.Title = "Recent Events"
    tv.eventBox.SetRect(0, eventStartY, termWidth, termHeight)
    tv.eventBox.BorderStyle.Fg = ui.ColorMagenta

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

func (tv *TerminalVisualizer) Stop() {
    close(tv.stop)
    ui.Close()
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

    // Update Global Queue
    tv.updateGlobalQueue(stats)

    // Update Processors
    tv.updateProcessors(stats)

    // Update Network Poller
    tv.updateNetworkPoller(stats)

    // Render everything
    tv.render()
}

func (tv *TerminalVisualizer) updateGlobalQueue(stats scheduler.SchedulerStats) {
    gqStats := stats.GlobalQueueStats
    tv.globalQueueBox.Text = fmt.Sprintf(
        "Size: %d/%d\n"+
        "Tasks Scheduled: %d  Completed: %d\n"+
        "Utilization: %.1f%%",
        gqStats.CurrentSize,
        gqStats.Capacity,
        gqStats.Submitted,
        gqStats.Executed,
        gqStats.Utilization*100,
    )
}

func (tv *TerminalVisualizer) updateProcessors(stats scheduler.SchedulerStats) {
    for i, pStats := range stats.ProcessorMetrics {
        currentTask := "Idle"
        if pStats.CurrentTask != nil {
            currentTask = fmt.Sprintf("G%d (%s)",
                pStats.CurrentTask.ID(),
                pStats.State.String(),
            )
        }

        tv.processorBoxes[i].Text = fmt.Sprintf(
            "State: %s\n"+
            "Current: %s\n"+
            "Queue Size: %d\n"+
            "Steals (L/G): %d/%d",
            pStats.State,
            currentTask,
            pStats.QueueSize,
            pStats.LocalSteals,
            pStats.GlobalSteals,
        )
    }
}

func (tv *TerminalVisualizer) updateNetworkPoller(stats scheduler.SchedulerStats) {
    pollerMetrics := stats.PollerMetrics
    tv.pollerBox.Text = fmt.Sprintf(
        "Currently Blocked: %d\n"+
        "Completed: %d  Timeouts: %d  Errors: %d\n"+
        "Average Block Time: %v",
        pollerMetrics.CurrentlyBlocked,
        pollerMetrics.CompletedEvents,
        pollerMetrics.Timeouts,
        pollerMetrics.Errors,
        pollerMetrics.AverageBlockTime,
    )
}

func (tv *TerminalVisualizer) AddEvent(msg string) {
    tv.mu.Lock()
    defer tv.mu.Unlock()

    event := Event{
        Timestamp: time.Now(),
        Message:   msg,
    }

    // Add to front
    tv.events = append([]Event{event}, tv.events...)

    // Maintain max size
    if len(tv.events) > tv.maxEvents {
        tv.events = tv.events[:tv.maxEvents]
    }

    // Update display
    tv.updateEventDisplay()
}

func (tv *TerminalVisualizer) updateEventDisplay() {
    rows := make([]string, len(tv.events))
    now := time.Now()

    for i, event := range tv.events {
        duration := now.Sub(event.Timestamp)
        rows[i] = fmt.Sprintf("[%s ago] %s",
            formatDuration(duration),
            event.Message,
        )
    }

    tv.eventBox.Rows = rows
}

func (tv *TerminalVisualizer) render() {
    ui.Render(
        tv.globalQueueBox,
        tv.pollerBox,
        tv.eventBox,
    )

    for _, box := range tv.processorBoxes {
        ui.Render(box)
    }
}

func (tv *TerminalVisualizer) handleResize(width, height int) {
    tv.mu.Lock()
    defer tv.mu.Unlock()

    // Recalculate component dimensions
    tv.globalQueueBox.SetRect(0, 0, width, 4)

    processorHeight := 5
    processorsStartY := 4

    for i := range tv.processorBoxes {
        tv.processorBoxes[i].SetRect(
            0,
            processorsStartY + (i * processorHeight),
            width,
            processorsStartY + ((i + 1) * processorHeight),
        )
    }

    pollerStartY := processorsStartY + (len(tv.processorBoxes) * processorHeight)
    tv.pollerBox.SetRect(0, pollerStartY, width, pollerStartY + 4)

    eventStartY := pollerStartY + 4
    tv.eventBox.SetRect(0, eventStartY, width, height)

    tv.render()
}

func formatDuration(d time.Duration) string {
    if d < time.Second {
        return "now"
    }
    if d < time.Minute {
        return fmt.Sprintf("%ds", int(d.Seconds()))
    }
    return fmt.Sprintf("%dm%ds", int(d.Minutes()), int(d.Seconds())%60)
}
