package visualization

import (
	"fmt"
	"sync"
	"time"
	"workstealing/internal/core"
	"workstealing/internal/scheduler"

	"github.com/gizak/termui/v3"
	"github.com/gizak/termui/v3/widgets"
)

type TerminalVisualizer struct {
	scheduler      *scheduler.Scheduler
	updateInterval time.Duration
	mu             sync.RWMutex
	stop           chan struct{}

	// UI components
	processorGauges []*widgets.Gauge
	statsTable      *widgets.Table
	taskGraph       *widgets.Plot
	stealGraph      *widgets.Plot
	statusBox       *widgets.Paragraph
	queueBox        *widgets.Paragraph

	// Historical data
	taskHistory   []float64
	stealHistory  []float64
	globalHistory []float64
	maxQueueSize  int
}

func NewTerminalVisualizer(s *scheduler.Scheduler, interval time.Duration) (*TerminalVisualizer, error) {
	if err := termui.Init(); err != nil {
		return nil, fmt.Errorf("failed to initialize terminal UI: %v", err)
	}

	tv := &TerminalVisualizer{
		scheduler:      s,
		updateInterval: interval,
		stop:           make(chan struct{}),
		taskHistory:    make([]float64, 100),
		stealHistory:   make([]float64, 100),
		globalHistory:  make([]float64, 100),
		maxQueueSize:   100,
	}

	stats := s.GetStats()
	if err := tv.initializeComponents(len(stats.ProcessorMetrics)); err != nil {
		termui.Close()
		return nil, fmt.Errorf("failed to initialize components: %v", err)
	}

	return tv, nil
}

func (tv *TerminalVisualizer) initializeComponents(numProcessors int) error {
	termWidth, termHeight := termui.TerminalDimensions()
	gaugeHeight := 3
	totalGaugeHeight := numProcessors * gaugeHeight

	// Initialize processor gauges
	tv.processorGauges = make([]*widgets.Gauge, numProcessors)
	for i := range tv.processorGauges {
		tv.processorGauges[i] = widgets.NewGauge()
		tv.processorGauges[i].Title = fmt.Sprintf("Processor %d", i)
		tv.processorGauges[i].BarColor = getStateColor(core.ProcessorIdle)
		tv.processorGauges[i].BorderStyle = BorderStyle
		tv.processorGauges[i].TitleStyle = HeaderStyle
		tv.processorGauges[i].SetRect(0, i*gaugeHeight, termWidth/2, (i*gaugeHeight)+3)
	}

	// Initialize statistics table
	tv.statsTable = widgets.NewTable()
	tv.statsTable.Title = "Scheduler Statistics"
	tv.statsTable.BorderStyle = BorderStyle
	tv.statsTable.TitleStyle = HeaderStyle
	tv.statsTable.TextStyle = DefaultStyle
	tv.statsTable.RowSeparator = true
	tv.statsTable.SetRect(termWidth/2, 0, termWidth, totalGaugeHeight)
	tv.statsTable.Rows = [][]string{
		{"Metric", "Value"},
		{"Tasks Scheduled", "0"},
		{"Tasks Completed", "0"},
		{"Total Steals", "0"},
		{"Running Time", "0s"},
	}

	// Initialize task distribution graph
	tv.taskGraph = widgets.NewPlot()
	tv.taskGraph.Title = "Task Distribution"
	tv.taskGraph.BorderStyle = BorderStyle
	tv.taskGraph.TitleStyle = HeaderStyle
	tv.taskGraph.AxesColor = ColorScheme.GraphAxis
	tv.taskGraph.LineColors = []termui.Color{ColorScheme.Success, ColorScheme.Warning}
	tv.taskGraph.Data = [][]float64{tv.taskHistory}
	tv.taskGraph.SetRect(0, totalGaugeHeight, termWidth/2, termHeight-totalGaugeHeight/2)

	// Initialize steal metrics graph
	tv.stealGraph = widgets.NewPlot()
	tv.stealGraph.Title = "Steal Activity"
	tv.stealGraph.BorderStyle = BorderStyle
	tv.stealGraph.TitleStyle = HeaderStyle
	tv.stealGraph.AxesColor = ColorScheme.GraphAxis
	tv.stealGraph.LineColors = []termui.Color{ColorScheme.Warning, ColorScheme.Error}
	tv.stealGraph.Data = [][]float64{tv.stealHistory, tv.globalHistory}
	tv.stealGraph.SetRect(0, termHeight-totalGaugeHeight/2, termWidth/2, termHeight)

	// Initialize status box
	tv.statusBox = widgets.NewParagraph()
	tv.statusBox.Title = "Processor Status"
	tv.statusBox.BorderStyle = BorderStyle
	tv.statusBox.TitleStyle = HeaderStyle
	tv.statusBox.TextStyle = DefaultStyle
	tv.statusBox.SetRect(termWidth/2, totalGaugeHeight, termWidth, termHeight-totalGaugeHeight/2)

	// Initialize queue box
	tv.queueBox = widgets.NewParagraph()
	tv.queueBox.Title = "Queue Status"
	tv.queueBox.BorderStyle = BorderStyle
	tv.queueBox.TitleStyle = HeaderStyle
	tv.queueBox.TextStyle = DefaultStyle
	tv.queueBox.SetRect(termWidth/2, termHeight-totalGaugeHeight/2, termWidth, termHeight)

	return nil
}

func (tv *TerminalVisualizer) Start() {
	go tv.updateLoop()

	uiEvents := termui.PollEvents()
	for {
		select {
		case e := <-uiEvents:
			switch e.ID {
			case "q", "<C-c>":
				tv.Stop()
				return
			case "<Resize>":
				payload := e.Payload.(termui.Resize)
				tv.handleResize(payload.Width, payload.Height)
			}
		case <-tv.stop:
			return
		}
	}
}

func (tv *TerminalVisualizer) Stop() {
	close(tv.stop)
	termui.Close()
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

	// Update processor gauges and status
	for i, metrics := range stats.ProcessorMetrics {
		tv.updateProcessorGauge(i, metrics)
	}

	// Update statistics
	tv.updateStatsTable(stats)

	// Update graphs
	tv.updateGraphs(stats)

	// Update status boxes
	tv.updateStatusBox(stats)
	tv.updateQueueBox(stats)

	tv.render()
}

func (tv *TerminalVisualizer) updateProcessorGauge(index int, metrics core.ProcessorMetricsData) {
	gauge := tv.processorGauges[index]
	utilization := int((float64(metrics.TasksExecuted) / float64(tv.maxQueueSize)) * 100)
	if utilization > 100 {
		utilization = 100
	}

	gauge.Percent = utilization
	gauge.Label = fmt.Sprintf("[Steals L/G: %d/%d](fg:yellow) [Tasks: %d](fg:green)",
		metrics.LocalQueueSteals,
		metrics.GlobalQueueSteals,
		metrics.TasksExecuted)
}

func (tv *TerminalVisualizer) updateStatsTable(stats scheduler.SchedulerStats) {
	tv.statsTable.Rows = [][]string{
		{"Metric", "Value"},
		{"Tasks Scheduled", fmt.Sprintf("%d", stats.TasksScheduled)},
		{"Tasks Completed", fmt.Sprintf("%d", stats.TasksCompleted)},
		{"Local Steals", fmt.Sprintf("%d", stats.LocalQueueSteals)},
		{"Global Steals", fmt.Sprintf("%d", stats.GlobalQueueSteals)},
		{"Running Time", stats.RunningTime.String()},
	}
}

func (tv *TerminalVisualizer) updateGraphs(stats scheduler.SchedulerStats) {
	// Update task history
	copy(tv.taskHistory[1:], tv.taskHistory)
	tv.taskHistory[0] = float64(stats.TasksCompleted)

	// Update steal history
	copy(tv.stealHistory[1:], tv.stealHistory)
	tv.stealHistory[0] = float64(stats.LocalQueueSteals)

	// Update global steal history
	copy(tv.globalHistory[1:], tv.globalHistory)
	tv.globalHistory[0] = float64(stats.GlobalQueueSteals)

	tv.taskGraph.Data = [][]float64{tv.taskHistory}
	tv.stealGraph.Data = [][]float64{tv.stealHistory, tv.globalHistory}
}

func (tv *TerminalVisualizer) updateStatusBox(stats scheduler.SchedulerStats) {
	var totalIdleTime, totalRunningTime time.Duration
	for _, pm := range stats.ProcessorMetrics {
		totalIdleTime += pm.TotalIdleTime
		totalRunningTime += pm.TotalRunningTime
	}

	tv.statusBox.Text = fmt.Sprintf(
		"Processors: %d\n"+
			"Total Idle Time: %v\n"+
			"Total Running Time: %v\n"+
			"Tasks Completed: %d\n"+
			"Steal Ratio: %.2f%%",
		len(stats.ProcessorMetrics),
		totalIdleTime,
		totalRunningTime,
		stats.TasksCompleted,
		float64(stats.TotalSteals)/float64(stats.TasksCompleted)*100,
	)
}

func (tv *TerminalVisualizer) updateQueueBox(stats scheduler.SchedulerStats) {
	gqs := stats.GlobalQueueStats
	tv.queueBox.Text = fmt.Sprintf(
		"Global Queue:\n"+
			"Size: %d/%d\n"+
			"Submitted: %d\n"+
			"Rejected: %d\n"+
			"Utilization: %.2f%%",
		gqs.CurrentSize,
		gqs.Capacity,
		gqs.Submitted,
		gqs.Rejected,
		gqs.Utilization*100,
	)
}

func (tv *TerminalVisualizer) render() {
	termui.Clear()

	// Render all components
	for _, gauge := range tv.processorGauges {
		termui.Render(gauge)
	}

	termui.Render(
		tv.statsTable,
		tv.taskGraph,
		tv.stealGraph,
		tv.statusBox,
		tv.queueBox,
	)
}

func (tv *TerminalVisualizer) handleResize(width, height int) {
	if width <= 0 || height <= 0 {
		return
	}

	tv.mu.Lock()
	defer tv.mu.Unlock()

	gaugeHeight := 3
	numProcessors := len(tv.processorGauges)
	totalGaugeHeight := numProcessors * gaugeHeight

	// Resize processor gauges
	for i, gauge := range tv.processorGauges {
		gauge.SetRect(0, i*gaugeHeight, width/2, (i*gaugeHeight)+3)
	}

	// Resize stats table
	tv.statsTable.SetRect(width/2, 0, width, totalGaugeHeight)

	// Resize task graph
	tv.taskGraph.SetRect(0, totalGaugeHeight, width/2, height-totalGaugeHeight/2)

	// Resize steal graph
	tv.stealGraph.SetRect(0, height-totalGaugeHeight/2, width/2, height)

	// Resize status box
	tv.statusBox.SetRect(width/2, totalGaugeHeight, width, height-totalGaugeHeight/2)

	// Resize queue box
	tv.queueBox.SetRect(width/2, height-totalGaugeHeight/2, width, height)

	tv.render()
}
