package visualization

import (
	"fmt"
	"workstealing/internal/core"

	"github.com/gizak/termui/v3"
)

// ColorScheme defines the complete color palette for visualization
var ColorScheme = struct {
    // Processor state colors
    ProcessorIdle     termui.Color
    ProcessorRunning  termui.Color
    ProcessorStealing termui.Color

    // Performance indicator colors
    Success          termui.Color
    Warning          termui.Color
    Error            termui.Color

    // UI element colors
    Text             termui.Color
    Border           termui.Color
    Background       termui.Color
    HeaderText       termui.Color
    GraphAxis        termui.Color
    GraphLine        termui.Color

    // Queue status colors
    QueueLow         termui.Color
    QueueMedium      termui.Color
    QueueHigh        termui.Color
}{
    // Processor states
    ProcessorIdle:     termui.ColorBlue,
    ProcessorRunning:  termui.ColorGreen,
    ProcessorStealing: termui.ColorYellow,

    // Performance indicators
    Success:          termui.ColorGreen,
    Warning:          termui.ColorYellow,
    Error:            termui.ColorRed,

    // UI elements
    Text:             termui.ColorWhite,
    Border:           termui.ColorWhite,
    Background:       termui.ColorClear,
    HeaderText:       termui.ColorCyan,
    GraphAxis:        termui.ColorWhite,
    GraphLine:        termui.ColorGreen,

    // Queue status
    QueueLow:         termui.ColorGreen,
    QueueMedium:      termui.ColorYellow,
    QueueHigh:        termui.ColorRed,
}

// Common style presets for consistent UI appearance
var (
    DefaultStyle = Style(ColorScheme.Text, ColorScheme.Background)
    HeaderStyle  = Style(ColorScheme.HeaderText, ColorScheme.Background, termui.ModifierBold)
    BorderStyle  = Style(ColorScheme.Border, ColorScheme.Background)
    ErrorStyle   = Style(ColorScheme.Error, ColorScheme.Background, termui.ModifierBold)
    SuccessStyle = Style(ColorScheme.Success, ColorScheme.Background)
    WarningStyle = Style(ColorScheme.Warning, ColorScheme.Background)
)

// Style creates a new termui style with specified colors and modifiers
func Style(fg, bg termui.Color, modifiers ...termui.Modifier) termui.Style {
    style := termui.NewStyle(fg, bg)
    for _, mod := range modifiers {
        style.Modifier |= mod
    }
    return style
}

// getStateColor returns the appropriate color for a processor state
func getStateColor(state core.ProcessorState) termui.Color {
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

// getQueueColor returns a color based on queue utilization
func getQueueColor(utilization float64) termui.Color {
    switch {
    case utilization < 0.3:
        return ColorScheme.QueueLow
    case utilization < 0.7:
        return ColorScheme.QueueMedium
    default:
        return ColorScheme.QueueHigh
    }
}

// getLoadColor returns a color based on processor load
func getLoadColor(load float64) termui.Color {
    switch {
    case load < 0.3:
        return ColorScheme.ProcessorIdle
    case load < 0.7:
        return ColorScheme.ProcessorRunning
    default:
        return ColorScheme.ProcessorStealing
    }
}

// FormatWithColor wraps text with color codes
func FormatWithColor(text string, color termui.Color) string {
    return fmt.Sprintf("[%s](fg:%v)", text, color)
}

// FormatMetric formats a metric with appropriate color based on value
func FormatMetric(name string, value float64, threshold float64) string {
    var color termui.Color
    if value > threshold {
        color = ColorScheme.Warning
    } else {
        color = ColorScheme.Success
    }
    return fmt.Sprintf("%s: [%0.2f](fg:%v)", name, value, color)
}
