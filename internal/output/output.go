// Package output provides human-friendly and machine-friendly output rendering.
// When --json is set, all output is pure JSON to stdout with no ANSI codes.
// When running in a TTY, styled tables and spinners are used.
package output

import (
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/lipgloss/table"
	"github.com/mattn/go-isatty"
	"github.com/muesli/termenv"
)

// Printer handles all output formatting decisions.
type Printer struct {
	JSON    bool
	Quiet   bool
	NoColor bool
	Out     io.Writer
	ErrOut  io.Writer
}

// Styles used for human output.
var (
	orange      = lipgloss.Color("#FF6B35")
	green       = lipgloss.Color("#00C853")
	red         = lipgloss.Color("#FF3D00")
	muted       = lipgloss.Color("#6C757D")
	headerStyle = lipgloss.NewStyle().Bold(true).Foreground(orange).Padding(0, 1)
	cellStyle   = lipgloss.NewStyle().Padding(0, 1)
	labelStyle  = lipgloss.NewStyle().Bold(true).Foreground(orange)
	valueStyle  = lipgloss.NewStyle()
	okStyle     = lipgloss.NewStyle().Foreground(green).Bold(true)
	errStyle    = lipgloss.NewStyle().Foreground(red).Bold(true)
	dimStyle    = lipgloss.NewStyle().Foreground(muted)
)

// New constructs a Printer based on flag values.
// JSON mode implies quiet and disables colors.
func New(jsonFlag, quietFlag, noColorFlag bool) *Printer {
	noColor := noColorFlag || jsonFlag
	if noColor || !isatty.IsTerminal(os.Stdout.Fd()) {
		lipgloss.SetColorProfile(termenv.Ascii)
	}
	return &Printer{
		JSON:    jsonFlag,
		Quiet:   quietFlag || jsonFlag,
		NoColor: noColor,
		Out:     os.Stdout,
		ErrOut:  os.Stderr,
	}
}

// PrintJSON emits v as indented JSON to stdout regardless of mode.
func (p *Printer) PrintJSON(v any) {
	enc := json.NewEncoder(p.Out)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		fmt.Fprintf(p.ErrOut, "error: could not encode JSON: %v\n", err)
	}
}

// Info prints a progress/info line to stderr. Suppressed in JSON/quiet mode.
func (p *Printer) Info(format string, args ...any) {
	if p.Quiet {
		return
	}
	fmt.Fprintf(p.ErrOut, dimStyle.Render(fmt.Sprintf(format, args...))+"\n")
}

// Success prints a success message to stderr. Suppressed in JSON/quiet mode.
func (p *Printer) Success(format string, args ...any) {
	if p.Quiet {
		return
	}
	msg := fmt.Sprintf(format, args...)
	fmt.Fprintf(p.ErrOut, okStyle.Render("✓ ")+msg+"\n")
}

// Error always prints to stderr with an error prefix.
func (p *Printer) Error(format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	fmt.Fprintf(p.ErrOut, errStyle.Render("✗ error: ")+msg+"\n")
}

// Table renders rows with the given headers as a lipgloss table.
func (p *Printer) Table(headers []string, rows [][]string) {
	t := table.New().
		Border(lipgloss.NormalBorder()).
		BorderStyle(lipgloss.NewStyle().Foreground(muted)).
		StyleFunc(func(row, _ int) lipgloss.Style {
			if row == table.HeaderRow {
				return headerStyle
			}
			return cellStyle
		}).
		Headers(headers...).
		Rows(rows...)
	fmt.Fprintln(p.Out, t)
}

// KV renders a set of key/value pairs as a vertical list.
func (p *Printer) KV(pairs [][2]string) {
	maxKey := 0
	for _, kv := range pairs {
		if len(kv[0]) > maxKey {
			maxKey = len(kv[0])
		}
	}
	for _, kv := range pairs {
		key := fmt.Sprintf("%-*s", maxKey, kv[0])
		fmt.Fprintf(p.Out, "%s  %s\n", labelStyle.Render(key), valueStyle.Render(kv[1]))
	}
}
