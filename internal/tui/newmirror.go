package tui

import (
	"fmt"
	"sort"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/progress"
	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/vinicgobbi/git-sync/internal/app"
	"github.com/vinicgobbi/git-sync/internal/config"
	"github.com/vinicgobbi/git-sync/internal/forge"
	"github.com/vinicgobbi/git-sync/internal/mirror"
)

type nmStep int

const (
	nmSource nmStep = iota
	nmRepos
	nmTargets
	nmOptions
	nmReview
	nmRun
)

var nmStepNames = []string{"Origem", "Repositórios", "Destinos", "Opções", "Revisão", "Execução"}

var intervalPresets = []struct{ label, value string }{
	{"a cada 10 minutos", "10m0s"},
	{"a cada hora", "1h0m0s"},
	{"a cada 8 horas", "8h0m0s"},
	{"uma vez por dia", "24h0m0s"},
	{"desligado", "0s"},
}

// mirrorFlow é o assistente "novo espelho", com etapas e volta sem perder escolhas.
type mirrorFlow struct {
	sh    *shared
	step  nmStep
	spin  spinner.Model
	known map[string][]string // origem/owner/repo → destinos já espelhados

	sources   []config.Instance
	srcCursor int
	source    config.Instance
	src       forge.Mirrorer

	repos        []forge.Repo
	reposLoading bool
	reposErr     error
	selected     map[string]bool
	filter       string
	rCursor      int
	rOffset      int
	listH        int

	targets []*flowTarget
	tCursor int

	oCursor     int
	visibility  mirror.Visibility
	intervalIdx int
	onCommit    bool
	rollback    bool
	force       bool

	planning bool
	req      *mirror.Request
	plan     *mirror.Plan
	planErr  error
	pOffset  int

	running  bool
	finished bool
	state    []stepState
	sum      mirror.Summary
	events   chan tea.Msg
	bar      progress.Model
}

type flowTarget struct {
	inst     config.Instance
	on       bool
	owners   []string
	ownerIdx int
	loading  bool
	err      error
	credErr  error // a credencial de espelho não pode ser usada (ex.: OAuth)
}

type stepState struct {
	status mirror.Status
	msg    string
	err    error
	seen   bool
}

type (
	flowReposMsg struct {
		source string
		src    forge.Mirrorer
		repos  []forge.Repo
		err    error
	}
	flowOwnersMsg struct {
		name    string
		owners  []string
		err     error
		credErr error
	}
	flowPlanMsg struct {
		req  *mirror.Request
		plan *mirror.Plan
		err  error
	}
	flowExecEventMsg struct{ ev mirror.Event }
	flowExecDoneMsg  struct{ sum mirror.Summary }
)

var (
	nkNext      = bind([]string{"enter"}, "enter", "continuar")
	nkSelectAll = bind([]string{"ctrl+a"}, "ctrl+a", "marcar visíveis")
	nkOwner     = bind([]string{"left", "right"}, "←/→", "trocar conta")
	nkChange    = bind([]string{"left", "right", " "}, "←/→", "alterar")
	nkExecute   = bind([]string{"enter"}, "enter", "executar")
	nkFinish    = bind([]string{"enter"}, "enter", "voltar ao painel")
	nkType      = bind([]string{""}, "digite", "filtrar")
)

func newMirrorFlow(sh *shared, links []mirror.Link) *mirrorFlow {
	f := &mirrorFlow{
		sh:       sh,
		spin:     spinner.New(spinner.WithSpinner(spinner.MiniDot)),
		known:    map[string][]string{},
		sources:  sh.env.Cfg.Sources(),
		selected: map[string]bool{},
		onCommit: sh.env.Cfg.Defaults.SyncOnCommit,
		rollback: true,
		bar:      progress.New(progress.WithDefaultGradient(), progress.WithoutPercentage()),
	}
	f.intervalIdx = 2
	for i, p := range intervalPresets {
		if p.value == sh.env.Cfg.Defaults.Interval {
			f.intervalIdx = i
		}
	}
	for _, l := range links {
		k := l.Source.Name + "/" + l.Repo.FullName()
		f.known[k] = append(f.known[k], targetName(l))
	}
	return f
}

func (f *mirrorFlow) Title() string { return "Novo espelho" }

func (f *mirrorFlow) Capturing() bool { return f.step == nmRepos }

func (f *mirrorFlow) Init() tea.Cmd {
	if len(f.sources) == 1 {
		return f.chooseSource(0)
	}
	return nil
}

func (f *mirrorFlow) Keys() []key.Binding {
	switch f.step {
	case nmSource:
		return []key.Binding{nkNext, kUp, kDown, kBack}
	case nmRepos:
		return []key.Binding{nkType, kToggle, nkSelectAll, nkNext, kBack}
	case nmTargets:
		return []key.Binding{kToggle, nkOwner, nkNext, kUp, kDown, kBack}
	case nmOptions:
		return []key.Binding{nkChange, nkNext, kUp, kDown, kBack}
	case nmReview:
		if f.plan != nil && f.runnable() > 0 {
			return []key.Binding{nkExecute, kUp, kDown, kBack}
		}
		return []key.Binding{kBack}
	case nmRun:
		if f.finished {
			return []key.Binding{nkFinish}
		}
	}
	return nil
}

// ---- carregamentos ----

func (f *mirrorFlow) chooseSource(i int) tea.Cmd {
	in := f.sources[i]
	if in.Name != f.source.Name {
		f.selected, f.repos, f.filter, f.rCursor, f.rOffset = map[string]bool{}, nil, "", 0, 0
		f.targets = nil
		for _, t := range f.sh.env.Cfg.Instances {
			if t.Name != in.Name {
				f.targets = append(f.targets, &flowTarget{inst: t})
			}
		}
		f.tCursor = 0
	}
	f.source, f.step = in, nmRepos
	if f.repos != nil {
		return nil
	}
	f.reposLoading, f.reposErr = true, nil
	sh := f.sh
	return tea.Batch(f.spin.Tick, func() tea.Msg {
		src, err := app.ConnectSource(in, sh.env.Store)
		if err != nil {
			return flowReposMsg{source: in.Name, err: err}
		}
		all, err := src.ListRepos(sh.ctx)
		var repos []forge.Repo
		for _, r := range all {
			if r.Admin {
				repos = append(repos, r)
			}
		}
		sort.Slice(repos, func(a, b int) bool {
			return strings.ToLower(repos[a].FullName()) < strings.ToLower(repos[b].FullName())
		})
		return flowReposMsg{source: in.Name, src: src, repos: repos, err: err}
	})
}

func (f *mirrorFlow) loadOwners(t *flowTarget) tea.Cmd {
	t.loading, t.err, t.credErr = true, nil, nil
	sh, in := f.sh, t.inst
	return tea.Batch(f.spin.Tick, func() tea.Msg {
		_, credErr := sh.env.MirrorPassword(sh.ctx, in)
		fg, err := app.Connect(in, sh.env.Store)
		if err != nil {
			return flowOwnersMsg{name: in.Name, err: err, credErr: credErr}
		}
		user, err := fg.Whoami(sh.ctx)
		if err != nil {
			return flowOwnersMsg{name: in.Name, err: err, credErr: credErr}
		}
		owners := []string{user}
		if orgs, err := fg.ListOrgs(sh.ctx); err == nil {
			sort.Strings(orgs)
			owners = append(owners, orgs...)
		}
		return flowOwnersMsg{name: in.Name, owners: owners, credErr: credErr}
	})
}

func (f *mirrorFlow) buildPlan() tea.Cmd {
	f.step, f.planning, f.plan, f.planErr, f.pOffset = nmReview, true, nil, nil, 0
	sh := f.sh
	req := &mirror.Request{
		Source: f.source, SourceForge: f.src, Repos: f.chosenRepos(),
		Interval: intervalPresets[f.intervalIdx].value, SyncOnCommit: f.onCommit,
		Visibility: f.visibility, Force: f.force, Concurrency: sh.env.Cfg.Defaults.Concurrency,
	}
	var specs [][2]string
	for _, t := range f.targets {
		if t.on {
			specs = append(specs, [2]string{t.inst.Name, t.owners[t.ownerIdx]})
		}
	}
	return tea.Batch(f.spin.Tick, func() tea.Msg {
		for _, s := range specs {
			tgt, err := sh.env.BuildTarget(sh.ctx, s[0], s[1])
			if err != nil {
				return flowPlanMsg{err: err}
			}
			req.Targets = append(req.Targets, tgt)
		}
		plan, err := mirror.BuildPlan(sh.ctx, req)
		return flowPlanMsg{req: req, plan: plan, err: err}
	})
}

func (f *mirrorFlow) execute() tea.Cmd {
	f.step, f.running = nmRun, true
	f.state = make([]stepState, len(f.plan.Steps))
	f.events = make(chan tea.Msg, 64)
	ch, req, plan, ctx := f.events, f.req, f.plan, f.sh.ctx
	opts := mirror.ExecOptions{Rollback: f.rollback}
	go func() {
		sum := mirror.Execute(ctx, req, plan, opts, func(ev mirror.Event) { ch <- flowExecEventMsg{ev} })
		ch <- flowExecDoneMsg{sum}
		close(ch)
	}()
	return tea.Batch(f.spin.Tick, waitFor(ch))
}

func waitFor(ch chan tea.Msg) tea.Cmd {
	return func() tea.Msg {
		if m, ok := <-ch; ok {
			return m
		}
		return nil
	}
}

// ---- helpers ----

func (f *mirrorFlow) visibleRepos() []forge.Repo {
	q := strings.ToLower(strings.TrimSpace(f.filter))
	if q == "" {
		return f.repos
	}
	var out []forge.Repo
	for _, r := range f.repos {
		if strings.Contains(strings.ToLower(r.FullName()), q) {
			out = append(out, r)
		}
	}
	return out
}

func (f *mirrorFlow) chosenRepos() []forge.Repo {
	var out []forge.Repo
	for _, r := range f.repos {
		if f.selected[r.FullName()] {
			out = append(out, r)
		}
	}
	return out
}

func (f *mirrorFlow) chosenTargets() []*flowTarget {
	var out []*flowTarget
	for _, t := range f.targets {
		if t.on {
			out = append(out, t)
		}
	}
	return out
}

func (f *mirrorFlow) runnable() int {
	if f.plan == nil {
		return 0
	}
	return f.plan.Count(mirror.ActionCreate) + f.plan.Count(mirror.ActionReuse)
}

// ---- update ----

func (f *mirrorFlow) Update(msg tea.Msg) (view, tea.Cmd) {
	switch msg := msg.(type) {
	case spinner.TickMsg:
		if f.reposLoading || f.planning || f.running || f.anyOwnerLoading() {
			var cmd tea.Cmd
			f.spin, cmd = f.spin.Update(msg)
			return f, cmd
		}
	case flowReposMsg:
		if msg.source != f.source.Name {
			return f, nil
		}
		f.reposLoading, f.src, f.repos, f.reposErr = false, msg.src, msg.repos, msg.err
	case flowOwnersMsg:
		for _, t := range f.targets {
			if t.inst.Name == msg.name {
				t.loading, t.err, t.credErr = false, msg.err, msg.credErr
				if msg.owners != nil {
					t.owners = msg.owners
				}
			}
		}
	case flowPlanMsg:
		f.planning, f.req, f.plan, f.planErr = false, msg.req, msg.plan, msg.err
	case flowExecEventMsg:
		st := &f.state[msg.ev.Index]
		st.status, st.msg, st.err, st.seen = msg.ev.Status, msg.ev.Msg, msg.ev.Err, true
		return f, waitFor(f.events)
	case flowExecDoneMsg:
		f.running, f.finished, f.sum = false, true, msg.sum
		return f, reload()
	case tea.KeyMsg:
		return f.handleKey(msg)
	}
	return f, nil
}

func (f *mirrorFlow) anyOwnerLoading() bool {
	for _, t := range f.targets {
		if t.loading {
			return true
		}
	}
	return false
}

func (f *mirrorFlow) handleKey(msg tea.KeyMsg) (view, tea.Cmd) {
	switch f.step {
	case nmSource:
		switch {
		case key.Matches(msg, kUp):
			f.srcCursor = max(f.srcCursor-1, 0)
		case key.Matches(msg, kDown):
			f.srcCursor = min(f.srcCursor+1, len(f.sources)-1)
		case msg.String() == "enter":
			return f, f.chooseSource(f.srcCursor)
		case msg.String() == "esc":
			return f, closeFlow()
		}

	case nmRepos:
		return f.handleReposKey(msg)

	case nmTargets:
		var t *flowTarget
		if f.tCursor < len(f.targets) {
			t = f.targets[f.tCursor]
		}
		switch {
		case msg.String() == "esc":
			f.step = nmRepos
		case key.Matches(msg, kUp):
			f.tCursor = max(f.tCursor-1, 0)
		case key.Matches(msg, kDown):
			f.tCursor = min(f.tCursor+1, max(len(f.targets)-1, 0))
		case msg.String() == " " && t != nil:
			t.on = !t.on
			if t.on && t.owners == nil && !t.loading {
				return f, f.loadOwners(t)
			}
		case (msg.String() == "left" || msg.String() == "right") && t != nil && t.on && len(t.owners) > 0:
			d := 1
			if msg.String() == "left" {
				d = -1
			}
			t.ownerIdx = (t.ownerIdx + d + len(t.owners)) % len(t.owners)
		case msg.String() == "enter":
			return f, f.leaveTargets()
		}

	case nmOptions:
		switch {
		case msg.String() == "esc":
			f.step = nmTargets
		case key.Matches(msg, kUp):
			f.oCursor = max(f.oCursor-1, 0)
		case key.Matches(msg, kDown):
			f.oCursor = min(f.oCursor+1, 4)
		case msg.String() == "left", msg.String() == "right", msg.String() == " ":
			d := 1
			if msg.String() == "left" {
				d = -1
			}
			f.changeOption(d)
		case msg.String() == "enter":
			return f, f.buildPlan()
		}

	case nmReview:
		switch {
		case msg.String() == "esc":
			if !f.planning {
				f.step = nmOptions
			}
		case key.Matches(msg, kUp):
			f.pOffset = max(f.pOffset-1, 0)
		case key.Matches(msg, kDown):
			f.pOffset++
		case msg.String() == "enter":
			if f.plan != nil && f.runnable() > 0 {
				return f, f.execute()
			}
		}

	case nmRun:
		if f.finished && (msg.String() == "enter" || msg.String() == "esc") {
			return f, closeFlow()
		}
		if !f.finished && msg.String() == "esc" {
			return f, notify(toastInfo, "Aguarde a execução terminar.")
		}
	}
	return f, nil
}

func (f *mirrorFlow) handleReposKey(msg tea.KeyMsg) (view, tea.Cmd) {
	vis := f.visibleRepos()
	switch msg.Type {
	case tea.KeyEsc:
		if f.filter != "" {
			f.filter, f.rCursor, f.rOffset = "", 0, 0
			return f, nil
		}
		if len(f.sources) > 1 {
			f.step = nmSource
			return f, nil
		}
		return f, closeFlow()
	case tea.KeyUp:
		f.rCursor--
	case tea.KeyDown:
		f.rCursor++
	case tea.KeyPgUp:
		f.rCursor -= max(f.listH, 1)
	case tea.KeyPgDown:
		f.rCursor += max(f.listH, 1)
	case tea.KeyHome:
		f.rCursor = 0
	case tea.KeyEnd:
		f.rCursor = len(vis) - 1
	case tea.KeySpace:
		if f.rCursor < len(vis) {
			n := vis[f.rCursor].FullName()
			f.selected[n] = !f.selected[n]
		}
	case tea.KeyCtrlA:
		all := true
		for _, r := range vis {
			all = all && f.selected[r.FullName()]
		}
		for _, r := range vis {
			f.selected[r.FullName()] = !all
		}
	case tea.KeyBackspace:
		if r := []rune(f.filter); len(r) > 0 {
			f.filter = string(r[:len(r)-1])
			f.rCursor, f.rOffset = 0, 0
		}
	case tea.KeyRunes:
		f.filter += string(msg.Runes)
		f.rCursor, f.rOffset = 0, 0
	case tea.KeyEnter:
		if f.reposLoading || f.src == nil {
			return f, nil
		}
		if len(f.chosenRepos()) == 0 {
			if f.rCursor >= len(vis) {
				return f, notify(toastErr, "Marque ao menos um repositório com espaço.")
			}
			f.selected[vis[f.rCursor].FullName()] = true // enter sem marcação = o repo atual
		}
		f.step = nmTargets
		return f, nil
	}
	f.rCursor = max(min(f.rCursor, len(vis)-1), 0)
	return f, nil
}

func (f *mirrorFlow) leaveTargets() tea.Cmd {
	chosen := f.chosenTargets()
	if len(chosen) == 0 {
		return notify(toastErr, "Marque ao menos um destino com espaço.")
	}
	for _, t := range chosen {
		switch {
		case t.loading:
			return notify(toastInfo, "Aguarde: consultando "+t.inst.Name+"…")
		case t.err != nil:
			return notify(toastErr, t.inst.Name+": "+humanize(t.err).msg)
		case t.credErr != nil:
			return notify(toastErr, t.inst.Name+" precisa de um token de mirror para receber espelhos (edite a instância).")
		}
	}
	f.step = nmOptions
	return nil
}

func (f *mirrorFlow) changeOption(d int) {
	switch f.oCursor {
	case 0:
		f.visibility = mirror.Visibility((int(f.visibility) + d + 3) % 3)
	case 1:
		f.intervalIdx = (f.intervalIdx + d + len(intervalPresets)) % len(intervalPresets)
	case 2:
		f.onCommit = !f.onCommit
	case 3:
		f.rollback = !f.rollback
	case 4:
		f.force = !f.force
	}
}

// ---- view ----

func (f *mirrorFlow) View(w, h int) string {
	cw := min(w-4, 110)
	head := "\n" + f.stepper(cw) + "\n\n"
	bodyH := h - lipgloss.Height(head)
	var body string
	switch f.step {
	case nmSource:
		body = f.viewSource(cw)
	case nmRepos:
		body = f.viewRepos(cw, bodyH)
	case nmTargets:
		body = f.viewTargets(cw)
	case nmOptions:
		body = f.viewOptions(cw)
	case nmReview:
		body = f.viewReview(cw, bodyH)
	case nmRun:
		body = f.viewRun(cw, bodyH)
	}
	return lipgloss.NewStyle().PaddingLeft(2).Render(head + body)
}

func (f *mirrorFlow) stepper(w int) string {
	parts := make([]string, len(nmStepNames))
	for i, name := range nmStepNames {
		switch {
		case nmStep(i) < f.step:
			parts[i] = sOK.Render("✔ " + name)
		case nmStep(i) == f.step:
			parts[i] = sAccent.Bold(true).Render("● " + name)
		default:
			parts[i] = sMuted.Render("○ " + name)
		}
	}
	full := strings.Join(parts, sMuted.Render("  ─  "))
	if lipgloss.Width(full) <= w {
		return full
	}
	return sAccent.Bold(true).Render(fmt.Sprintf("Etapa %d de %d · %s", f.step+1, len(nmStepNames), nmStepNames[f.step]))
}

func (f *mirrorFlow) viewSource(w int) string {
	var b strings.Builder
	b.WriteString(sBold.Render("De onde vêm os repositórios?") + "\n" + sMuted.Render("Só Gitea e Forgejo podem ser origem.") + "\n\n")
	for i, s := range f.sources {
		line := padRight(sBold.Render(s.Name), 18) + padRight(kindBadge(s.Kind), 10) + sMuted.Render(s.URL)
		b.WriteString(row(line, i == f.srcCursor, w) + "\n")
	}
	return b.String()
}

func (f *mirrorFlow) viewRepos(w, h int) string {
	var b strings.Builder
	b.WriteString(sBold.Render("Quais repositórios de "+f.source.Name+"?") + "\n")
	cursor := sAccent.Render("▏")
	filter := sMuted.Render("Filtrar: ") + f.filter + cursor
	if f.filter == "" {
		filter = sMuted.Render("Filtrar: ") + cursor + sMuted.Render("digite parte do nome")
	}
	b.WriteString(filter + "\n\n")

	switch {
	case f.reposLoading:
		b.WriteString(f.spin.View() + " Carregando repositórios…\n")
		return b.String()
	case f.reposErr != nil:
		fr := humanize(f.reposErr)
		b.WriteString(sErr.Render("✘ "+fr.msg) + "\n" + sMuted.Render(fr.hint) + "\n")
		return b.String()
	case len(f.repos) == 0:
		b.WriteString(sMuted.Render("Você não é administrador de nenhum repositório nesta instância.") + "\n")
		return b.String()
	}

	vis := f.visibleRepos()
	f.listH = max(h-6, 3)
	if f.rCursor < f.rOffset {
		f.rOffset = f.rCursor
	} else if f.rCursor >= f.rOffset+f.listH {
		f.rOffset = f.rCursor - f.listH + 1
	}
	if len(vis) == 0 {
		b.WriteString(sMuted.Render("Nenhum repositório com “"+f.filter+"”.") + "\n")
	}
	end := min(f.rOffset+f.listH, len(vis))
	for i := f.rOffset; i < end; i++ {
		r := vis[i]
		var tags []string
		if r.Private {
			tags = append(tags, "privado")
		}
		if r.Fork {
			tags = append(tags, "fork")
		}
		if r.Mirror {
			tags = append(tags, "espelho de outro repo")
		}
		if r.Empty {
			tags = append(tags, "vazio")
		}
		line := checkbox(f.selected[r.FullName()]) + " " + padRight(r.FullName(), 36)
		if len(tags) > 0 {
			line += sMuted.Render(strings.Join(tags, " · ")) + "  "
		}
		if dsts := f.known[f.source.Name+"/"+r.FullName()]; len(dsts) > 0 {
			line += sAccent.Render("↪ já espelhado em " + strings.Join(dsts, ", "))
		}
		b.WriteString(row(line, i == f.rCursor, w) + "\n")
	}
	n := len(f.chosenRepos())
	status := plural(n, "selecionado", "selecionados") + sMuted.Render(fmt.Sprintf(" · %d de %d visíveis", len(vis), len(f.repos)))
	if n == 0 {
		status += sMuted.Render(" · espaço marca; enter sem marcação usa o destacado")
	}
	b.WriteString("\n" + status)
	return b.String()
}

func (f *mirrorFlow) previewName() string {
	repos := f.chosenRepos()
	if len(repos) == 1 {
		return repos[0].Name
	}
	return "<repositório>"
}

func (f *mirrorFlow) viewTargets(w int) string {
	var b strings.Builder
	n := len(f.chosenRepos())
	b.WriteString(sBold.Render("Para onde enviar "+plural(n, "repositório", "repositórios")+"?") + "\n" +
		sMuted.Render("Marque um ou mais destinos. O repositório é criado vazio no destino, com o mesmo nome.") + "\n\n")
	if len(f.targets) == 0 {
		b.WriteString(sWarn.Render("⚠ ") + "Nenhuma outra instância cadastrada. Adicione um destino na aba Instâncias.\n")
	}
	for i, t := range f.targets {
		line := checkbox(t.on) + " " + padRight(sBold.Render(t.inst.Name), 16) + padRight(kindBadge(t.inst.Kind), 10)
		if t.on {
			switch {
			case t.loading:
				line += f.spin.View() + sMuted.Render(" consultando contas…")
			case t.err != nil:
				line += sErr.Render("✘ " + humanize(t.err).msg)
			case len(t.owners) > 0:
				line += sMuted.Render("conta ") + selector(t.owners[t.ownerIdx], i == f.tCursor && len(t.owners) > 1)
			}
		} else {
			line += sMuted.Render(t.inst.URL)
		}
		b.WriteString(row(line, i == f.tCursor, w) + "\n")
		if t.on && !t.loading && t.err == nil && len(t.owners) > 0 {
			b.WriteString("      " + sMuted.Render("→ "+t.inst.URL+"/"+t.owners[t.ownerIdx]+"/"+f.previewName()) + "\n")
		}
		if t.on && t.credErr != nil {
			b.WriteString("      " + sWarn.Render("⚠ ") + wrap("Esta instância usa login OAuth, que expira e não serve como credencial de espelho. "+
				"Edite-a na aba Instâncias e informe um token de mirror.", w-10) + "\n")
		}
	}
	return b.String()
}

func (f *mirrorFlow) viewOptions(w int) string {
	vis := []string{"igual à origem", "público", "privado"}
	yesNo := func(b bool) string {
		if b {
			return "sim"
		}
		return "não"
	}
	opts := []struct{ label, value, desc string }{
		{"Visibilidade no destino", vis[f.visibility], "“Igual à origem” mantém privado o que é privado."},
		{"Envio periódico", intervalPresets[f.intervalIdx].label, "Além dos pushes, a origem reenvia tudo nesse intervalo, corrigindo alterações feitas direto no destino."},
		{"Enviar a cada push", yesNo(f.onCommit), "Envia para os destinos logo após cada push na origem. Recomendado."},
		{"Desfazer se der erro", yesNo(f.rollback), "Apaga o repositório recém-criado no destino se não for possível configurar o espelho."},
		{"Sobrescrever existentes", yesNo(f.force), "Permite usar um repositório de destino que já tem conteúdo: ele será substituído pelo conteúdo da origem."},
	}
	var b strings.Builder
	b.WriteString(sBold.Render("Como os espelhos devem funcionar?") + "\n\n")
	for i, o := range opts {
		val := selector(o.value, i == f.oCursor)
		if i == 4 && f.force {
			val = sWarn.Render(val)
		}
		b.WriteString(row(padRight(o.label, 26)+val, i == f.oCursor, min(w, 70)) + "\n")
	}
	b.WriteString("\n" + sMuted.Render(wrap(opts[f.oCursor].desc, min(w, 80))) + "\n")
	if intervalPresets[f.intervalIdx].value == "0s" && !f.onCommit {
		b.WriteString("\n" + sWarn.Render("⚠ ") + "Com envio periódico desligado e sem envio a cada push, os espelhos nunca serão atualizados sozinhos.\n")
	}
	if f.force {
		b.WriteString("\n" + sWarn.Render("⚠ ") + "Repositórios de destino com conteúdo serão sobrescritos.\n")
	}
	return b.String()
}

func actionLabel(s mirror.Step) (icon, label string) {
	switch s.Action {
	case mirror.ActionCreate:
		return sOK.Render("+"), sOK.Render("criar e espelhar")
	case mirror.ActionReuse:
		if strings.Contains(s.Reason, "sobrescrito") {
			return sWarn.Render("!"), sWarn.Render("espelhar sobrescrevendo o conteúdo")
		}
		return sOK.Render("+"), sOK.Render("espelhar (repositório vazio já existe)")
	case mirror.ActionSkip:
		return sMuted.Render("="), sMuted.Render("já espelhado")
	case mirror.ActionConflict:
		return sWarn.Render("!"), sWarn.Render("destino já tem conteúdo — ignorado")
	}
	return sErr.Render("✘"), sErr.Render(s.Reason)
}

func (f *mirrorFlow) stepLine(i int, s mirror.Step, w int) string {
	dst := f.req.Targets[s.Target].Instance.Name + ":" + s.TargetOwner + "/" + s.TargetName
	icon, label := actionLabel(s)
	if f.state != nil && f.state[i].seen {
		st := f.state[i]
		switch st.status {
		case mirror.StatusRunning:
			icon, label = sAccent.Render(f.spin.View()), sAccent.Render(st.msg)
		case mirror.StatusDone:
			icon, label = sOK.Render("✔"), sOK.Render("espelho configurado")
		case mirror.StatusFailed:
			fr := humanize(st.err)
			icon, label = sErr.Render("✘"), sErr.Render(fr.msg)
		case mirror.StatusSkipped:
			icon = sMuted.Render("–")
		}
	}
	return truncate(icon+" "+padRight(s.Repo.FullName(), 30)+sMuted.Render(" → ")+padRight(dst, 34)+" "+label, w)
}

func (f *mirrorFlow) viewReview(w, h int) string {
	var b strings.Builder
	switch {
	case f.planning:
		return f.spin.View() + " Conferindo origem e destinos (nada é alterado nesta etapa)…"
	case f.planErr != nil:
		fr := humanize(f.planErr)
		s := sErr.Render("✘ Não foi possível montar o plano: ") + fr.msg
		if fr.hint != "" {
			s += "\n" + sMuted.Render(fr.hint)
		}
		return s + "\n\n" + keyHint("esc", "voltar")
	}
	p := f.plan
	create, reuse := p.Count(mirror.ActionCreate), p.Count(mirror.ActionReuse)
	skip, conflict, invalid := p.Count(mirror.ActionSkip), p.Count(mirror.ActionConflict), p.Count(mirror.ActionInvalid)

	b.WriteString(sBold.Render("Confira antes de executar") + "\n\n")
	var chips []string
	if create > 0 {
		chips = append(chips, sOK.Render(plural(create, "repositório será criado", "repositórios serão criados")))
	}
	if reuse > 0 {
		chips = append(chips, sOK.Render(plural(reuse, "repositório existente será usado", "repositórios existentes serão usados")))
	}
	if skip > 0 {
		chips = append(chips, sMuted.Render(plural(skip, "já espelhado", "já espelhados")))
	}
	if conflict > 0 {
		chips = append(chips, sWarn.Render(plural(conflict, "conflito", "conflitos")))
	}
	if invalid > 0 {
		chips = append(chips, sErr.Render(plural(invalid, "problema", "problemas")))
	}
	b.WriteString(strings.Join(chips, sMuted.Render("  ·  ")) + "\n\n")

	listH := max(h-10, 3)
	f.pOffset = min(f.pOffset, max(len(p.Steps)-listH, 0))
	end := min(f.pOffset+listH, len(p.Steps))
	for i := f.pOffset; i < end; i++ {
		b.WriteString(f.stepLine(i, p.Steps[i], w) + "\n")
	}
	if len(p.Steps) > listH {
		b.WriteString(sMuted.Render(fmt.Sprintf("  %d–%d de %d  (↑/↓ para rolar)", f.pOffset+1, end, len(p.Steps))) + "\n")
	}

	b.WriteString("\n")
	if conflict > 0 {
		b.WriteString(sWarn.Render("⚠ ") + wrap("Alguns destinos já têm conteúdo e foram ignorados. Para usá-los mesmo assim, volte (esc) e ative “Sobrescrever existentes”.", w-4) + "\n")
	}
	if n := f.runnable(); n > 0 {
		b.WriteString(keyHint("enter", "executar "+plural(n, "espelho", "espelhos")) + "   " + keyHint("esc", "voltar e ajustar"))
	} else {
		b.WriteString(sMuted.Render("Nada a fazer: tudo já está espelhado.") + "   " + keyHint("esc", "voltar"))
	}
	return b.String()
}

func (f *mirrorFlow) viewRun(w, h int) string {
	var b strings.Builder
	total := f.runnable()
	done := 0
	for i, s := range f.state {
		if s.seen && s.status != mirror.StatusRunning && f.plan.Steps[i].Runnable() {
			done++
		}
	}
	if f.finished {
		if f.sum.Failed == 0 {
			b.WriteString(sOK.Bold(true).Render("✔ "+plural(f.sum.Done, "espelho configurado", "espelhos configurados")) + "\n\n")
			b.WriteString(wrap("A primeira cópia acontece em segundo plano no servidor de origem e pode levar alguns minutos. Acompanhe o status no painel.", w) + "\n\n")
		} else {
			b.WriteString(sWarn.Bold(true).Render(fmt.Sprintf("%d configurado(s), %d com falha", f.sum.Done, f.sum.Failed)) + "\n\n")
			b.WriteString(wrap("Veja abaixo o motivo de cada falha. Corrija e rode o assistente de novo: o que já foi feito é reaproveitado.", w) + "\n\n")
		}
	} else {
		f.bar.Width = min(w-20, 60)
		pct := 0.0
		if total > 0 {
			pct = float64(done) / float64(total)
		}
		b.WriteString(sBold.Render("Configurando espelhos…") + "\n\n" + f.bar.ViewAs(pct) + sMuted.Render(fmt.Sprintf("  %d de %d", done, total)) + "\n\n")
	}
	listH := max(h-10, 3)
	shown := 0
	for i, s := range f.plan.Steps {
		if !s.Runnable() && !(f.state[i].seen && f.state[i].status == mirror.StatusFailed) {
			continue
		}
		if shown >= listH {
			b.WriteString(sMuted.Render("  …") + "\n")
			break
		}
		b.WriteString(f.stepLine(i, s, w) + "\n")
		shown++
	}
	if f.finished {
		b.WriteString("\n" + keyHint("enter", "voltar ao painel"))
	}
	return b.String()
}
