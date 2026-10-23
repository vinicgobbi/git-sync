package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/vinicgobbi/git-sync/internal/app"
	"github.com/vinicgobbi/git-sync/internal/config"
)

type obStep int

const (
	obWelcome obStep = iota
	obImport
	obDone
)

// onboarding guia a primeira configuração: explica o conceito e importa
// os logins do tea e do gh. Também é usado só para importar (importOnly).
type onboarding struct {
	sh         *shared
	step       obStep
	importOnly bool
	spin       spinner.Model
	loading    bool
	cands      []app.Candidate
	selected   []bool
	results    []*importResult
	importing  bool
	cursor     int
	err        error
}

type importResult struct {
	running bool
	inst    config.Instance
	err     error
}

type (
	candidatesMsg struct {
		cands []app.Candidate
		err   error
	}
	importedOneMsg struct {
		i    int
		inst config.Instance
		err  error
	}
)

var (
	okContinue = bind([]string{"enter"}, "enter", "continuar")
	okSkip     = bind([]string{"esc"}, "esc", "pular")
	okImport   = bind([]string{"enter"}, "enter", "importar marcados")
	okManual   = bind([]string{"a"}, "a", "adicionar manualmente")
	okFinish   = bind([]string{"enter"}, "enter", "concluir")
)

func newOnboarding(sh *shared, importOnly bool) *onboarding {
	o := &onboarding{sh: sh, importOnly: importOnly, spin: spinner.New(spinner.WithSpinner(spinner.MiniDot))}
	if importOnly {
		o.step = obImport
	}
	return o
}

func (o *onboarding) Title() string {
	if o.importOnly {
		return "Importar logins do tea e do gh"
	}
	return "Primeiros passos"
}

func (o *onboarding) Capturing() bool { return false }

func (o *onboarding) Init() tea.Cmd {
	if o.step == obImport {
		return o.loadCandidates()
	}
	return nil
}

func (o *onboarding) Keys() []key.Binding {
	switch o.step {
	case obWelcome:
		return []key.Binding{okContinue, okSkip}
	case obImport:
		if o.importing {
			return nil
		}
		if o.results != nil {
			return []key.Binding{okContinue}
		}
		if len(o.cands) == 0 {
			return []key.Binding{okManual, okContinue, kBack}
		}
		return []key.Binding{kToggle, okImport, okManual, kUp, kDown, kBack}
	}
	return []key.Binding{okFinish, okManual}
}

func (o *onboarding) loadCandidates() tea.Cmd {
	o.loading = true
	sh := o.sh
	return tea.Batch(o.spin.Tick, func() tea.Msg {
		c, err := sh.env.Candidates(sh.ctx)
		return candidatesMsg{c, err}
	})
}

func (o *onboarding) Update(msg tea.Msg) (view, tea.Cmd) {
	switch msg := msg.(type) {
	case spinner.TickMsg:
		if o.loading || o.importing {
			var cmd tea.Cmd
			o.spin, cmd = o.spin.Update(msg)
			return o, cmd
		}
	case candidatesMsg:
		o.loading, o.cands, o.err = false, msg.cands, msg.err
		o.selected = make([]bool, len(o.cands))
		for i, c := range o.cands {
			o.selected[i] = !c.Exists
		}
	case importedOneMsg:
		o.results[msg.i] = &importResult{inst: msg.inst, err: msg.err}
		if next := o.nextToImport(msg.i + 1); next >= 0 {
			return o, o.importOne(next)
		}
		o.importing = false
		return o, reload()
	case tea.KeyMsg:
		return o.handleKey(msg)
	}
	return o, nil
}

func (o *onboarding) nextToImport(from int) int {
	for i := from; i < len(o.cands); i++ {
		if o.selected[i] && !o.cands[i].Exists {
			return i
		}
	}
	return -1
}

func (o *onboarding) importOne(i int) tea.Cmd {
	o.results[i] = &importResult{running: true}
	sh, c := o.sh, o.cands[i]
	return func() tea.Msg {
		inst, err := sh.env.ImportCandidate(sh.ctx, c)
		return importedOneMsg{i, inst, err}
	}
}

func (o *onboarding) handleKey(msg tea.KeyMsg) (view, tea.Cmd) {
	switch o.step {
	case obWelcome:
		switch msg.String() {
		case "enter":
			o.step = obImport
			return o, o.loadCandidates()
		case "esc":
			return o, closeFlow()
		}

	case obImport:
		if o.loading || o.importing {
			return o, nil
		}
		switch {
		case key.Matches(msg, okManual):
			return o, open(newInstanceForm(o.sh, nil))
		case key.Matches(msg, kBack):
			if o.importOnly {
				return o, closeFlow()
			}
			o.step, o.results = obWelcome, nil
		case o.results != nil && msg.String() == "enter":
			return o, o.finishImport()
		case key.Matches(msg, kUp):
			o.cursor = max(o.cursor-1, 0)
		case key.Matches(msg, kDown):
			o.cursor = min(o.cursor+1, max(len(o.cands)-1, 0))
		case key.Matches(msg, kToggle):
			if o.cursor < len(o.cands) && !o.cands[o.cursor].Exists {
				o.selected[o.cursor] = !o.selected[o.cursor]
			}
		case msg.String() == "enter":
			first := o.nextToImport(0)
			if first < 0 {
				return o, o.finishImport()
			}
			o.importing = true
			o.results = make([]*importResult, len(o.cands))
			return o, tea.Batch(o.spin.Tick, o.importOne(first))
		}

	case obDone:
		switch {
		case key.Matches(msg, okManual):
			return o, open(newInstanceForm(o.sh, nil))
		case msg.String() == "enter", msg.String() == "esc":
			text := "Tudo pronto! Pressione n para criar seu primeiro espelho."
			if len(o.sh.env.Cfg.Sources()) == 0 {
				text = "Cadastre uma instância Gitea ou Forgejo para começar a espelhar."
			}
			return o, tea.Batch(closeFlow(), notify(toastInfo, text))
		}
	}
	return o, nil
}

func (o *onboarding) finishImport() tea.Cmd {
	if o.importOnly {
		return closeFlow()
	}
	o.step = obDone
	return nil
}

func (o *onboarding) View(w, h int) string {
	cw := min(w-4, 84)
	var body string
	switch o.step {
	case obWelcome:
		body = o.viewWelcome(cw)
	case obImport:
		body = o.viewImport(cw)
	default:
		body = o.viewDone(cw)
	}
	return lipgloss.PlaceHorizontal(w, lipgloss.Center, lipgloss.NewStyle().Width(cw).Render(body))
}

func (o *onboarding) viewWelcome(w int) string {
	box := func(title, sub string) string {
		return sCard.Padding(0, 2).Align(lipgloss.Center).Render(sBold.Render(title) + "\n" + sMuted.Render(sub))
	}
	arrow := lipgloss.NewStyle().Foreground(colAccent).Render("\n──── push mirror ───▶")
	diagram := lipgloss.JoinHorizontal(lipgloss.Center,
		box("Gitea · Forgejo", "origem"), " ", arrow, " ", box("GitHub · Gitea · Forgejo", "destinos"))

	text := "O git-sync usa o espelhamento nativo do Gitea e do Forgejo: ele cria o " +
		"repositório vazio no destino e configura a origem para enviar cada push automaticamente. " +
		"Depois disso, nada precisa ficar rodando na sua máquina."
	steps := []string{
		sAccent.Render("1.") + " Cadastrar suas contas (a origem e os destinos)",
		sAccent.Render("2.") + " Escolher repositórios e para onde enviá-los",
		sAccent.Render("3.") + " Acompanhar os envios no painel",
	}
	return "\n" + sTitle.Render("Bem-vindo ao git-sync") + "\n\n" + wrap(text, w) + "\n\n" +
		lipgloss.PlaceHorizontal(w, lipgloss.Center, diagram) + "\n\n" + strings.Join(steps, "\n") + "\n\n" +
		keyHint("enter", "começar") + "   " + keyHint("esc", "pular")
}

func (o *onboarding) viewImport(w int) string {
	var b strings.Builder
	b.WriteString("\n" + sTitle.Render("Suas contas") + "\n\n")
	switch {
	case o.loading:
		b.WriteString(o.spin.View() + " Procurando logins do tea e do gh…\n")
		return b.String()
	case o.err != nil:
		b.WriteString(sErr.Render("✘ "+humanize(o.err).msg) + "\n")
	}
	if len(o.cands) == 0 {
		b.WriteString(wrap("Não encontramos logins do tea nem do gh nesta máquina. "+
			"Você pode adicionar as instâncias manualmente com um token de acesso, "+
			"ou fazer login com `tea login add` / `gh auth login` e voltar aqui.", w) + "\n")
		return b.String()
	}
	if o.results == nil {
		b.WriteString(wrap("Encontramos estes logins. Os marcados serão cadastrados usando a própria ferramenta "+
			"como credencial: nenhum token é copiado, e logins OAuth continuam sendo renovados pelo tea.", w) + "\n\n")
	}

	for i, c := range o.cands {
		mark := checkbox(o.selected[i])
		if c.Exists {
			mark = sMuted.Render("✔")
		}
		via := "via " + c.From
		if c.OAuth {
			via += " · OAuth"
		}
		user := ""
		if c.User != "" {
			user = " · " + c.User
		}
		status := ""
		if o.results != nil && o.results[i] != nil {
			switch r := o.results[i]; {
			case r.running:
				status = o.spin.View() + sMuted.Render(" conectando…")
			case r.err != nil:
				status = sErr.Render("✘ " + humanize(r.err).msg)
			default:
				status = sOK.Render("✔ conectado como " + r.inst.User)
			}
		} else if c.Exists {
			status = sMuted.Render("já cadastrada")
		}
		line := mark + " " + padRight(sBold.Render(c.Name), 16) + padRight(sMuted.Render(via), 18) + status
		if status == "" {
			line += sMuted.Render(strings.TrimPrefix(user, " · ") + "  " + c.URL)
		}
		b.WriteString(row(line, o.results == nil && i == o.cursor, w) + "\n")
	}
	if o.results != nil && !o.importing {
		b.WriteString("\n" + keyHint("enter", "continuar"))
	}
	return b.String()
}

func (o *onboarding) viewDone(w int) string {
	cfg := o.sh.env.Cfg
	var srcs, dsts []string
	for _, in := range cfg.Instances {
		if in.CanBeSource() {
			srcs = append(srcs, in.Name)
		}
		dsts = append(dsts, in.Name)
	}
	var b strings.Builder
	b.WriteString("\n" + sTitle.Render("Resumo") + "\n\n")
	b.WriteString(sLabel.Width(20).Render("Podem ser origem") + listOrNone(srcs) + "\n")
	b.WriteString(sLabel.Width(20).Render("Podem ser destino") + listOrNone(dsts) + "\n\n")
	switch {
	case len(srcs) == 0:
		b.WriteString(sWarn.Render("⚠ ") + wrap("Ainda falta uma origem: cadastre a instância Gitea ou Forgejo onde estão seus repositórios.", w-2) + "\n")
	case len(cfg.Instances) < 2:
		b.WriteString(sWarn.Render("⚠ ") + wrap("Cadastre também a instância de destino (por exemplo, o GitHub).", w-2) + "\n")
	default:
		b.WriteString(sOK.Render("✔ ") + "Tudo pronto para criar espelhos.\n")
	}
	b.WriteString(fmt.Sprintf("\n%s   %s", keyHint("enter", "ir para o painel"), keyHint("a", "adicionar outra instância")))
	return b.String()
}

func listOrNone(xs []string) string {
	if len(xs) == 0 {
		return sMuted.Render("nenhuma")
	}
	return strings.Join(xs, ", ")
}
