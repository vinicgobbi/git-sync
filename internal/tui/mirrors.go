package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/vinicgobbi/git-sync/internal/forge"
	"github.com/vinicgobbi/git-sync/internal/mirror"
)

// mirrorsView é a tela principal: todos os push mirrors das origens.
type mirrorsView struct {
	sh       *shared
	spin     spinner.Model
	loading  bool // carregando (a lista antiga continua visível)
	loaded   bool
	links    []mirror.Link
	sources  map[string]forge.Mirrorer
	warnings []error
	loadedAt time.Time
	filter   textinput.Model
	cursor   int
	offset   int
	syncing  map[string]bool // chave do repo → sync disparado
	seq      int             // descarta respostas de cargas antigas
	// linhas de itens visíveis na última renderização (para a rolagem)
	listHeight int
}

type (
	mirrorsLoadedMsg struct {
		seq     int
		res     mirror.DiscoverResult
		sources map[string]forge.Mirrorer
		err     error
	}
	syncedMsg struct {
		keys []string
		err  error
	}
	mirrorRemovedMsg struct {
		label string
		err   error
	}
	autoReloadMsg struct{}
)

var (
	mkNew     = bind([]string{"n"}, "n", "novo espelho")
	mkSync    = bind([]string{"s"}, "s", "sincronizar")
	mkSyncAll = bind([]string{"S"}, "S", "sincronizar todos")
	mkRemove  = bind([]string{"x", "delete"}, "x", "remover")
	mkOpen    = bind([]string{"o"}, "o", "abrir destino")
	mkOpenSrc = bind([]string{"O"}, "O", "abrir origem")
	mkFilter  = bind([]string{"/"}, "/", "filtrar")
	mkSetup   = bind([]string{"enter"}, "enter", "configurar")
)

func newMirrorsView(sh *shared) *mirrorsView {
	f := textinput.New()
	_ = f.Cursor.SetMode(cursorMode)
	f.Prompt = "/ "
	f.Placeholder = "filtrar por repositório, instância ou URL"
	return &mirrorsView{sh: sh, spin: spinner.New(spinner.WithSpinner(spinner.MiniDot)), filter: f, syncing: map[string]bool{}}
}

func linkKey(l mirror.Link) string { return l.Source.Name + "/" + l.Repo.FullName() }

func (v *mirrorsView) Init() tea.Cmd { return v.load() }

func (v *mirrorsView) Capturing() bool { return v.filter.Focused() }

func (v *mirrorsView) Keys() []key.Binding {
	switch {
	case v.filter.Focused():
		return []key.Binding{bind([]string{"enter"}, "enter", "aplicar"), bind([]string{"esc"}, "esc", "limpar")}
	case len(v.sh.env.Cfg.Instances) == 0 || len(v.sh.env.Cfg.Sources()) == 0:
		return []key.Binding{mkSetup}
	case len(v.links) == 0:
		return []key.Binding{mkNew, kRefresh}
	}
	return []key.Binding{mkNew, mkSync, mkOpen, mkRemove, mkFilter, kRefresh, mkSyncAll, mkOpenSrc, kRenew, kUp, kDown}
}

func (v *mirrorsView) load() tea.Cmd {
	if len(v.sh.env.Cfg.Sources()) == 0 {
		v.loading, v.loaded, v.links = false, true, nil
		return nil
	}
	v.loading = true
	v.seq++
	seq, sh := v.seq, v.sh
	return tea.Batch(v.spin.Tick, func() tea.Msg {
		srcs, err := sh.env.Sources()
		if err != nil {
			return mirrorsLoadedMsg{seq: seq, err: err}
		}
		byName := map[string]forge.Mirrorer{}
		for _, s := range srcs {
			byName[s.Instance.Name] = s.Forge
		}
		res := mirror.Discover(sh.ctx, srcs, sh.env.Cfg.Instances, sh.env.Cfg.Defaults.Concurrency)
		return mirrorsLoadedMsg{seq: seq, res: res, sources: byName}
	})
}

func (v *mirrorsView) visible() []mirror.Link {
	q := strings.ToLower(strings.TrimSpace(v.filter.Value()))
	if q == "" {
		return v.links
	}
	var out []mirror.Link
	for _, l := range v.links {
		target := ""
		if l.Target != nil {
			target = l.Target.Name
		}
		hay := strings.ToLower(l.Source.Name + " " + l.Repo.FullName() + " " + target + " " + l.Mirror.RemoteAddress)
		if strings.Contains(hay, q) {
			out = append(out, l)
		}
	}
	return out
}

func (v *mirrorsView) selected() (mirror.Link, bool) {
	vis := v.visible()
	if v.cursor < 0 || v.cursor >= len(vis) {
		return mirror.Link{}, false
	}
	return vis[v.cursor], true
}

func (v *mirrorsView) Update(msg tea.Msg) (view, tea.Cmd) {
	switch msg := msg.(type) {
	case reloadMsg, autoReloadMsg:
		return v, v.load()
	case spinner.TickMsg:
		if v.loading || len(v.syncing) > 0 {
			var cmd tea.Cmd
			v.spin, cmd = v.spin.Update(msg)
			return v, cmd
		}
	case mirrorsLoadedMsg:
		if msg.seq != v.seq {
			return v, nil
		}
		v.loading, v.loaded, v.loadedAt = false, true, time.Now()
		if msg.err != nil {
			v.warnings = []error{msg.err}
			return v, nil
		}
		v.links, v.warnings, v.sources = msg.res.Links, msg.res.Errors, msg.sources
		v.syncing = map[string]bool{}
		v.cursor = min(v.cursor, max(len(v.visible())-1, 0))
	case syncedMsg:
		if msg.err != nil {
			for _, k := range msg.keys {
				delete(v.syncing, k)
			}
			return v, notifyErr(msg.err)
		}
		// o servidor sincroniza em segundo plano; recarrega em alguns segundos
		text := "Sincronização iniciada para " + plural(len(msg.keys), "repositório", "repositórios") + ". Atualizando em instantes…"
		return v, tea.Batch(notify(toastOK, text),
			tick(6*time.Second, func(time.Time) tea.Msg { return autoReloadMsg{} }))
	case mirrorRemovedMsg:
		if msg.err != nil {
			return v, notifyErr(msg.err)
		}
		return v, tea.Batch(notify(toastOK, "Espelho removido: "+msg.label), v.load())
	case tea.KeyMsg:
		return v.handleKey(msg)
	}
	return v, nil
}

func (v *mirrorsView) handleKey(msg tea.KeyMsg) (view, tea.Cmd) {
	if v.filter.Focused() {
		switch msg.String() {
		case "esc":
			v.filter.SetValue("")
			v.filter.Blur()
		case "enter":
			v.filter.Blur()
		default:
			var cmd tea.Cmd
			v.filter, cmd = v.filter.Update(msg)
			v.cursor, v.offset = 0, 0
			return v, cmd
		}
		return v, nil
	}

	cfg := v.sh.env.Cfg
	if len(cfg.Instances) == 0 && key.Matches(msg, mkSetup) {
		return v, open(newOnboarding(v.sh))
	}
	if len(cfg.Instances) > 0 && len(cfg.Sources()) == 0 && key.Matches(msg, mkSetup) {
		return v, open(newInstanceForm(v.sh, nil))
	}

	vis := v.visible()
	switch {
	case key.Matches(msg, kUp):
		v.cursor = max(v.cursor-1, 0)
	case key.Matches(msg, kDown):
		v.cursor = min(v.cursor+1, max(len(vis)-1, 0))
	case msg.String() == "home" || msg.String() == "g":
		v.cursor = 0
	case msg.String() == "end" || msg.String() == "G":
		v.cursor = max(len(vis)-1, 0)
	case key.Matches(msg, mkFilter):
		return v, v.filter.Focus()
	case key.Matches(msg, kRefresh):
		return v, v.load()
	case key.Matches(msg, mkNew):
		return v, v.newMirror()
	case key.Matches(msg, mkSync):
		if l, ok := v.selected(); ok {
			return v, v.sync([]mirror.Link{l})
		}
	case key.Matches(msg, mkSyncAll):
		if len(v.links) > 0 {
			repos := uniqueRepos(v.links)
			return v, confirm("Sincronizar todos?",
				fmt.Sprintf("Vamos pedir às origens que enviem agora %s para todos os destinos.", plural(len(repos), "repositório", "repositórios")),
				"Sincronizar", false, v.sync(v.links))
		}
	case key.Matches(msg, mkOpen):
		if l, ok := v.selected(); ok {
			return v, openBrowser(webURL(l.Mirror.RemoteAddress))
		}
	case key.Matches(msg, mkOpenSrc):
		if l, ok := v.selected(); ok {
			return v, openBrowser(l.Repo.HTMLURL)
		}
	case key.Matches(msg, kRenew):
		if l, ok := v.selected(); ok {
			if l.Target == nil {
				return v, notify(toastErr, "O destino deste espelho ("+hostOf(l.Mirror.RemoteAddress)+") não está cadastrado no git-sync.")
			}
			return v, renew(v.sh, l.Target.Name, v.countTo(l.Target.Name))
		}
	case key.Matches(msg, mkRemove):
		if l, ok := v.selected(); ok {
			return v, confirm("Remover este espelho?",
				fmt.Sprintf("%s deixará de enviar para %s.\n\nO repositório de destino não será apagado.",
					l.Repo.FullName(), webURL(l.Mirror.RemoteAddress)),
				"Remover", true, v.remove(l))
		}
	}
	v.scroll()
	return v, nil
}

// countTo conta os espelhos que enviam para a instância name.
func (v *mirrorsView) countTo(name string) int {
	n := 0
	for _, l := range v.links {
		if l.Target != nil && l.Target.Name == name {
			n++
		}
	}
	return n
}

// HandleEsc limpa o filtro aplicado antes de o esc voltar ao menu.
func (v *mirrorsView) HandleEsc() bool {
	if v.filter.Value() == "" {
		return false
	}
	v.filter.SetValue("")
	v.cursor, v.offset = 0, 0
	return true
}

// newMirror abre o assistente, ou avisa o que falta cadastrar antes.
func (v *mirrorsView) newMirror() tea.Cmd {
	cfg := v.sh.env.Cfg
	if len(cfg.Sources()) == 0 {
		return notify(toastErr, `Cadastre uma instância Gitea ou Forgejo para servir de origem, em "Adicionar instância".`)
	}
	if len(cfg.Instances) < 2 {
		return notify(toastErr, `Cadastre também a instância de destino, em "Adicionar instância".`)
	}
	return open(newMirrorFlow(v.sh, v.links))
}

func (v *mirrorsView) scroll() {
	h := max(v.listHeight, 1)
	if v.cursor < v.offset {
		v.offset = v.cursor
	} else if v.cursor >= v.offset+h {
		v.offset = v.cursor - h + 1
	}
}

func uniqueRepos(links []mirror.Link) []mirror.Link {
	seen := map[string]bool{}
	var out []mirror.Link
	for _, l := range links {
		if k := linkKey(l); !seen[k] {
			seen[k] = true
			out = append(out, l)
		}
	}
	return out
}

func (v *mirrorsView) sync(links []mirror.Link) tea.Cmd {
	repos := uniqueRepos(links)
	keys := make([]string, len(repos))
	for i, l := range repos {
		keys[i] = linkKey(l)
		v.syncing[keys[i]] = true
	}
	sources, ctx := v.sources, v.sh.ctx
	return tea.Batch(v.spin.Tick, func() tea.Msg {
		for _, l := range repos {
			if err := sources[l.Source.Name].SyncPushMirrors(ctx, l.Repo.Owner, l.Repo.Name); err != nil {
				return syncedMsg{keys: keys, err: err}
			}
		}
		return syncedMsg{keys: keys}
	})
}

func (v *mirrorsView) remove(l mirror.Link) tea.Cmd {
	src, ctx := v.sources[l.Source.Name], v.sh.ctx
	label := l.Repo.FullName() + " → " + targetName(l)
	return func() tea.Msg {
		err := src.DeletePushMirror(ctx, l.Repo.Owner, l.Repo.Name, l.Mirror.RemoteName)
		return mirrorRemovedMsg{label: label, err: err}
	}
}

func targetName(l mirror.Link) string {
	if l.Target != nil {
		return l.Target.Name
	}
	return hostOf(l.Mirror.RemoteAddress)
}

func hostOf(u string) string {
	s := webURL(u)
	if i := strings.Index(s, "://"); i >= 0 {
		s = s[i+3:]
	}
	h, _, _ := strings.Cut(s, "/")
	return h
}

// estado visual de um espelho
type linkState int

const (
	stateOK linkState = iota
	statePending
	stateErr
	stateSyncing
)

func (v *mirrorsView) state(l mirror.Link) linkState {
	switch {
	case v.syncing[linkKey(l)]:
		return stateSyncing
	case l.Mirror.LastError != "":
		return stateErr
	case l.Mirror.LastUpdate.IsZero():
		return statePending
	}
	return stateOK
}

func (v *mirrorsView) dot(s linkState) string {
	switch s {
	case stateErr:
		return sErr.Render("●")
	case statePending:
		return sWarn.Render("◌")
	case stateSyncing:
		return sAccent.Render(v.spin.View())
	}
	return sOK.Render("●")
}

func (v *mirrorsView) View(w, h int) string {
	cfg := v.sh.env.Cfg
	switch {
	case len(cfg.Instances) == 0:
		return emptyCard("Bem-vindo ao git-sync",
			"Espelhe repositórios do seu Gitea ou Forgejo para o GitHub,\n"+
				"Codeberg ou outra instância — usando o espelhamento nativo\n"+
				"da origem. Vamos começar encontrando suas contas.",
			keyHint("enter", "configurar agora"))
	case len(cfg.Sources()) == 0:
		return emptyCard("Falta uma origem",
			"Só Gitea e Forgejo conseguem enviar espelhos (push mirrors).\n"+
				"Cadastre a instância Gitea ou Forgejo onde estão seus repositórios.",
			keyHint("enter", "adicionar instância"))
	case !v.loaded:
		return "\n" + v.spin.View() + " Procurando espelhos nas suas instâncias…"
	case len(v.links) == 0 && len(v.warnings) == 0:
		return emptyCard("Nenhum espelho ainda",
			"Escolha repositórios de "+sourceNames(v)+" e para onde enviá-los.\n"+
				"O git-sync cria os repositórios no destino e configura tudo.",
			keyHint("n", "criar o primeiro espelho"))
	}

	header := v.summary(w)
	if v.filter.Focused() || v.filter.Value() != "" {
		header += "\n" + v.filter.View()
	}
	for _, e := range v.warnings {
		f := humanize(e)
		header += "\n" + sWarn.Render("⚠ ") + truncate(f.msg, w-4)
	}
	bodyH := h - lipgloss.Height(header) - 1

	if w >= 110 {
		listW := w * 58 / 100
		detailW := w - listW - 1
		list := v.renderList(listW, bodyH)
		detail := v.renderDetail(detailW-2, bodyH)
		return header + "\n" + lipgloss.JoinHorizontal(lipgloss.Top, padBlock(list, listW, bodyH), " ", detail)
	}
	detail := v.renderDetail(w-2, 0)
	listH := max(bodyH-lipgloss.Height(detail), 3)
	return header + "\n" + fitHeight(v.renderList(w, listH), listH) + "\n" + detail
}

func padBlock(s string, w, h int) string {
	lines := strings.Split(fitHeight(s, h), "\n")
	for i := range lines {
		lines[i] = padRight(lines[i], w)
	}
	return strings.Join(lines, "\n")
}

func sourceNames(v *mirrorsView) string {
	var names []string
	for _, s := range v.sh.env.Cfg.Sources() {
		names = append(names, s.Name)
	}
	return strings.Join(names, ", ")
}

// emptyCard é o texto de uma tela ainda sem nada para mostrar.
func emptyCard(title, body, action string) string {
	return "\n" + sBold.Render(title) + "\n\n" + body + "\n\n" + action
}

func (v *mirrorsView) counts() (ok, pend, bad int) {
	for _, l := range v.links {
		switch v.state(l) {
		case stateErr:
			bad++
		case statePending:
			pend++
		default:
			ok++
		}
	}
	return ok, pend, bad
}

// health resume os espelhos para o canto do cabeçalho.
func (v *mirrorsView) health() string {
	if !v.loaded || len(v.links) == 0 {
		return ""
	}
	switch _, pend, bad := v.counts(); {
	case bad > 0:
		return sErr.Render(plural(bad, "com erro", "com erro"))
	case pend > 0:
		return sWarn.Render(plural(pend, "aguardando 1º envio", "aguardando 1º envio"))
	default:
		return sOK.Render("todos em dia ✓")
	}
}

func (v *mirrorsView) summary(w int) string {
	ok, pend, bad := v.counts()
	parts := []string{sOK.Render("● ") + plural(ok, "em dia", "em dia")}
	if bad > 0 {
		parts = append(parts, sErr.Render("● ")+plural(bad, "com erro", "com erro"))
	}
	if pend > 0 {
		parts = append(parts, sWarn.Render("◌ ")+plural(pend, "aguardando 1º envio", "aguardando 1º envio"))
	}
	right := sMuted.Render("atualizado " + relTime(v.loadedAt))
	if v.loading {
		right = sAccent.Render(v.spin.View()) + sMuted.Render(" atualizando…")
	}
	left := " " + strings.Join(parts, "   ")
	gap := w - lipgloss.Width(left) - lipgloss.Width(right) - 1
	return left + strings.Repeat(" ", max(gap, 1)) + right
}

func (v *mirrorsView) renderList(w, h int) string {
	v.listHeight = h - 1
	vis := v.visible()
	if len(vis) == 0 {
		return "\n  " + sMuted.Render("Nenhum espelho corresponde ao filtro.")
	}
	v.cursor = min(v.cursor, len(vis)-1)
	v.scroll()

	timeW, dstW := 16, 14
	repoW := max(w-timeW-dstW-10, 12)
	head := "  " + sMuted.Render(padRight("  REPOSITÓRIO", repoW+2)+"   "+padRight("DESTINO", dstW)+" "+padRight("ÚLTIMO ENVIO", timeW))
	lines := []string{head}
	end := min(v.offset+v.listHeight, len(vis))
	for i := v.offset; i < end; i++ {
		l := vis[i]
		st := v.state(l)
		when := relTime(l.Mirror.LastUpdate)
		switch st {
		case stateErr:
			when = sErr.Render("falhou")
		case statePending:
			when = sWarn.Render("aguardando")
		case stateSyncing:
			when = sAccent.Render("enviando…")
		}
		repo := l.Repo.FullName()
		if len(v.sources) > 1 {
			repo = sMuted.Render(l.Source.Name+":") + repo
		}
		line := v.dot(st) + " " + padRight(repo, repoW) + sMuted.Render(" → ") + padRight(targetName(l), dstW) + " " + padRight(when, timeW)
		lines = append(lines, row(line, i == v.cursor, w))
	}
	if len(vis) > v.listHeight {
		lines = append(lines, sMuted.Render(fmt.Sprintf("  %d–%d de %d", v.offset+1, end, len(vis))))
	}
	return strings.Join(lines, "\n")
}

func (v *mirrorsView) renderDetail(w, h int) string {
	l, ok := v.selected()
	if !ok {
		return ""
	}
	field := func(label, val string) string {
		return sLabel.Width(14).Render(label) + truncate(val, w-18)
	}
	src := l.Source.Name + " " + sMuted.Render("("+kindName(l.Source.Kind)+")")
	dst := targetName(l)
	if l.Target != nil {
		dst += " " + sMuted.Render("("+kindName(l.Target.Kind)+")")
	} else {
		dst += " " + sMuted.Render("(não cadastrada)")
	}
	interval := intervalLabel(l.Mirror.Interval)
	onPush := "não"
	if l.Mirror.SyncOnCommit {
		onPush = "sim"
	}
	last := "ainda não enviado"
	if !l.Mirror.LastUpdate.IsZero() {
		last = l.Mirror.LastUpdate.Local().Format("02/01/2006 15:04") + sMuted.Render(" ("+relTime(l.Mirror.LastUpdate)+")")
	}

	lines := []string{
		sBold.Render(l.Repo.FullName()),
		"",
		field("Origem", src),
		field("", sMuted.Render(l.Repo.HTMLURL)),
		field("Destino", dst),
		field("", sMuted.Render(webURL(l.Mirror.RemoteAddress))),
		field("Último envio", last),
		field("A cada push", onPush),
		field("Periódico", interval),
	}
	if l.Mirror.LastError != "" {
		errText := sErr.Render("Último erro") + "\n" + wrap(oneLine(l.Mirror.LastError), w-4)
		if hint := mirrorHint(l.Mirror.LastError); hint != "" {
			errText += "\n\n" + sWarn.Render("Dica: ") + wrap(hint, w-10)
		}
		lines = append(lines, "", lipgloss.NewStyle().Border(lipgloss.NormalBorder(), false, false, false, true).
			BorderForeground(colErr).PaddingLeft(1).Width(w-2).Render(errText))
	}
	lines = append(lines, "", keyHint("s", "sincronizar")+"  "+keyHint("o", "abrir")+"  "+keyHint("x", "remover"))

	card := sCard.Width(w).Render(strings.Join(lines, "\n"))
	if h > 0 {
		return fitHeight(card, h)
	}
	return card
}

// intervalLabel descreve o intervalo do Gitea ("8h0m0s") em português.
func intervalLabel(v string) string {
	if v == "" {
		return intervalPresets[len(intervalPresets)-1].label
	}
	for _, p := range intervalPresets {
		if p.value == v {
			return p.label
		}
	}
	return "a cada " + strings.TrimSuffix(strings.TrimSuffix(v, "0s"), "0m")
}
