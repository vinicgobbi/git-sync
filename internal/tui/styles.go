package tui

import (
	"strings"

	"github.com/charmbracelet/bubbles/table"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

var (
	colAccent = lipgloss.AdaptiveColor{Light: "#5B3CC4", Dark: "#A78BFA"}
	colSubtle = lipgloss.AdaptiveColor{Light: "#6B7280", Dark: "#9CA3AF"}
	colOK     = lipgloss.AdaptiveColor{Light: "#15803D", Dark: "#4ADE80"}
	colErr    = lipgloss.AdaptiveColor{Light: "#B91C1C", Dark: "#F87171"}
	colWarn   = lipgloss.AdaptiveColor{Light: "#A16207", Dark: "#FACC15"}

	titleStyle  = lipgloss.NewStyle().Bold(true).Foreground(colAccent)
	crumbStyle  = lipgloss.NewStyle().Foreground(colSubtle)
	helpStyle   = lipgloss.NewStyle().Foreground(colSubtle)
	okStyle     = lipgloss.NewStyle().Foreground(colOK)
	errStyle    = lipgloss.NewStyle().Foreground(colErr)
	warnStyle   = lipgloss.NewStyle().Foreground(colWarn)
	cursorStyle = lipgloss.NewStyle().Foreground(colAccent).Bold(true)
	boldStyle   = lipgloss.NewStyle().Bold(true)
	labelStyle  = lipgloss.NewStyle().Foreground(colSubtle).Width(18)
)

func newTable(cols []table.Column) table.Model {
	t := table.New(table.WithColumns(cols), table.WithFocused(true))
	s := table.DefaultStyles()
	s.Header = s.Header.BorderStyle(lipgloss.NormalBorder()).BorderBottom(true).
		BorderForeground(colSubtle).Bold(true)
	s.Selected = s.Selected.Foreground(lipgloss.Color("#FFFFFF")).Background(colAccent).Bold(false)
	t.SetStyles(s)
	return t
}

// fitColumns distribui a largura total: colunas com Width 0 dividem a sobra.
func fitColumns(cols []table.Column, total int) []table.Column {
	out := make([]table.Column, len(cols))
	copy(out, cols)
	fixed, flex := 0, 0
	for _, c := range out {
		if c.Width == 0 {
			flex++
		}
		fixed += c.Width + 2 // padding das células
	}
	if flex == 0 {
		return out
	}
	rest := (total - fixed - 2*flex) / flex
	if rest < 10 {
		rest = 10
	}
	for i := range out {
		if out[i].Width == 0 {
			out[i].Width = rest
		}
	}
	return out
}

func truncate(s string, w int) string { return ansi.Truncate(s, w, "…") }

func oneLine(s string) string { return strings.Join(strings.Fields(s), " ") }

func checkbox(on bool) string {
	if on {
		return "[x]"
	}
	return "[ ]"
}

// setRows troca as linhas mantendo o cursor válido: o bubbles deixa o cursor
// em -1 quando recebe zero linhas e não o corrige quando linhas voltam.
func setRows(t *table.Model, rows []table.Row) {
	t.SetRows(rows)
	if t.Cursor() < 0 && len(rows) > 0 {
		t.SetCursor(0)
	}
}
