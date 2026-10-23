package tui

import (
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// modal é uma confirmação sim/não desenhada por cima da tela.
// Por segurança, o botão inicial é sempre "Cancelar".
type modal struct {
	msg confirmMsg
	yes bool // botão selecionado
}

func newModal(msg confirmMsg) *modal { return &modal{msg: msg} }

func (m *modal) handle(k tea.KeyMsg) (done bool, cmd tea.Cmd) {
	switch k.String() {
	case "left", "right", "tab", "shift+tab", "h", "l":
		m.yes = !m.yes
	case "y", "s":
		return true, m.msg.onYes
	case "n", "esc", "q":
		return true, nil
	case "enter":
		if m.yes {
			return true, m.msg.onYes
		}
		return true, nil
	}
	return false, nil
}

func (m *modal) view(w int) string {
	border := colAccent
	if m.msg.danger {
		border = colErr
	}
	cancel, yes := sButton.Render("Cancelar"), sButton.Render(m.msg.yes)
	if m.yes {
		yes = sButtonFocus.Foreground(border).Render(m.msg.yes)
	} else {
		cancel = sButtonFocus.Render("Cancelar")
	}
	inner := w - 6
	content := sBold.Render(m.msg.title) + "\n\n" + wrap(m.msg.body, inner) + "\n\n" +
		lipgloss.PlaceHorizontal(inner, lipgloss.Right, cancel+"  "+yes)
	return lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(border).
		Padding(1, 2).Width(w).Render(content)
}
