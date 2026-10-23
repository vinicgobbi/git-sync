package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/vinicgobbi/git-sync/internal/app"
	"github.com/vinicgobbi/git-sync/internal/config"
)

// instancesView lista as contas cadastradas e testa cada uma.
type instancesView struct {
	sh     *shared
	spin   spinner.Model
	cursor int
	checks map[string]*instCheck
}

type instCheck struct {
	running bool
	st      app.InstanceStatus
}

type (
	instCheckedMsg struct {
		name string
		st   app.InstanceStatus
	}
	instRemovedMsg struct {
		name string
		err  error
	}
)

var (
	ikAdd    = bind([]string{"a"}, "a", "adicionar")
	ikEdit   = bind([]string{"enter", "e"}, "enter", "editar")
	ikRemove = bind([]string{"x", "delete"}, "x", "remover")
	ikTest   = bind([]string{"t"}, "t", "testar conexão")
	ikImport = bind([]string{"i"}, "i", "importar do tea/gh")
)

func newInstancesView(sh *shared) *instancesView {
	return &instancesView{sh: sh, spin: spinner.New(spinner.WithSpinner(spinner.MiniDot)), checks: map[string]*instCheck{}}
}

func (v *instancesView) Init() tea.Cmd   { return v.checkAll() }
func (v *instancesView) Capturing() bool { return false }

func (v *instancesView) Keys() []key.Binding {
	if len(v.sh.env.Cfg.Instances) == 0 {
		return []key.Binding{ikAdd, ikImport}
	}
	return []key.Binding{ikAdd, ikEdit, ikRemove, ikTest, ikImport, kUp, kDown}
}

func (v *instancesView) checkAll() tea.Cmd {
	cmds := []tea.Cmd{v.spin.Tick}
	for _, in := range v.sh.env.Cfg.Instances {
		cmds = append(cmds, v.check(in))
	}
	return tea.Batch(cmds...)
}

func (v *instancesView) check(in config.Instance) tea.Cmd {
	v.checks[in.Name] = &instCheck{running: true}
	sh := v.sh
	return func() tea.Msg { return instCheckedMsg{in.Name, sh.env.CheckInstance(sh.ctx, in)} }
}

func (v *instancesView) anyRunning() bool {
	for _, c := range v.checks {
		if c.running {
			return true
		}
	}
	return false
}

func (v *instancesView) Update(msg tea.Msg) (view, tea.Cmd) {
	insts := v.sh.env.Cfg.Instances
	switch msg := msg.(type) {
	case reloadMsg:
		v.cursor = min(v.cursor, max(len(insts)-1, 0))
		return v, v.checkAll()
	case spinner.TickMsg:
		if v.anyRunning() {
			var cmd tea.Cmd
			v.spin, cmd = v.spin.Update(msg)
			return v, cmd
		}
	case instCheckedMsg:
		v.checks[msg.name] = &instCheck{st: msg.st}
	case instRemovedMsg:
		if msg.err != nil {
			return v, notifyErr(msg.err)
		}
		v.cursor = min(v.cursor, max(len(v.sh.env.Cfg.Instances)-1, 0))
		return v, tea.Batch(notify(toastOK, "Instância "+msg.name+" removida"), reload())
	case tea.KeyMsg:
		switch {
		case key.Matches(msg, kUp):
			v.cursor = max(v.cursor-1, 0)
		case key.Matches(msg, kDown):
			v.cursor = min(v.cursor+1, max(len(insts)-1, 0))
		case key.Matches(msg, ikAdd):
			return v, open(newInstanceForm(v.sh, nil))
		case key.Matches(msg, ikImport):
			return v, open(newOnboarding(v.sh, true))
		case key.Matches(msg, ikEdit):
			if v.cursor < len(insts) {
				in := insts[v.cursor]
				return v, open(newInstanceForm(v.sh, &in))
			}
		case key.Matches(msg, ikTest):
			if v.cursor < len(insts) {
				return v, tea.Batch(v.spin.Tick, v.check(insts[v.cursor]))
			}
		case key.Matches(msg, ikRemove):
			if v.cursor < len(insts) {
				name, env := insts[v.cursor].Name, v.sh.env
				return v, confirm("Remover "+name+"?",
					"A instância e seus tokens saem do git-sync.\n\nEspelhos já configurados nos servidores continuam funcionando.",
					"Remover", true, func() tea.Msg { return instRemovedMsg{name, env.RemoveInstance(name)} })
			}
		}
	}
	return v, nil
}

func authLabel(in config.Instance, st app.InstanceStatus) string {
	switch in.Auth {
	case config.AuthTea:
		if st.OAuth {
			return "login do tea (OAuth)"
		}
		return "login do tea"
	case config.AuthGh:
		return "login do gh"
	}
	return "token no keyring"
}

func (v *instancesView) status(in config.Instance) (dot, text string) {
	c, ok := v.checks[in.Name]
	switch {
	case !ok || c.running:
		return sAccent.Render(v.spin.View()), sMuted.Render("testando…")
	case c.st.Err != nil:
		return sErr.Render("●"), sErr.Render(humanize(c.st.Err).msg)
	case c.st.MirrorsDisabled:
		return sWarn.Render("●"), sWarn.Render("espelhamento desativado pelo administrador")
	}
	return sOK.Render("●"), "conectado como " + sBold.Render(c.st.User)
}

func (v *instancesView) View(w, h int) string {
	insts := v.sh.env.Cfg.Instances
	if len(insts) == 0 {
		return centered(emptyCard("Nenhuma instância",
			"Cadastre as contas que o git-sync vai usar: a origem\n(Gitea ou Forgejo) e os destinos (GitHub, Codeberg…).",
			keyHint("i", "importar do tea/gh")+"   "+keyHint("a", "adicionar manualmente")), w, h)
	}

	var b strings.Builder
	b.WriteString("\n")
	nameW, urlW := 16, max(w-70, 20)
	for i, in := range insts {
		dot, st := v.status(in)
		line := dot + " " + padRight(sBold.Render(in.Name), nameW) + padRight(kindBadge(in.Kind), 10) +
			padRight(sMuted.Render(in.URL), urlW) + "  " + st
		b.WriteString(row(line, i == v.cursor, w) + "\n")
	}
	if v.cursor < len(insts) {
		b.WriteString("\n" + v.detail(insts[v.cursor], min(w-2, 100)))
	}
	return b.String()
}

func (v *instancesView) detail(in config.Instance, w int) string {
	var st app.InstanceStatus
	if c, ok := v.checks[in.Name]; ok && !c.running {
		st = c.st
	}
	field := func(label, val string) string { return sLabel.Width(26).Render(label) + val }

	role := "origem e destino"
	if !in.CanBeSource() {
		role = "somente destino " + sMuted.Render("(o GitHub não envia espelhos)")
	}
	lines := []string{
		sBold.Render(in.Name) + "  " + kindBadge(in.Kind),
		"",
		field("Endereço", in.URL),
		field("Papel", role),
		field("Credencial da API", authLabel(in, st)),
	}

	mirrorCred := "usa a credencial da API"
	var warn string
	switch {
	case st.MirrorToken:
		mirrorCred = sOK.Render("token dedicado ✔")
	case st.OAuth:
		mirrorCred = sWarn.Render("nenhuma")
		warn = "Logins OAuth expiram e não servem como credencial de espelho. Para usar " + in.Name +
			" como destino, edite a instância e informe um token de mirror."
	case in.Kind == config.KindGitHub:
		warn = "Recomendado: um fine-grained PAT dedicado com Contents e Workflows: write como token de mirror."
	}
	lines = append(lines, field("Credencial dos espelhos", mirrorCred))

	if c, ok := v.checks[in.Name]; ok && !c.running && c.st.Err != nil {
		f := humanize(c.st.Err)
		lines = append(lines, "", sErr.Render("✘ "+f.msg))
		if f.hint != "" {
			lines = append(lines, sMuted.Render(wrap(f.hint, w-4)))
		}
	}
	if warn != "" {
		lines = append(lines, "", sWarn.Render("⚠ ")+wrap(warn, w-6))
	}
	return sCard.Width(w).Render(strings.Join(lines, "\n"))
}

// diagnosticsView verifica ferramentas, keyring e configuração.
type diagnosticsView struct {
	sh      *shared
	spin    spinner.Model
	running bool
	report  *diagReport
}

type diagReport struct {
	checks     []depCheck
	keyringErr error
}

type depCheck struct {
	name, version, detail, purpose string
	found, required                bool
}

type diagDoneMsg struct{ r *diagReport }

var dkRun = bind([]string{"r"}, "r", "verificar de novo")

func newDiagnosticsView(sh *shared) *diagnosticsView {
	return &diagnosticsView{sh: sh, spin: spinner.New(spinner.WithSpinner(spinner.MiniDot))}
}

func (v *diagnosticsView) Init() tea.Cmd   { return v.run() }
func (v *diagnosticsView) Capturing() bool { return false }
func (v *diagnosticsView) Keys() []key.Binding {
	return []key.Binding{dkRun, ikImport}
}

func (v *diagnosticsView) run() tea.Cmd {
	v.running = true
	sh := v.sh
	return tea.Batch(v.spin.Tick, func() tea.Msg { return diagDoneMsg{runDiagnostics(sh)} })
}

func (v *diagnosticsView) Update(msg tea.Msg) (view, tea.Cmd) {
	switch msg := msg.(type) {
	case spinner.TickMsg:
		if v.running {
			var cmd tea.Cmd
			v.spin, cmd = v.spin.Update(msg)
			return v, cmd
		}
	case diagDoneMsg:
		v.running, v.report = false, msg.r
	case tea.KeyMsg:
		switch {
		case key.Matches(msg, dkRun):
			return v, v.run()
		case key.Matches(msg, ikImport):
			return v, open(newOnboarding(v.sh, true))
		}
	}
	return v, nil
}

func (v *diagnosticsView) View(w, h int) string {
	if v.report == nil {
		return centered(v.spin.View()+" Verificando o ambiente…", w, h)
	}
	cw := min(w-2, 100)
	var b strings.Builder

	b.WriteString(sSection.Render("Ferramentas") + "\n")
	for _, c := range v.report.checks {
		mark := sOK.Render("✔")
		if !c.found {
			mark = sMuted.Render("○")
			if c.required {
				mark = sErr.Render("✘")
			}
		}
		b.WriteString(fmt.Sprintf(" %s %s %s\n", mark, padRight(sBold.Render(c.name), 6), sMuted.Render(c.version)))
		b.WriteString("     " + sMuted.Render(c.purpose) + "\n")
		if c.detail != "" {
			b.WriteString("     " + truncate(c.detail, cw-6) + "\n")
		}
	}

	b.WriteString(sSection.Render("Armazenamento") + "\n")
	if v.report.keyringErr != nil {
		b.WriteString(" " + sErr.Render("✘") + " Keyring do sistema indisponível: " + humanize(v.report.keyringErr).msg + "\n")
		b.WriteString("     " + sMuted.Render(wrap("Tokens digitados não poderão ser salvos. Instâncias via tea/gh continuam funcionando.", cw-6)) + "\n")
	} else {
		b.WriteString(" " + sOK.Render("✔") + " Keyring do sistema disponível " + sMuted.Render("(tokens ficam fora dos arquivos)") + "\n")
	}
	b.WriteString(" " + sOK.Render("✔") + " Configuração em " + sMuted.Render(v.sh.env.CfgPath) + "\n")

	if v.running {
		b.WriteString("\n" + v.spin.View() + " verificando…")
	}
	return b.String()
}
