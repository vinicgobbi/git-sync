// Package tui é a interface de terminal do git-sync (bubbletea).
//
// Navegação: uma pilha de telas. Teclas vão só para a tela do topo; as demais
// mensagens (resultados assíncronos, tamanho da janela) vão para todas, para que
// uma tela que disparou uma operação e foi coberta ainda receba o resultado.
package tui

import (
	"context"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/vinicgobbi/git-sync/internal/app"
)

type screen interface {
	Init() tea.Cmd
	Update(tea.Msg) (screen, tea.Cmd)
	View() string
	Title() string
	Keys() string // dicas de teclas no rodapé
}

// shared é o estado comum a todas as telas.
type shared struct {
	ctx    context.Context
	env    *app.Env
	width  int
	height int
}

// bodyHeight é a altura disponível para o conteúdo da tela (sem cabeçalho/rodapé).
func (sh *shared) bodyHeight() int { return max(sh.height-4, 5) }

type (
	pushMsg    struct{ s screen }
	popMsg     struct{ toRoot bool }
	refreshMsg struct{} // enviada à tela que volta ao topo, para recarregar
	statusMsg  string
)

func push(s screen) tea.Cmd { return func() tea.Msg { return pushMsg{s} } }
func pop() tea.Cmd          { return func() tea.Msg { return popMsg{} } }
func popToRoot() tea.Cmd    { return func() tea.Msg { return popMsg{toRoot: true} } }

type model struct {
	sh    *shared
	stack []screen
}

// Run abre a TUI e bloqueia até o usuário sair.
func Run(ctx context.Context, env *app.Env) error {
	sh := &shared{ctx: ctx, env: env}
	m := model{sh: sh, stack: []screen{newDashboard(sh)}}
	_, err := tea.NewProgram(m, tea.WithAltScreen(), tea.WithContext(ctx)).Run()
	return err
}

func (m model) top() screen { return m.stack[len(m.stack)-1] }

func (m model) Init() tea.Cmd { return m.top().Init() }

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		if msg.String() == "ctrl+c" {
			return m, tea.Quit
		}
		s, cmd := m.top().Update(msg)
		m.stack[len(m.stack)-1] = s
		return m, cmd

	case tea.WindowSizeMsg:
		m.sh.width, m.sh.height = msg.Width, msg.Height

	case pushMsg:
		m.stack = append(m.stack, msg.s)
		size := tea.WindowSizeMsg{Width: m.sh.width, Height: m.sh.height}
		s, cmd := msg.s.Update(size)
		m.stack[len(m.stack)-1] = s
		return m, tea.Batch(s.Init(), cmd)

	case popMsg:
		if msg.toRoot {
			m.stack = m.stack[:1]
		} else if len(m.stack) > 1 {
			m.stack = m.stack[:len(m.stack)-1]
		}
		// a tela que volta ao topo recarrega (instâncias/mirrors podem ter mudado)
		s, cmd := m.top().Update(refreshMsg{})
		m.stack[len(m.stack)-1] = s
		return m, cmd
	}

	// demais mensagens: entregues a todas as telas da pilha
	var cmds []tea.Cmd
	for i, s := range m.stack {
		ns, cmd := s.Update(msg)
		m.stack[i] = ns
		cmds = append(cmds, cmd)
	}
	return m, tea.Batch(cmds...)
}

func (m model) View() string {
	if m.sh.width == 0 {
		return ""
	}
	titles := make([]string, len(m.stack))
	for i, s := range m.stack {
		titles[i] = s.Title()
	}
	header := titleStyle.Render("git-sync") + crumbStyle.Render(" › "+strings.Join(titles, " › "))
	footer := helpStyle.Render(m.top().Keys() + " • ctrl+c sair")

	body := m.top().View()
	// mantém o rodapé colado embaixo
	gap := m.sh.height - lipgloss.Height(header) - lipgloss.Height(body) - lipgloss.Height(footer) - 1
	return header + "\n" + body + strings.Repeat("\n", max(gap, 1)) + footer
}
