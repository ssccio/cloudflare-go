// Package output provides human-friendly and machine-friendly output rendering.
// When --json is set, all output is pure JSON to stdout with no ANSI codes.
// When --toon is set, output is Token-Oriented Object Notation (30-60% fewer tokens than JSON).
// When running in a TTY, styled tables and spinners are used.
package output

import (
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/lipgloss/table"
	"github.com/jmespath/go-jmespath"
	"github.com/mattn/go-isatty"
	"github.com/muesli/termenv"
	toonlib "github.com/toon-format/toon-go"
)

// Printer handles all output formatting decisions.
type Printer struct {
	JSON    bool
	TOON    bool
	Quiet   bool
	NoColor bool
	Query   string
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
// JSON and TOON modes imply quiet and disable colors.
func New(jsonFlag, toonFlag, quietFlag, noColorFlag bool, query string) *Printer {
	machineMode := jsonFlag || toonFlag
	noColor := noColorFlag || machineMode
	if noColor || !isatty.IsTerminal(os.Stdout.Fd()) {
		lipgloss.SetColorProfile(termenv.Ascii)
	}
	return &Printer{
		JSON:    jsonFlag,
		TOON:    toonFlag,
		Quiet:   quietFlag || machineMode,
		NoColor: noColor,
		Query:   query,
		Out:     os.Stdout,
		ErrOut:  os.Stderr,
	}
}

// PrintJSON emits v as indented JSON to stdout.
func (p *Printer) PrintJSON(v any) {
	enc := json.NewEncoder(p.Out)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		fmt.Fprintf(p.ErrOut, "error: could not encode JSON: %v\n", err)
	}
}

// PrintTOON emits v as Token-Oriented Object Notation to stdout.
// TOON is 30-60% more token-efficient than JSON for LLM consumption.
func (p *Printer) PrintTOON(v any) {
	s, err := toonlib.MarshalString(v, toonlib.WithLengthMarkers(true))
	if err != nil {
		fmt.Fprintf(p.ErrOut, "error: could not encode TOON: %v\n", err)
		return
	}
	fmt.Fprint(p.Out, s)
}

// PrintResult outputs v as JSON or TOON, applying --query (JMESPath) if set.
// Commands should call this instead of PrintJSON/PrintTOON directly.
func (p *Printer) PrintResult(v any) {
	result := v
	if p.Query != "" {
		filtered, err := applyJMESPath(v, p.Query)
		if err != nil {
			fmt.Fprintf(p.ErrOut, "error: invalid --query expression: %v\n", err)
			return
		}
		result = filtered
	}
	if p.JSON {
		p.PrintJSON(result)
	} else if p.TOON {
		p.PrintTOON(result)
	}
}

// applyJMESPath evaluates a JMESPath expression against v.
// v is round-tripped through JSON so struct tags are respected.
func applyJMESPath(v any, expr string) (any, error) {
	data, err := json.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("marshalling for query: %w", err)
	}
	var generic any
	if err := json.Unmarshal(data, &generic); err != nil {
		return nil, fmt.Errorf("parsing for query: %w", err)
	}
	result, err := jmespath.Search(expr, generic)
	if err != nil {
		return nil, err
	}
	return result, nil
}

// Info prints a progress/info line to stderr. Suppressed in machine modes.
func (p *Printer) Info(format string, args ...any) {
	if p.Quiet {
		return
	}
	fmt.Fprintf(p.ErrOut, dimStyle.Render(fmt.Sprintf(format, args...))+"\n")
}

// Success prints a success message to stderr. Suppressed in machine modes.
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
