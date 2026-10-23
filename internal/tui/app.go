// Package tui é a interface de terminal do git-sync (bubbletea).
//
// Estrutura: três abas fixas (Espelhos, Instâncias, Diagnóstico) e, por cima
// delas, fluxos em tela cheia (onboarding, novo espelho, formulário de
// instância). Confirmações aparecem em um modal e avisos curtos em "toasts".
//
// Teclas vão só para a tela visível; as demais mensagens (resultados
// assíncronos, tamanho da janela) vão para todas as telas, para que uma tela
// coberta por outra ainda receba o resultado do que disparou.
package tui

import (
	"context"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/vinicgobbi/git-sync/internal/app"
)

// view é uma aba ou um fluxo.
type view interface {
	Init() tea.Cmd
	Update(tea.Msg) (view, tea.Cmd)
	View(w, h int) string
	Keys() []key.Binding
	// Capturing indica que um campo de texto está ativo: as teclas globais
	// (q, ?, 1-3) vão para o campo em vez de agir.
	Capturing() bool
}

// flow é uma view em tela cheia aberta sobre as abas.
type flow interface {
	view
	Title() string
}

type shared struct {
	ctx     context.Context
	env     *app.Env
	version string
}

type (
	openMsg   struct{ f flow }
	closeMsg  struct{}
	reloadMsg struct{} // instâncias ou espelhos podem ter mudado
	tabMsg    int
	toastMsg  struct {
		text string
		kind toastKind
	}
	toastExpireMsg struct{ id int }
	confirmMsg     struct {
		title, body, yes string
		danger           bool
		onYes            tea.Cmd
	}
)

// tick agenda uma mensagem futura; os testes trocam por uma versão instantânea.
var tick = tea.Tick

type toastKind int

const (
	toastInfo toastKind = iota
	toastOK
	toastErr
)

func open(f flow) tea.Cmd     { return func() tea.Msg { return openMsg{f} } }
func closeFlow() tea.Cmd      { return func() tea.Msg { return closeMsg{} } }
func reload() tea.Cmd         { return func() tea.Msg { return reloadMsg{} } }
func switchTab(i int) tea.Cmd { return func() tea.Msg { return tabMsg(i) } }
func notify(k toastKind, text string) tea.Cmd {
	return func() tea.Msg { return toastMsg{text, k} }
}
func notifyErr(err error) tea.Cmd {
	f := humanize(err)
	text := f.msg
	if f.hint != "" {
		text += " — " + f.hint
	}
	return notify(toastErr, text)
}

// confirm pede confirmação num modal antes de executar onYes.
func confirm(title, body, yes string, danger bool, onYes tea.Cmd) tea.Cmd {
	return func() tea.Msg { return confirmMsg{title, body, yes, danger, onYes} }
}

type model struct {
	sh       *shared
	width    int
	height   int
	tabs     []view
	tabNames []string
	active   int
	flows    []flow
	modal    *modal
	toast    *toastMsg
	toastID  int
	help     help.Model
	showHelp bool
}

// Run abre a TUI e bloqueia até o usuário sair.
func Run(ctx context.Context, env *app.Env, version string) error {
	_, err := tea.NewProgram(newModel(ctx, env, version), tea.WithAltScreen(), tea.WithContext(ctx)).Run()
	return err
}

func newModel(ctx context.Context, env *app.Env, version string) model {
	sh := &shared{ctx: ctx, env: env, version: version}
	h := help.New()
	h.Styles.ShortKey = sKey
	h.Styles.FullKey = sKey
	h.Styles.ShortDesc = sMuted
	h.Styles.FullDesc = sMuted
	return model{
		sh:       sh,
		tabs:     []view{newMirrorsView(sh), newInstancesView(sh), newDiagnosticsView(sh)},
		tabNames: []string{"Espelhos", "Instâncias", "Diagnóstico"},
		help:     h,
	}
}

func (m model) Init() tea.Cmd {
	cmds := make([]tea.Cmd, 0, len(m.tabs)+1)
	for _, t := range m.tabs {
		cmds = append(cmds, t.Init())
	}
	if len(m.sh.env.Cfg.Instances) == 0 {
		cmds = append(cmds, open(newOnboarding(m.sh, false)))
	}
	return tea.Batch(cmds...)
}

func (m model) current() view {
	if n := len(m.flows); n > 0 {
		return m.flows[n-1]
	}
	return m.tabs[m.active]
}

func (m *model) setCurrent(v view) {
	if n := len(m.flows); n > 0 {
		m.flows[n-1] = v.(flow)
		return
	}
	m.tabs[m.active] = v
}

// broadcast entrega a mensagem a todas as abas e fluxos.
func (m *model) broadcast(msg tea.Msg) tea.Cmd {
	var cmds []tea.Cmd
	for i, t := range m.tabs {
		nt, cmd := t.Update(msg)
		m.tabs[i] = nt
		cmds = append(cmds, cmd)
	}
	for i, f := range m.flows {
		nf, cmd := f.Update(msg)
		m.flows[i] = nf.(flow)
		cmds = append(cmds, cmd)
	}
	return tea.Batch(cmds...)
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.help.Width = msg.Width - 2
		return m, m.broadcast(msg)

	case tea.KeyMsg:
		return m.handleKey(msg)

	case openMsg:
		m.flows = append(m.flows, msg.f)
		m.showHelp = false
		return m, msg.f.Init()

	case closeMsg:
		if len(m.flows) > 0 {
			m.flows = m.flows[:len(m.flows)-1]
		}
		return m, m.broadcast(reloadMsg{})

	case tabMsg:
		m.flows = nil
		m.active = int(msg)
		return m, nil

	case toastMsg:
		m.toastID++
		m.toast = &msg
		id := m.toastID
		return m, tick(5*time.Second, func(time.Time) tea.Msg { return toastExpireMsg{id} })

	case toastExpireMsg:
		if msg.id == m.toastID {
			m.toast = nil
		}
		return m, nil

	case confirmMsg:
		m.modal = newModal(msg)
		return m, nil
	}
	return m, m.broadcast(msg)
}

func (m model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if msg.String() == "ctrl+c" {
		return m, tea.Quit
	}
	if m.modal != nil {
		done, cmd := m.modal.handle(msg)
		if done {
			m.modal = nil
		}
		return m, cmd
	}
	cur := m.current()
	if m.showHelp {
		if msg.String() == "?" || msg.String() == "esc" || msg.String() == "q" {
			m.showHelp = false
		}
		return m, nil
	}
	if !cur.Capturing() {
		switch msg.String() {
		case "?":
			m.showHelp = true
			return m, nil
		}
		if len(m.flows) == 0 {
			switch msg.String() {
			case "q":
				return m, tea.Quit
			case "1", "2", "3":
				m.active = int(msg.Runes[0] - '1')
				return m, nil
			case "tab":
				m.active = (m.active + 1) % len(m.tabs)
				return m, nil
			case "shift+tab":
				m.active = (m.active + len(m.tabs) - 1) % len(m.tabs)
				return m, nil
			}
		}
	}
	nv, cmd := cur.Update(msg)
	m.setCurrent(nv)
	return m, cmd
}

const (
	headerHeight = 2
	footerHeight = 2
)

func (m model) View() string {
	if m.width == 0 {
		return ""
	}
	bodyH := max(m.height-headerHeight-footerHeight, 3)
	cur := m.current()
	body := fitHeight(cur.View(m.width, bodyH), bodyH)

	switch {
	case m.modal != nil:
		body = overlay(body, m.modal.view(min(m.width-4, 64)), m.width, bodyH)
	case m.showHelp:
		body = overlay(body, m.helpBox(), m.width, bodyH)
	}
	return m.header() + "\n" + body + "\n" + m.footer()
}

func (m model) header() string {
	logo := sTitle.Render(" ◆ git-sync")
	var mid string
	if n := len(m.flows); n > 0 {
		mid = sMuted.Render("  ›  ") + sBold.Render(m.flows[n-1].Title())
	} else {
		parts := make([]string, len(m.tabNames))
		for i, name := range m.tabNames {
			label := itoaKey(i) + " " + name
			if i == m.active {
				parts[i] = sTabActive.Render(label)
			} else {
				parts[i] = sTabInactive.Render(label)
			}
		}
		mid = "   " + strings.Join(parts, " ")
	}
	right := sMuted.Render("v" + m.sh.version + " ")
	gap := m.width - lipgloss.Width(logo) - lipgloss.Width(mid) - lipgloss.Width(right)
	line := logo + mid + strings.Repeat(" ", max(gap, 1)) + right
	rule := lipgloss.NewStyle().Foreground(colFaint).Render(strings.Repeat("─", m.width))
	return truncate(line, m.width) + "\n" + rule
}

func itoaKey(i int) string { return string(rune('1' + i)) }

func (m model) keyMap() keyMap {
	km := keyMap{view: m.current().Keys()}
	switch {
	case m.current().Capturing():
		// num campo de texto, ? e q são digitados: não anuncia atalhos globais
	case len(m.flows) > 0:
		km.global = []key.Binding{kHelp}
	default:
		km.global = []key.Binding{kTabs, kHelp, kQuit}
	}
	return km
}

func (m model) footer() string {
	var status string
	if m.toast != nil {
		switch m.toast.kind {
		case toastOK:
			status = sOK.Render("✔ ") + m.toast.text
		case toastErr:
			status = sErr.Render("✘ ") + m.toast.text
		default:
			status = sAccent.Render("• ") + m.toast.text
		}
	}
	if m.modal != nil {
		return " " + truncate(status, m.width-2) + "\n " + keyHint("←/→", "escolher") + "  " + keyHint("enter", "confirmar") + "  " + keyHint("esc", "cancelar")
	}
	return " " + truncate(status, m.width-2) + "\n " + truncate(m.help.ShortHelpView(m.keyMap().ShortHelp()), m.width-2)
}

func (m model) helpBox() string {
	h := m.help
	h.ShowAll = true
	content := sTitle.Render("Atalhos") + "\n\n" + h.View(m.keyMap()) + "\n\n" + sMuted.Render("? ou esc para fechar")
	return sCard.BorderForeground(colAccent).Padding(1, 2).Render(content)
}
