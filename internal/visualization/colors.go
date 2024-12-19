package visualization

import (
	"workstealing/internal/core"

	ui "github.com/gizak/termui/v3"
)

var ColorScheme = struct {
	// State colors
	ProcessorIdle     ui.Color
	ProcessorRunning  ui.Color
	ProcessorStealing ui.Color

	// Queue colors
	QueueLow    ui.Color
	QueueMedium ui.Color
	QueueHigh   ui.Color

	// UI elements
	HeaderText ui.Color
	Border     ui.Color
	Text       ui.Color
	GraphLine  ui.Color
	GraphAxis  ui.Color
	Success    ui.Color
	Warning    ui.Color
	Error      ui.Color
}{
	ProcessorIdle:     ui.ColorBlue,
	ProcessorRunning:  ui.ColorGreen,
	ProcessorStealing: ui.ColorYellow,

	QueueLow:    ui.ColorGreen,
	QueueMedium: ui.ColorYellow,
	QueueHigh:   ui.ColorRed,

	HeaderText: ui.ColorCyan,
	Border:     ui.ColorWhite,
	Text:       ui.ColorWhite,
	GraphLine:  ui.ColorGreen,
	GraphAxis:  ui.ColorWhite,
	Success:    ui.ColorGreen,
	Warning:    ui.ColorYellow,
	Error:      ui.ColorRed,
}

func getStateColor(state core.ProcessorState) ui.Color {
	switch state {
	case core.ProcessorIdle:
		return ColorScheme.ProcessorIdle
	case core.ProcessorRunning:
		return ColorScheme.ProcessorRunning
	case core.ProcessorStealing:
		return ColorScheme.ProcessorStealing
	default:
		return ColorScheme.Text
	}
}

func getQueueColor(utilization float64) ui.Color {
	switch {
	case utilization < 0.5:
		return ColorScheme.QueueLow
	case utilization < 0.8:
		return ColorScheme.QueueMedium
	default:
		return ColorScheme.QueueHigh
	}
}

func getLoadColor(load float64) ui.Color {
	switch {
	case load < 0.3:
		return ColorScheme.ProcessorIdle
	case load < 0.7:
		return ColorScheme.ProcessorRunning
	default:
		return ColorScheme.ProcessorStealing
	}
}
