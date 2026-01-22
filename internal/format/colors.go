package format

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/fatih/color"
)

var (
	Green  = color.New(color.FgGreen).SprintFunc()
	Red    = color.New(color.FgRed).SprintFunc()
	Yellow = color.New(color.FgYellow).SprintFunc()
	Bold   = color.New(color.Bold).SprintFunc()
	Dim    = color.New(color.Faint).SprintFunc()
)

var ansiRegex = regexp.MustCompile(`\x1b\[[0-9;]*[a-zA-Z]`)

// stripANSI removes ANSI escape codes to get actual visible length
func stripANSI(str string) string {
	return ansiRegex.ReplaceAllString(str, "")
}

// padRight pads a colored string to ensure it displays at the specified width
func padRight(str string, width int) string {
	visibleLen := len(stripANSI(str))
	if visibleLen < width {
		return str + strings.Repeat(" ", width-visibleLen)
	}
	return str
}

func ColorLatency(ms int64) string {
	switch {
	case ms < 100:
		return Green(fmt.Sprintf("%dms", ms))
	case ms < 300:
		return Yellow(fmt.Sprintf("%dms", ms))
	default:
		return Red(fmt.Sprintf("%dms", ms))
	}
}

func ColorLag(lag uint64) string {
	if lag == 0 {
		return Dim("—")
	}
	if lag <= 1 {
		return Yellow(fmt.Sprintf("-%d", lag))
	}
	return Red(fmt.Sprintf("-%d", lag))
}

func ColorSuccess(success, total int) string {
	pct := float64(success) / float64(total) * 100
	str := fmt.Sprintf("%.0f%%", pct)
	switch {
	case pct >= 100:
		return Green(str)
	case pct >= 80:
		return Yellow(str)
	default:
		return Red(str)
	}
}
