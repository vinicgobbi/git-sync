package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/vinicgobbi/git-sync/internal/config"
)

// Paleta: cores adaptativas para terminais claros e escuros.
var (
	colAccent   = lipgloss.AdaptiveColor{Light: "#5B3CC4", Dark: "#A78BFA"}
	colText     = lipgloss.AdaptiveColor{Light: "#1F2937", Dark: "#E5E7EB"}
	colMuted    = lipgloss.AdaptiveColor{Light: "#6B7280", Dark: "#9CA3AF"}
	colFaint    = lipgloss.AdaptiveColor{Light: "#D1D5DB", Dark: "#3F3F46"}
	colSelectBg = lipgloss.AdaptiveColor{Light: "#EDE9FE", Dark: "#2E2A47"}
	colOK       = lipgloss.AdaptiveColor{Light: "#15803D", Dark: "#4ADE80"}
	colErr      = lipgloss.AdaptiveColor{Light: "#B91C1C", Dark: "#F87171"}
	colWarn     = lipgloss.AdaptiveColor{Light: "#A16207", Dark: "#FACC15"}
)

var (
	sAccent  = lipgloss.NewStyle().Foreground(colAccent)
	sBold    = lipgloss.NewStyle().Bold(true)
	sTitle   = lipgloss.NewStyle().Bold(true).Foreground(colAccent)
	sMuted   = lipgloss.NewStyle().Foreground(colMuted)
	sOK      = lipgloss.NewStyle().Foreground(colOK)
	sErr     = lipgloss.NewStyle().Foreground(colErr)
	sWarn    = lipgloss.NewStyle().Foreground(colWarn)
	sKey     = lipgloss.NewStyle().Foreground(colAccent).Bold(true)
	sLabel   = lipgloss.NewStyle().Foreground(colMuted).Width(16)
	sSection = lipgloss.NewStyle().Bold(true).Foreground(colText).MarginTop(1)

	sCard = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(colFaint).Padding(0, 1)

	sTabActive   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#FFFFFF")).Background(colAccent).Padding(0, 1)
	sTabInactive = lipgloss.NewStyle().Foreground(colMuted).Padding(0, 1)
)

// row destaca a linha selecionada em listas.
func row(s string, selected bool, width int) string {
	if !selected {
		return "  " + truncate(s, width-2)
	}
	line := sAccent.Bold(true).Render("▌ ") + truncate(s, width-2)
	pad := width - ansi.StringWidth(line)
	if pad > 0 {
		line += strings.Repeat(" ", pad)
	}
	return lipgloss.NewStyle().Background(colSelectBg).Render(line)
}

func kindBadge(k config.Kind) string {
	var bg, label string
	switch k {
	case config.KindGitea:
		bg, label = "#609926", "Gitea"
	case config.KindForgejo:
		bg, label = "#C2410C", "Forgejo"
	case config.KindGitHub:
		bg, label = "#24292F", "GitHub"
	default:
		return sMuted.Render(string(k))
	}
	return lipgloss.NewStyle().Foreground(lipgloss.Color("#FFFFFF")).Background(lipgloss.Color(bg)).Padding(0, 1).Render(label)
}

func kindName(k config.Kind) string {
	switch k {
	case config.KindGitea:
		return "Gitea"
	case config.KindForgejo:
		return "Forgejo"
	case config.KindGitHub:
		return "GitHub"
	}
	return string(k)
}

func checkbox(on bool) string {
	if on {
		return sAccent.Render("◉")
	}
	return sMuted.Render("○")
}

func truncate(s string, w int) string {
	if w <= 0 {
		return ""
	}
	return ansi.Truncate(s, w, "…")
}

// padRight completa com espaços até a largura (respeitando códigos ANSI).
func padRight(s string, w int) string {
	s = truncate(s, w)
	if d := w - ansi.StringWidth(s); d > 0 {
		s += strings.Repeat(" ", d)
	}
	return s
}

func oneLine(s string) string { return strings.Join(strings.Fields(s), " ") }

func wrap(s string, w int) string {
	if w < 10 {
		w = 10
	}
	return ansi.Wordwrap(s, w, " ")
}

// relTime descreve há quanto tempo algo aconteceu, em português.
func relTime(t time.Time) string {
	if t.IsZero() {
		return "nunca"
	}
	d := time.Since(t)
	switch {
	case d < 0:
		return "agora"
	case d < time.Minute:
		return "agora há pouco"
	case d < time.Hour:
		return fmt.Sprintf("há %d min", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("há %d h", int(d.Hours()))
	case d < 30*24*time.Hour:
		return fmt.Sprintf("há %d dias", int(d.Hours()/24))
	}
	return t.Local().Format("02/01/2006")
}

func plural(n int, one, many string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, one)
	}
	return fmt.Sprintf("%d %s", n, many)
}

// fitHeight corta ou completa o texto para ter exatamente h linhas.
func fitHeight(s string, h int) string {
	lines := strings.Split(s, "\n")
	if len(lines) > h {
		lines = lines[:h]
	}
	for len(lines) < h {
		lines = append(lines, "")
	}
	return strings.Join(lines, "\n")
}

// overlay desenha fg centralizado sobre bg (ambos com largura w e altura h).
func overlay(bg, fg string, w, h int) string {
	bgLines := strings.Split(fitHeight(bg, h), "\n")
	fgLines := strings.Split(fg, "\n")
	fw := lipgloss.Width(fg)
	x, y := max((w-fw)/2, 0), max((h-len(fgLines))/2, 0)
	for i, fl := range fgLines {
		if y+i >= len(bgLines) {
			break
		}
		bl := padRight(bgLines[y+i], w)
		bgLines[y+i] = ansi.Truncate(bl, x, "") + padRight(fl, fw) + ansi.TruncateLeft(bl, x+fw, "")
	}
	return strings.Join(bgLines, "\n")
}

// centered posiciona um bloco no meio da área.
func centered(s string, w, h int) string {
	return lipgloss.Place(w, h, lipgloss.Center, lipgloss.Center, s)
}

// keyHint formata "tecla descrição" para textos de ajuda embutidos.
func keyHint(k, desc string) string { return sKey.Render(k) + " " + sMuted.Render(desc) }
