package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/vinicgobbi/git-sync/internal/config"
)

// Paleta: as cores ANSI do terminal (como no deploy-tools), que seguem o
// tema claro ou escuro escolhido pelo usuário.
var (
	colAccent = lipgloss.Color("12")
	colFaint  = lipgloss.Color("8")
	colOK     = lipgloss.Color("10")
	colErr    = lipgloss.Color("9")
	colWarn   = lipgloss.Color("11")
	colNotice = lipgloss.Color("14")
)

var (
	sAccent   = lipgloss.NewStyle().Foreground(colAccent)
	sBold     = lipgloss.NewStyle().Bold(true)
	sTitle    = lipgloss.NewStyle().Bold(true).Foreground(colAccent)
	sSelected = sTitle
	sMuted    = lipgloss.NewStyle().Faint(true)
	sOK       = lipgloss.NewStyle().Foreground(colOK)
	sErr      = lipgloss.NewStyle().Foreground(colErr)
	sWarn     = lipgloss.NewStyle().Foreground(colWarn)
	sNotice   = lipgloss.NewStyle().Foreground(colNotice)
	sKey      = lipgloss.NewStyle().Foreground(colAccent).Bold(true)
	sLabel    = lipgloss.NewStyle().Faint(true).Width(16)
	sSection  = lipgloss.NewStyle().Bold(true).MarginTop(1)

	sCard = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(colFaint).Padding(0, 1)

	// caixas do menu e do painel; a de quem tem o foco fica na cor de destaque
	sBox       = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(colFaint).Padding(0, 1)
	sBoxActive = sBox.BorderForeground(colAccent)

	// botões de modais e formulários: o focado em vídeo reverso
	sButton      = lipgloss.NewStyle().Padding(0, 2).Faint(true)
	sButtonFocus = lipgloss.NewStyle().Padding(0, 2).Bold(true).Reverse(true)
)

// row marca a linha selecionada em listas.
func row(s string, selected bool, width int) string {
	if !selected {
		return "  " + truncate(s, width-2)
	}
	return sSelected.Render("▸ ") + truncate(s, width-2)
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

// clip corta cada linha na largura w e completa ou corta até h linhas, para
// o conteúdo não estourar a caixa onde vai ser desenhado.
func clip(s string, w, h int) string {
	lines := strings.Split(fitHeight(s, h), "\n")
	for i, l := range lines {
		lines[i] = truncate(l, w)
	}
	return strings.Join(lines, "\n")
}

// keyHint formata "tecla descrição" para textos de ajuda embutidos.
func keyHint(k, desc string) string { return sKey.Render(k) + " " + sMuted.Render(desc) }
