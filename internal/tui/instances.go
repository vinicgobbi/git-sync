package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/vinicgobbi/git-sync/internal/config"
)

// instancesScreen lista as instâncias configuradas.
type instancesScreen struct {
	sh      *shared
	cursor  int
	status  string
	confirm *confirmation
}

func newInstancesScreen(sh *shared) *instancesScreen { return &instancesScreen{sh: sh} }

func (s *instancesScreen) Title() string { return "Instâncias" }
func (s *instancesScreen) Keys() string {
	if s.confirm != nil {
		return "y confirmar • qualquer tecla cancela"
	}
	return "a adicionar • e editar • x remover • esc voltar"
}
func (s *instancesScreen) Init() tea.Cmd { return nil }

func (s *instancesScreen) Update(msg tea.Msg) (screen, tea.Cmd) {
	insts := s.sh.env.Cfg.Instances
	switch msg := msg.(type) {
	case refreshMsg:
		s.cursor = min(s.cursor, max(len(insts)-1, 0))
	case tea.KeyMsg:
		if s.confirm != nil {
			c := s.confirm
			s.confirm = nil
			if msg.String() == "y" {
				return s, c.run
			}
			return s, nil
		}
		switch msg.String() {
		case "esc", "q":
			return s, pop()
		case "up", "k":
			s.cursor = max(s.cursor-1, 0)
		case "down", "j":
			s.cursor = min(s.cursor+1, max(len(insts)-1, 0))
		case "a":
			return s, push(newInstanceForm(s.sh, nil))
		case "e", "enter":
			if s.cursor < len(insts) {
				in := insts[s.cursor]
				return s, push(newInstanceForm(s.sh, &in))
			}
		case "x":
			if s.cursor < len(insts) {
				name := insts[s.cursor].Name
				s.confirm = &confirmation{
					prompt: fmt.Sprintf("remover a instância %q e seus tokens do keyring? (mirrors existentes não são afetados)", name),
					run: func() tea.Msg {
						if err := s.sh.env.RemoveInstance(name); err != nil {
							return statusMsg(errStyle.Render("✘ " + err.Error()))
						}
						return statusMsg(okStyle.Render("✔ " + name + " removida"))
					},
				}
			}
		}
	case statusMsg:
		s.status = string(msg)
		s.cursor = min(s.cursor, max(len(s.sh.env.Cfg.Instances)-1, 0))
	}
	return s, nil
}

func (s *instancesScreen) View() string {
	var b strings.Builder
	b.WriteString("\n")
	insts := s.sh.env.Cfg.Instances
	if len(insts) == 0 {
		b.WriteString("Nenhuma instância. Pressione " + boldStyle.Render("a") + " para adicionar.\n")
	}
	for i, in := range insts {
		role := "destino"
		if in.CanBeSource() {
			role = "origem/destino"
		}
		line := fmt.Sprintf("%-14s %-8s %-36s %-16s %s", in.Name, in.Kind, truncate(in.URL, 36), in.User, helpStyle.Render(role))
		if i == s.cursor {
			b.WriteString(cursorStyle.Render("› ") + line + "\n")
		} else {
			b.WriteString("  " + line + "\n")
		}
	}
	b.WriteString("\n")
	if s.confirm != nil {
		b.WriteString(warnStyle.Render("? "+s.confirm.prompt+" [y/N]") + "\n")
	} else if s.status != "" {
		b.WriteString(s.status + "\n")
	}
	return b.String()
}

// instanceForm cadastra ou edita uma instância.
type instanceForm struct {
	sh      *shared
	editing bool
	inputs  []textinput.Model
	focus   int
	spin    spinner.Model
	saving  bool
	err     error
}

const (
	fName = iota
	fURL
	fKind
	fToken
	fMirrorToken
)

var formLabels = []string{"Nome", "URL", "Tipo", "Token de API", "Token de mirror"}

type instanceSavedMsg struct {
	in  config.Instance
	err error
}

func newInstanceForm(sh *shared, existing *config.Instance) *instanceForm {
	f := &instanceForm{sh: sh, editing: existing != nil, spin: spinner.New(spinner.WithSpinner(spinner.Dot))}
	placeholders := []string{
		"ex.: casa, codeberg, github",
		"ex.: https://git.exemplo.com",
		"gitea | forgejo | github (vazio = detectar)",
		"cole o token",
		"opcional: token gravado nos mirrors que apontam para cá",
	}
	for i := range formLabels {
		ti := textinput.New()
		ti.Placeholder = placeholders[i]
		ti.Width = 50
		if i == fToken || i == fMirrorToken {
			ti.EchoMode = textinput.EchoPassword
		}
		f.inputs = append(f.inputs, ti)
	}
	if existing != nil {
		f.inputs[fName].SetValue(existing.Name)
		f.inputs[fURL].SetValue(existing.URL)
		f.inputs[fKind].SetValue(string(existing.Kind))
		f.inputs[fToken].Placeholder = "vazio = manter o atual"
		f.inputs[fMirrorToken].Placeholder = "vazio = manter o atual"
		f.focus = fURL
	}
	f.inputs[f.focus].Focus()
	return f
}

func (f *instanceForm) Title() string {
	if f.editing {
		return "Editar instância"
	}
	return "Nova instância"
}
func (f *instanceForm) Keys() string {
	return "tab/↓ próximo • shift+tab/↑ anterior • enter salvar no último campo • ctrl+s salvar • esc cancelar"
}
func (f *instanceForm) Init() tea.Cmd { return textinput.Blink }

func (f *instanceForm) setFocus(i int) tea.Cmd {
	// no modo edição o nome é a chave no keyring: não pode mudar
	lo := 0
	if f.editing {
		lo = fURL
	}
	n := len(f.inputs)
	i = lo + ((i-lo)%(n-lo)+(n-lo))%(n-lo)
	f.inputs[f.focus].Blur()
	f.focus = i
	return f.inputs[i].Focus()
}

func (f *instanceForm) Update(msg tea.Msg) (screen, tea.Cmd) {
	switch msg := msg.(type) {
	case spinner.TickMsg:
		if f.saving {
			var cmd tea.Cmd
			f.spin, cmd = f.spin.Update(msg)
			return f, cmd
		}
	case instanceSavedMsg:
		f.saving = false
		if msg.err != nil {
			f.err = msg.err
			return f, nil
		}
		return f, pop()
	case tea.KeyMsg:
		if f.saving {
			return f, nil
		}
		switch msg.String() {
		case "esc":
			return f, pop()
		case "tab", "down":
			return f, f.setFocus(f.focus + 1)
		case "shift+tab", "up":
			return f, f.setFocus(f.focus - 1)
		case "ctrl+s":
			return f, f.save()
		case "enter":
			if f.focus == len(f.inputs)-1 {
				return f, f.save()
			}
			return f, f.setFocus(f.focus + 1)
		}
		var cmd tea.Cmd
		f.inputs[f.focus], cmd = f.inputs[f.focus].Update(msg)
		return f, cmd
	}
	return f, nil
}

func (f *instanceForm) save() tea.Cmd {
	val := func(i int) string { return strings.TrimSpace(f.inputs[i].Value()) }
	in := config.Instance{Name: val(fName), URL: val(fURL), Kind: config.Kind(strings.ToLower(val(fKind)))}
	if in.Kind != "" && !in.Kind.Valid() {
		f.err = fmt.Errorf("tipo inválido %q: use gitea, forgejo, github ou deixe vazio", in.Kind)
		return nil
	}
	if !f.editing {
		if _, exists := f.sh.env.Cfg.Find(in.Name); exists {
			f.err = fmt.Errorf("já existe uma instância chamada %q", in.Name)
			return nil
		}
	}
	token, mirrorToken := val(fToken), val(fMirrorToken)
	f.saving, f.err = true, nil
	sh := f.sh
	return tea.Batch(f.spin.Tick, func() tea.Msg {
		saved, err := sh.env.AddInstance(sh.ctx, in, token, mirrorToken)
		return instanceSavedMsg{saved, err}
	})
}

func (f *instanceForm) View() string {
	var b strings.Builder
	b.WriteString("\n")
	for i, ti := range f.inputs {
		label := labelStyle.Render(formLabels[i])
		if i == f.focus {
			label = cursorStyle.Width(18).Render(formLabels[i])
		}
		b.WriteString(label + ti.View() + "\n")
	}
	b.WriteString("\n" + helpStyle.Render(
		"Os tokens vão para o keyring do sistema. O token de mirror fica gravado no servidor de origem;\n"+
			"no GitHub, use um fine-grained PAT com Contents + Workflows: write.") + "\n\n")
	if f.saving {
		b.WriteString(f.spin.View() + " validando token e salvando…\n")
	}
	if f.err != nil {
		b.WriteString(errStyle.Render("✘ "+f.err.Error()) + "\n")
	}
	return b.String()
}
