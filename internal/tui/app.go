// Package tui é a interface de terminal do git-sync (bubbletea).
//
// Estrutura, no mesmo desenho do deploy-tools: um menu à esquerda, com as
// ações em seções, e um painel à direita. Itens de consulta (Espelhos,
// Instâncias, Diagnóstico) mostram a tela no painel; enter passa o foco
// para ela e esc devolve ao menu. Itens de ação descrevem o que fazem, com
// o comando equivalente, e enter abre o fluxo (novo espelho, formulário de
// instância...) dentro do painel. Confirmações aparecem em um modal e
// avisos curtos na linha sob o título, no lugar do próximo passo.
//
// Teclas vão só para quem tem o foco; as demais mensagens (resultados
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
	"github.com/vinicgobbi/git-sync/internal/install"
)

// view é uma tela do painel ou um fluxo.
type view interface {
	Init() tea.Cmd
	Update(tea.Msg) (view, tea.Cmd)
	View(w, h int) string
	Keys() []key.Binding
	// Capturing indica que um campo de texto está ativo: as teclas globais
	// (q, ?, esc) vão para o campo em vez de agir.
	Capturing() bool
}

// flow é uma view aberta no painel por cima da tela do item.
type flow interface {
	view
	Title() string
}

// escHandler é uma view que usa o esc antes de devolver o foco ao menu
// (ex.: limpar um filtro). HandleEsc diz se usou.
type escHandler interface {
	HandleEsc() bool
}

type shared struct {
	ctx       context.Context
	env       *app.Env
	version   string
	installer *install.Installer // nil se não deu para saber onde estamos
	install   install.Status
}

type (
	openMsg   struct{ f flow }
	closeMsg  struct{}
	reloadMsg struct{} // instâncias ou espelhos podem ter mudado
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

func open(f flow) tea.Cmd { return func() tea.Msg { return openMsg{f} } }
func closeFlow() tea.Cmd  { return func() tea.Msg { return closeMsg{} } }
func reload() tea.Cmd     { return func() tea.Msg { return reloadMsg{} } }
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

// item é uma entrada do menu: uma tela (view >= 0) ou uma ação (run).
type item struct {
	label string
	view  int // índice em model.views; -1 para ações
	// desc é o texto do painel de uma ação. É calculado a cada desenho,
	// então pode mostrar o contexto atual.
	desc func() string
	cli  string // o comando equivalente, mostrado sob a descrição
	run  func() tea.Cmd
	// hidden tira o item do menu enquanto devolver true (ex.: instalar,
	// quando já está instalado).
	hidden func() bool
}

type section struct {
	title string
	items []item
}

type model struct {
	sh       *shared
	width    int
	height   int
	views    []view
	mirrors  *mirrorsView // também em views; dá o estado para o cabeçalho
	sections []section
	refs     [][2]int // índice plano → seção, item
	cursor   int
	panel    bool // o foco está no painel (senão, no menu)
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
	if in, err := install.New(); err == nil {
		sh.installer, sh.install = in, in.Status()
	}
	h := help.New()
	h.ShortSeparator = " · "
	h.Styles.ShortKey = sMuted
	h.Styles.ShortDesc = sMuted
	h.Styles.ShortSeparator = sMuted
	h.Styles.FullKey = sKey
	h.Styles.FullDesc = sMuted
	mv := newMirrorsView(sh)
	m := model{
		sh:      sh,
		views:   []view{mv, newInstancesView(sh), newDiagnosticsView(sh)},
		mirrors: mv,
		help:    h,
	}
	m.sections = []section{
		{title: "Espelhos", items: []item{
			{label: "Espelhos", view: 0},
			{
				label: "Novo espelho", view: -1, run: mv.newMirror,
				desc: func() string {
					return "Escolha repositórios de uma origem (Gitea ou Forgejo) e para onde enviá-los. O git-sync " +
						"cria os repositórios vazios nos destinos e configura o espelhamento nativo da origem: " +
						"cada push passa a ser enviado sozinho, sem nada rodando na sua máquina.\n\n" +
						"Nada é criado antes da revisão final, e repositórios já espelhados são pulados."
				},
				cli: "git-sync mirror [flags]",
			},
		}},
		{title: "Instâncias", items: []item{
			{label: "Instâncias", view: 1},
			{
				label: "Adicionar instância", view: -1,
				run: func() tea.Cmd { return open(newInstanceForm(sh, nil)) },
				desc: func() string {
					return "Cadastra uma conta: Gitea ou Forgejo (podem ser origem e destino) ou GitHub (só destino). " +
						"O tipo é detectado pelo endereço, e a conexão é testada antes de salvar.\n\n" +
						"O token fica no keyring do sistema, nunca no arquivo de configuração."
				},
				cli: "git-sync instance add [flags]",
			},
			{
				label: "Importar do tea/gh", view: -1,
				run: func() tea.Cmd { return open(newOnboarding(sh, true)) },
				desc: func() string {
					return "Procura os logins do tea (Gitea e Forgejo, inclusive OAuth) e do gh (GitHub) nesta máquina " +
						"e cadastra os que você marcar, usando a própria ferramenta como credencial: nenhum token " +
						"é copiado, e logins OAuth continuam sendo renovados pelo tea."
				},
				cli: "git-sync instance import",
			},
		}},
		{title: "Esta ferramenta", items: []item{
			installItem(sh),
			{label: "Diagnóstico", view: 2},
			{
				label: "Primeiros passos", view: -1,
				run: func() tea.Cmd { return open(newOnboarding(sh, false)) },
				desc: func() string {
					return "Explica como o git-sync funciona e importa suas contas do tea e do gh. É o mesmo " +
						"guia da primeira vez que você abre o git-sync."
				},
			},
		}},
	}
	m.rebuildRefs()
	return m
}

// rebuildRefs refaz a lista de itens visíveis do menu.
func (m *model) rebuildRefs() {
	m.refs = nil
	for si, s := range m.sections {
		for ii, it := range s.items {
			if it.hidden == nil || !it.hidden() {
				m.refs = append(m.refs, [2]int{si, ii})
			}
		}
	}
	m.cursor = min(m.cursor, len(m.refs)-1)
}

func (m model) Init() tea.Cmd {
	cmds := make([]tea.Cmd, 0, len(m.views)+1)
	for _, v := range m.views {
		cmds = append(cmds, v.Init())
	}
	if len(m.sh.env.Cfg.Instances) == 0 {
		cmds = append(cmds, open(newOnboarding(m.sh, false)))
	}
	return tea.Batch(cmds...)
}

func (m model) item() item {
	r := m.refs[m.cursor]
	return m.sections[r[0]].items[r[1]]
}

// current é quem recebe as teclas do painel: o fluxo aberto ou a tela do
// item; nil num item de ação.
func (m model) current() view {
	if n := len(m.flows); n > 0 {
		return m.flows[n-1]
	}
	if it := m.item(); it.view >= 0 {
		return m.views[it.view]
	}
	return nil
}

func (m *model) setCurrent(v view) {
	if n := len(m.flows); n > 0 {
		m.flows[n-1] = v.(flow)
		return
	}
	m.views[m.item().view] = v
}

// broadcast entrega a mensagem a todas as telas e fluxos.
func (m *model) broadcast(msg tea.Msg) tea.Cmd {
	var cmds []tea.Cmd
	for i, v := range m.views {
		nv, cmd := v.Update(msg)
		m.views[i] = nv
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

	case installedMsg:
		if msg.err != nil {
			return m, notifyErr(msg.err)
		}
		m.sh.install.Installed = true
		m.rebuildRefs()
		return m, notify(toastOK, msg.text)
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
	if m.showHelp {
		if msg.String() == "?" || msg.String() == "esc" || msg.String() == "q" {
			m.showHelp = false
		}
		return m, nil
	}

	cur := m.current()
	if len(m.flows) == 0 && !m.panel {
		return m.handleMenuKey(msg)
	}
	if !cur.Capturing() {
		switch msg.String() {
		case "?":
			m.showHelp = true
			return m, nil
		case "q":
			if len(m.flows) == 0 {
				return m, tea.Quit
			}
		case "esc", "left":
			if len(m.flows) == 0 {
				if eh, ok := cur.(escHandler); ok && eh.HandleEsc() {
					return m, nil
				}
				m.panel = false
				return m, nil
			}
		}
	}
	nv, cmd := cur.Update(msg)
	m.setCurrent(nv)
	return m, cmd
}

func (m model) handleMenuKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "q":
		return m, tea.Quit
	case "?":
		m.showHelp = true
	case "up", "k":
		m.cursor = max(m.cursor-1, 0)
	case "down", "j":
		m.cursor = min(m.cursor+1, len(m.refs)-1)
	case "home", "g":
		m.cursor = 0
	case "end", "G":
		m.cursor = len(m.refs) - 1
	case "enter", "right", "l":
		if it := m.item(); it.view >= 0 {
			m.panel = true
		} else {
			return m, it.run()
		}
	}
	return m, nil
}

// ---- layout ----

func (m model) sideW() int {
	w := 18
	for _, s := range m.sections {
		for _, it := range s.items {
			w = max(w, lipgloss.Width(it.label)+4)
		}
	}
	return w
}

// panelW é a largura do texto dentro da caixa do painel.
func (m model) panelW() int { return max(20, m.width-m.sideW()-4-4) }

// status é a linha sob o título: o aviso mais recente ou o próximo passo.
func (m model) status() string {
	if m.toast != nil {
		switch m.toast.kind {
		case toastOK:
			return sOK.Render("✔ ") + m.toast.text
		case toastErr:
			return sErr.Render("✘ ") + m.toast.text
		}
		return sNotice.Render("• ") + m.toast.text
	}
	if n := m.nextStep(); n != "" {
		return sNotice.Render("→ " + n)
	}
	return ""
}

// bodyH é a altura dentro das caixas (cabeçalho e rodapé ocupam uma linha
// cada, e a linha de status só existe quando tem algo).
func (m model) bodyH() int {
	h := m.height - 2 - 2
	if m.status() != "" {
		h--
	}
	return max(5, h)
}

func (m model) View() string {
	if m.width == 0 {
		return ""
	}
	if m.width < 60 || m.height < 16 {
		return "Terminal pequeno demais: aumente a janela (mínimo 60×16).\n\nq sai."
	}
	body := m.body()
	switch {
	case m.modal != nil:
		body = overlay(body, m.modal.view(min(m.width-4, 64)), m.width, m.bodyH()+2)
	case m.showHelp:
		body = overlay(body, m.helpBox(), m.width, m.bodyH()+2)
	}
	return lipgloss.JoinVertical(lipgloss.Left, m.header(), body, m.footer())
}

func (m model) header() string {
	left := sTitle.Render("git-sync v" + m.sh.version)
	if sub := m.subtitle(); sub != "" {
		left += sMuted.Render("  ·  " + sub)
	}
	right := m.mirrors.health()
	gap := max(1, m.width-lipgloss.Width(left)-lipgloss.Width(right)-2)
	line := " " + left + strings.Repeat(" ", gap) + right
	if s := m.status(); s != "" {
		line += "\n " + truncate(s, m.width-2)
	}
	return line
}

func (m model) subtitle() string {
	cfg := m.sh.env.Cfg
	sub := plural(len(cfg.Instances), "instância", "instâncias")
	if m.mirrors.loaded && len(cfg.Sources()) > 0 {
		sub += " · " + plural(len(m.mirrors.links), "espelho", "espelhos")
	}
	return sub
}

// nextStep é o que falta fazer, na ordem em que uma instalação nova
// precisa. Roda a cada desenho: só olha o estado já carregado.
func (m model) nextStep() string {
	cfg := m.sh.env.Cfg
	switch {
	case len(cfg.Instances) == 0:
		return `Nenhuma conta ainda: comece por "Importar do tea/gh" ou "Adicionar instância".`
	case len(cfg.Sources()) == 0:
		return `Falta uma origem: cadastre o Gitea ou Forgejo dos seus repositórios em "Adicionar instância".`
	case len(cfg.Instances) < 2:
		return `Falta um destino: cadastre o GitHub, o Codeberg ou outra instância em "Adicionar instância".`
	case !m.mirrors.loaded:
		return ""
	case len(m.mirrors.links) == 0 && len(m.mirrors.warnings) == 0:
		return `Nenhum espelho ainda: crie o primeiro em "Novo espelho".`
	}
	if _, _, bad := m.mirrors.counts(); bad > 0 {
		return plural(bad, "espelho com erro", "espelhos com erro") + `: veja o motivo em "Espelhos".`
	}
	return ""
}

func (m model) body() string {
	h := m.bodyH()
	sideBox, panelBox := sBoxActive, sBox
	if m.panel || len(m.flows) > 0 {
		sideBox, panelBox = sBox, sBoxActive
	}
	l := sideBox.Width(m.sideW()).Height(h).Render(fitHeight(m.sidebar(), h))
	r := panelBox.Width(m.panelW() + 2).Height(h).Render(clip(m.panelView(), m.panelW(), h))
	return lipgloss.JoinHorizontal(lipgloss.Top, l, r)
}

func (m model) sidebar() string {
	var b strings.Builder
	active := m.panel || len(m.flows) > 0
	for i, r := range m.refs {
		if i == 0 || r[0] != m.refs[i-1][0] {
			if i > 0 {
				b.WriteString("\n")
			}
			b.WriteString(sMuted.Render(strings.ToUpper(m.sections[r[0]].title)) + "\n")
		}
		it := m.sections[r[0]].items[r[1]]
		switch {
		case i == m.cursor && !active:
			b.WriteString(sSelected.Render("▸ "+it.label) + "\n")
		case i == m.cursor:
			b.WriteString(sSelected.Render("● "+it.label) + "\n")
		default:
			b.WriteString("  " + it.label + "\n")
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

func (m model) panelView() string {
	w, h := m.panelW(), m.bodyH()
	if n := len(m.flows); n > 0 {
		f := m.flows[n-1]
		return sTitle.Render(f.Title()) + "\n" + f.View(w, h-1)
	}
	it := m.item()
	title := sTitle.Render(it.label)
	if it.view >= 0 {
		return title + "\n" + m.views[it.view].View(w, h-1)
	}
	var parts []string
	if it.desc != nil {
		parts = append(parts, wrap(it.desc(), w))
	}
	if it.cli != "" {
		parts = append(parts, sMuted.Render("Pelo terminal:")+"\n"+sMuted.Render(wrap(it.cli, w)))
	}
	return title + "\n\n" + strings.Join(parts, "\n\n")
}

// Atalhos do menu e do painel, para o rodapé e a ajuda.
var (
	kNav  = bind([]string{"up", "down"}, "↑↓", "navegar")
	kOpen = bind([]string{"enter"}, "enter", "abrir")
	kMenu = bind([]string{"esc"}, "esc", "menu")
)

func (m model) keyMap() keyMap {
	switch {
	case m.modal != nil:
		return keyMap{view: []key.Binding{
			bind([]string{"left", "right"}, "←→", "escolher"), bind([]string{"enter"}, "enter", "confirmar"),
			bind([]string{"esc"}, "esc", "cancelar"),
		}}
	case len(m.flows) == 0 && !m.panel:
		return keyMap{global: []key.Binding{kNav, kOpen, kHelp, kQuit}}
	}
	km := keyMap{view: m.current().Keys()}
	switch {
	case m.current().Capturing():
		// num campo de texto, ? e q são digitados: não anuncia atalhos globais
	case len(m.flows) > 0:
		km.global = []key.Binding{kHelp}
	default:
		km.global = []key.Binding{kMenu, kHelp, kQuit}
	}
	return km
}

func (m model) footer() string {
	return " " + truncate(m.help.ShortHelpView(m.keyMap().ShortHelp()), m.width-2)
}

func (m model) helpBox() string {
	h := m.help
	h.ShowAll = true
	content := sTitle.Render("Atalhos") + "\n\n" + h.View(m.keyMap()) + "\n\n" + sMuted.Render("? ou esc para fechar")
	return sBoxActive.Padding(1, 2).Render(content)
}
