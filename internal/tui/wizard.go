package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/vinicgobbi/git-sync/internal/config"
	"github.com/vinicgobbi/git-sync/internal/forge"
	"github.com/vinicgobbi/git-sync/internal/mirror"
)

type wizFieldKind int

const (
	wTarget wizFieldKind = iota
	wOwner
	wVisibility
	wInterval
	wOnCommit
	wForce
	wRollback
	wSubmit
)

type wizField struct {
	kind   wizFieldKind
	target int // índice em wizard.targets (wTarget/wOwner)
}

type wizTarget struct {
	inst  config.Instance
	on    bool
	owner textinput.Model
}

// wizard configura destinos e opções do espelhamento.
type wizard struct {
	sh         *shared
	source     config.Instance
	src        forge.Mirrorer
	repos      []forge.Repo
	targets    []wizTarget
	visibility mirror.Visibility
	interval   textinput.Model
	onCommit   bool
	force      bool
	rollback   bool
	fields     []wizField
	cursor     int
	err        string
}

// options é o resultado do wizard, consumido pela tela de plano.
type options struct {
	source     config.Instance
	src        forge.Mirrorer
	repos      []forge.Repo
	targets    []targetSpec
	visibility mirror.Visibility
	interval   string
	onCommit   bool
	force      bool
	rollback   bool
}

type targetSpec struct{ name, owner string }

func newWizard(sh *shared, source config.Instance, src forge.Mirrorer, repos []forge.Repo) *wizard {
	w := &wizard{
		sh: sh, source: source, src: src, repos: repos,
		onCommit: sh.env.Cfg.Defaults.SyncOnCommit, rollback: true,
	}
	for _, in := range sh.env.Cfg.Instances {
		if in.Name == source.Name {
			continue
		}
		ti := textinput.New()
		ti.Prompt = ""
		ti.Placeholder = "vazio = " + in.User
		ti.Width = 30
		w.targets = append(w.targets, wizTarget{inst: in, owner: ti})
	}
	w.interval = textinput.New()
	w.interval.Prompt = ""
	w.interval.Width = 12
	w.interval.SetValue(sh.env.Cfg.Defaults.Interval)
	w.rebuild()
	return w
}

func (w *wizard) Title() string { return "Destinos e opções" }
func (w *wizard) Keys() string {
	return "↑/↓ navegar • espaço marcar/alternar • ←/→ mudar opção • enter planejar • esc voltar"
}
func (w *wizard) Init() tea.Cmd { return nil }

// rebuild recria a lista de campos (owners só aparecem para destinos marcados).
func (w *wizard) rebuild() {
	w.fields = w.fields[:0]
	for i, t := range w.targets {
		w.fields = append(w.fields, wizField{kind: wTarget, target: i})
		if t.on {
			w.fields = append(w.fields, wizField{kind: wOwner, target: i})
		}
	}
	for _, k := range []wizFieldKind{wVisibility, wInterval, wOnCommit, wForce, wRollback, wSubmit} {
		w.fields = append(w.fields, wizField{kind: k})
	}
	w.cursor = min(w.cursor, len(w.fields)-1)
}

func (w *wizard) current() wizField { return w.fields[w.cursor] }

// input retorna o textinput do campo atual, se houver.
func (w *wizard) input() *textinput.Model {
	f := w.current()
	switch f.kind {
	case wOwner:
		return &w.targets[f.target].owner
	case wInterval:
		return &w.interval
	}
	return nil
}

func (w *wizard) move(delta int) tea.Cmd {
	if in := w.input(); in != nil {
		in.Blur()
	}
	w.cursor = (w.cursor + delta + len(w.fields)) % len(w.fields)
	if in := w.input(); in != nil {
		return in.Focus()
	}
	return nil
}

func (w *wizard) Update(msg tea.Msg) (screen, tea.Cmd) {
	key, ok := msg.(tea.KeyMsg)
	if !ok {
		return w, nil
	}
	w.err = ""
	f := w.current()
	switch key.String() {
	case "esc":
		return w, pop()
	case "up", "shift+tab":
		return w, w.move(-1)
	case "down", "tab":
		return w, w.move(1)
	case "enter":
		if f.kind == wTarget {
			w.toggleTarget(f.target)
			return w, nil
		}
		return w, w.submit()
	case " ":
		switch f.kind {
		case wTarget:
			w.toggleTarget(f.target)
			return w, nil
		case wOnCommit:
			w.onCommit = !w.onCommit
		case wForce:
			w.force = !w.force
		case wRollback:
			w.rollback = !w.rollback
		case wVisibility:
			w.visibility = (w.visibility + 1) % 3
		case wSubmit:
			return w, w.submit()
		}
		if w.input() == nil {
			return w, nil
		}
	case "left", "right":
		if f.kind == wVisibility {
			d := mirror.Visibility(1)
			if key.String() == "left" {
				d = 2
			}
			w.visibility = (w.visibility + d) % 3
			return w, nil
		}
	}
	if in := w.input(); in != nil {
		var cmd tea.Cmd
		*in, cmd = in.Update(key)
		return w, cmd
	}
	return w, nil
}

func (w *wizard) toggleTarget(i int) {
	w.targets[i].on = !w.targets[i].on
	w.rebuild()
}

func (w *wizard) submit() tea.Cmd {
	opts := options{
		source: w.source, src: w.src, repos: w.repos, visibility: w.visibility,
		interval: strings.TrimSpace(w.interval.Value()), onCommit: w.onCommit, force: w.force, rollback: w.rollback,
	}
	for _, t := range w.targets {
		if t.on {
			opts.targets = append(opts.targets, targetSpec{t.inst.Name, strings.TrimSpace(t.owner.Value())})
		}
	}
	if len(opts.targets) == 0 {
		w.err = "marque ao menos um destino"
		return nil
	}
	d, err := time.ParseDuration(opts.interval)
	if err != nil {
		w.err = fmt.Sprintf("intervalo inválido %q (ex.: 8h0m0s, 30m)", opts.interval)
		return nil
	}
	if d != 0 && d < 10*time.Minute {
		w.err = "o intervalo mínimo padrão do Gitea/Forgejo é 10m (0 desativa o sync periódico)"
		return nil
	}
	return push(newPlanScreen(w.sh, opts))
}

var visibilityNames = []string{"herdar da origem", "público", "privado"}

func (w *wizard) View() string {
	var b strings.Builder
	names := make([]string, 0, 3)
	for i, r := range w.repos {
		if i == 3 {
			names = append(names, fmt.Sprintf("+%d", len(w.repos)-3))
			break
		}
		names = append(names, r.FullName())
	}
	b.WriteString(fmt.Sprintf("\n%s %s: %s\n\n", helpStyle.Render("origem"), w.source.Name, strings.Join(names, ", ")))

	if len(w.targets) == 0 {
		b.WriteString(warnStyle.Render("nenhuma outra instância cadastrada para servir de destino (Instâncias → a)") + "\n")
	}
	b.WriteString(boldStyle.Render("Destinos") + "\n")
	for i, f := range w.fields {
		if f.kind == wVisibility {
			b.WriteString("\n" + boldStyle.Render("Opções") + "\n")
		}
		if f.kind == wSubmit {
			b.WriteString("\n")
		}
		prefix := "  "
		if i == w.cursor {
			prefix = cursorStyle.Render("› ")
		}
		b.WriteString(prefix + w.renderField(f) + "\n")
	}
	if w.err != "" {
		b.WriteString("\n" + errStyle.Render("✘ "+w.err) + "\n")
	}
	return b.String()
}

func (w *wizard) renderField(f wizField) string {
	switch f.kind {
	case wTarget:
		t := w.targets[f.target]
		return fmt.Sprintf("%s %-14s %s", checkbox(t.on), t.inst.Name, helpStyle.Render(string(t.inst.Kind)+" · "+t.inst.URL))
	case wOwner:
		return "    " + labelStyle.Width(14).Render("usuário/org") + w.targets[f.target].owner.View()
	case wVisibility:
		return labelStyle.Render("Visibilidade") + "‹ " + visibilityNames[w.visibility] + " ›"
	case wInterval:
		return labelStyle.Render("Intervalo") + w.interval.View()
	case wOnCommit:
		return labelStyle.Render("Sync a cada push") + checkbox(w.onCommit)
	case wForce:
		s := labelStyle.Render("Forçar") + checkbox(w.force) + helpStyle.Render(" usar repo destino que já tem conteúdo (será sobrescrito)")
		if w.force {
			s = labelStyle.Render("Forçar") + checkbox(w.force) + warnStyle.Render(" repos destino com conteúdo serão SOBRESCRITOS")
		}
		return s
	case wRollback:
		return labelStyle.Render("Rollback") + checkbox(w.rollback) + helpStyle.Render(" apagar repo criado se o mirror falhar")
	case wSubmit:
		return boldStyle.Render("[ Planejar → ]")
	}
	return ""
}
