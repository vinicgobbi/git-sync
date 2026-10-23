package tui

import (
	"fmt"
	"sort"
	"strings"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/vinicgobbi/git-sync/internal/app"
	"github.com/vinicgobbi/git-sync/internal/config"
	"github.com/vinicgobbi/git-sync/internal/forge"
)

// repoPicker escolhe a origem e os repositórios a espelhar.
type repoPicker struct {
	sh       *shared
	sources  []config.Instance
	srcIdx   int
	src      forge.Mirrorer
	repos    []forge.Repo // só repos onde o usuário é admin
	selected map[string]bool
	filter   textinput.Model
	cursor   int
	offset   int
	spin     spinner.Model
	loading  bool
	err      error
}

type reposLoadedMsg struct {
	source string
	src    forge.Mirrorer
	repos  []forge.Repo
	err    error
}

func newRepoPicker(sh *shared) *repoPicker {
	ti := textinput.New()
	ti.Prompt = "/ "
	ti.Placeholder = "filtrar"
	return &repoPicker{
		sh: sh, sources: sh.env.Cfg.Sources(), selected: map[string]bool{},
		filter: ti, spin: spinner.New(spinner.WithSpinner(spinner.Dot)),
	}
}

func (p *repoPicker) Title() string { return "Novo espelho: repositórios" }
func (p *repoPicker) Keys() string {
	if p.filter.Focused() {
		return "digite para filtrar • enter/esc concluir"
	}
	k := "espaço marcar • a marcar todos visíveis • / filtrar • enter continuar • esc voltar"
	if len(p.sources) > 1 {
		k = "tab trocar origem • " + k
	}
	return k
}

func (p *repoPicker) Init() tea.Cmd { return p.load() }

func (p *repoPicker) load() tea.Cmd {
	p.loading, p.err, p.repos, p.cursor, p.offset = true, nil, nil, 0, 0
	p.selected = map[string]bool{}
	in, sh := p.sources[p.srcIdx], p.sh
	return tea.Batch(p.spin.Tick, func() tea.Msg {
		src, err := app.ConnectSource(in, sh.env.Store)
		if err != nil {
			return reposLoadedMsg{source: in.Name, err: err}
		}
		all, err := src.ListRepos(sh.ctx)
		var repos []forge.Repo
		for _, r := range all {
			if r.Admin {
				repos = append(repos, r)
			}
		}
		sort.Slice(repos, func(i, j int) bool { return repos[i].FullName() < repos[j].FullName() })
		return reposLoadedMsg{source: in.Name, src: src, repos: repos, err: err}
	})
}

func (p *repoPicker) visible() []forge.Repo {
	q := strings.ToLower(strings.TrimSpace(p.filter.Value()))
	if q == "" {
		return p.repos
	}
	var out []forge.Repo
	for _, r := range p.repos {
		if strings.Contains(strings.ToLower(r.FullName()), q) {
			out = append(out, r)
		}
	}
	return out
}

func (p *repoPicker) listHeight() int { return max(p.sh.bodyHeight()-6, 3) }

func (p *repoPicker) Update(msg tea.Msg) (screen, tea.Cmd) {
	switch msg := msg.(type) {
	case spinner.TickMsg:
		if p.loading {
			var cmd tea.Cmd
			p.spin, cmd = p.spin.Update(msg)
			return p, cmd
		}
	case reposLoadedMsg:
		if msg.source != p.sources[p.srcIdx].Name {
			return p, nil // resposta de uma origem anterior
		}
		p.loading, p.src, p.repos, p.err = false, msg.src, msg.repos, msg.err
	case tea.KeyMsg:
		if p.filter.Focused() {
			switch msg.String() {
			case "enter", "esc":
				p.filter.Blur()
				return p, nil
			}
			var cmd tea.Cmd
			p.filter, cmd = p.filter.Update(msg)
			p.cursor, p.offset = 0, 0
			return p, cmd
		}
		return p.handleKey(msg)
	}
	return p, nil
}

func (p *repoPicker) handleKey(msg tea.KeyMsg) (screen, tea.Cmd) {
	vis := p.visible()
	switch msg.String() {
	case "esc", "q":
		return p, pop()
	case "tab":
		if len(p.sources) > 1 {
			p.srcIdx = (p.srcIdx + 1) % len(p.sources)
			return p, p.load()
		}
	case "/":
		return p, p.filter.Focus()
	case "up", "k":
		p.cursor--
	case "down", "j":
		p.cursor++
	case "pgup":
		p.cursor -= p.listHeight()
	case "pgdown":
		p.cursor += p.listHeight()
	case "home", "g":
		p.cursor = 0
	case "end", "G":
		p.cursor = len(vis) - 1
	case " ", "x":
		if p.cursor < len(vis) {
			n := vis[p.cursor].FullName()
			p.selected[n] = !p.selected[n]
		}
	case "a":
		allOn := true
		for _, r := range vis {
			allOn = allOn && p.selected[r.FullName()]
		}
		for _, r := range vis {
			p.selected[r.FullName()] = !allOn
		}
	case "enter":
		var chosen []forge.Repo
		for _, r := range p.repos {
			if p.selected[r.FullName()] {
				chosen = append(chosen, r)
			}
		}
		if len(chosen) == 0 && p.cursor < len(vis) {
			chosen = []forge.Repo{vis[p.cursor]} // enter sem marcação = só o repo sob o cursor
		}
		if len(chosen) > 0 && p.src != nil {
			return p, push(newWizard(p.sh, p.sources[p.srcIdx], p.src, chosen))
		}
	}
	p.cursor = max(min(p.cursor, len(vis)-1), 0)
	h := p.listHeight()
	if p.cursor < p.offset {
		p.offset = p.cursor
	} else if p.cursor >= p.offset+h {
		p.offset = p.cursor - h + 1
	}
	return p, nil
}

func (p *repoPicker) View() string {
	var b strings.Builder
	b.WriteString("\nOrigem: ")
	for i, s := range p.sources {
		if i == p.srcIdx {
			b.WriteString(cursorStyle.Render("["+s.Name+"]") + " ")
		} else {
			b.WriteString(helpStyle.Render(s.Name) + " ")
		}
	}
	b.WriteString("\n" + p.filter.View() + "\n\n")

	switch {
	case p.loading:
		b.WriteString(p.spin.View() + " carregando repositórios…\n")
		return b.String()
	case p.err != nil:
		b.WriteString(errStyle.Render("✘ "+p.err.Error()) + "\n")
		return b.String()
	}

	vis := p.visible()
	if len(vis) == 0 {
		b.WriteString(helpStyle.Render("nenhum repositório (só aparecem repos onde você é admin)") + "\n")
	}
	end := min(p.offset+p.listHeight(), len(vis))
	for i := p.offset; i < end; i++ {
		r := vis[i]
		var tags []string
		if r.Private {
			tags = append(tags, "privado")
		}
		if r.Fork {
			tags = append(tags, "fork")
		}
		if r.Mirror {
			tags = append(tags, "pull-mirror")
		}
		if r.Empty {
			tags = append(tags, "vazio")
		}
		line := checkbox(p.selected[r.FullName()]) + " " + r.FullName()
		if len(tags) > 0 {
			line += " " + helpStyle.Render(strings.Join(tags, " · "))
		}
		if i == p.cursor {
			b.WriteString(cursorStyle.Render("› ") + line + "\n")
		} else {
			b.WriteString("  " + line + "\n")
		}
	}
	n := 0
	for _, on := range p.selected {
		if on {
			n++
		}
	}
	b.WriteString("\n" + helpStyle.Render(fmt.Sprintf("%d selecionado(s) · %d visíveis de %d", n, len(vis), len(p.repos))) + "\n")
	return b.String()
}
