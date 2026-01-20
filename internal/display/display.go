// Package display contains terminal formatting logic for CLI commands.
//
// Commands should keep parsing and business logic separate from rendering concerns by
// delegating all human-readable output to formatters in this package.
package display

import "io"

const (
	ClearScreen = "\033[2J\033[H"

	ColorReset  = "\033[0m"
	ColorRed    = "\033[31m"
	ColorGreen  = "\033[32m"
	ColorYellow = "\033[33m"
)

// Formatter writes formatted output to a writer.
type Formatter interface {
	Format(w io.Writer) error
}

// Clear writes ANSI clear screen sequence to w.
func Clear(w io.Writer) {
	_, _ = io.WriteString(w, ClearScreen)
}
