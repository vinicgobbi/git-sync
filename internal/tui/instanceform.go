package tui

import (
	"net"
	"net/url"
	"strings"

	"github.com/charmbracelet/bubbles/cursor"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/vinicgobbi/git-sync/internal/config"
	"github.com/vinicgobbi/git-sync/internal/secrets"
)

type formField int

const (
	ffKind formField = iota
	ffURL
	ffName
	ffToken
	ffMirrorToken
	ffSave
)

var kindOptions = []struct {
	kind  config.Kind
	label string
}{
	{"", "Detectar automaticamente"},
	{config.KindGitea, "Gitea"},
	{config.KindForgejo, "Forgejo"},
	{config.KindGitHub, "GitHub"},
}

// instanceForm cadastra ou edita uma instância.
type instanceForm struct {
	sh          *shared
	editing     *config.Instance
	kindIdx     int
	url         textinput.Model
	name        textinput.Model
	token       textinput.Model
	mirrorToken textinput.Model
	nameTouched bool
	fields      []formField
	focus       int
	showTokens  bool
	spin        spinner.Model
	saving      bool
	fieldErr    map[formField]string
	err         error
	credChanged bool // um token novo foi informado ao editar
}

type instanceSavedMsg struct {
	in  config.Instance
	err error
}

var (
	fkSave   = bind([]string{"ctrl+s"}, "ctrl+s", "salvar")
	fkReveal = bind([]string{"ctrl+t"}, "ctrl+t", "mostrar tokens")
	fkNext   = bind([]string{"tab", "down"}, "tab/↓", "próximo")
	fkPrev   = bind([]string{"shift+tab", "up"}, "shift+tab/↑", "anterior")
	fkChoose = bind([]string{"left", "right"}, "←/→", "escolher")
	fkCancel = bind([]string{"esc"}, "esc", "cancelar")
)

// cursorMode é o cursor dos campos de texto; os testes usam um fixo, porque
// o piscar é um comando com espera que o driver síncrono executaria.
var cursorMode = cursor.CursorBlink

func newInput(placeholder string, password bool) textinput.Model {
	ti := textinput.New()
	_ = ti.Cursor.SetMode(cursorMode)
	ti.Prompt = ""
	ti.Placeholder = placeholder
	ti.Width = 48
	if password {
		ti.EchoMode = textinput.EchoPassword
		ti.EchoCharacter = '•'
	}
	return ti
}

func newInstanceForm(sh *shared, existing *config.Instance) *instanceForm {
	f := &instanceForm{
		sh:          sh,
		editing:     existing,
		url:         newInput("https://git.exemplo.com", false),
		name:        newInput("ex.: casa, codeberg, github", false),
		token:       newInput("cole o token aqui", true),
		mirrorToken: newInput("opcional", true),
		spin:        spinner.New(spinner.WithSpinner(spinner.MiniDot)),
		fieldErr:    map[formField]string{},
	}
	if existing != nil {
		for i, o := range kindOptions {
			if o.kind == existing.Kind {
				f.kindIdx = i
			}
		}
		f.url.SetValue(existing.URL)
		f.name.SetValue(existing.Name)
		f.nameTouched = true
	}
	f.rebuild()
	if existing != nil {
		f.focusField(ffURL)
	}
	return f
}

func (f *instanceForm) Title() string {
	if f.editing != nil {
		return "Editar " + f.editing.Name
	}
	return "Adicionar instância"
}

func (f *instanceForm) Capturing() bool { return f.input() != nil }

func (f *instanceForm) Keys() []key.Binding {
	ks := []key.Binding{fkNext, fkPrev}
	if !f.Capturing() {
		ks = append(ks, fkChoose)
	}
	return append(ks, fkSave, fkReveal, fkCancel)
}

func (f *instanceForm) Init() tea.Cmd { return textinput.Blink }

func (f *instanceForm) kind() config.Kind { return kindOptions[f.kindIdx].kind }

// effKind é o tipo escolhido ou, em "Detectar", o que dá para saber pela URL.
func (f *instanceForm) effKind() config.Kind {
	if k := f.kind(); k != "" {
		return k
	}
	if u, err := url.Parse(strings.TrimSpace(f.url.Value())); err == nil && strings.EqualFold(u.Hostname(), "github.com") {
		return config.KindGitHub
	}
	return ""
}

// needsToken: instância nova, ou de uma configuração antiga sem token.
func (f *instanceForm) needsToken() bool {
	return f.editing == nil || f.editing.LegacyAuth != ""
}

// hasMirrorToken: a instância editada já tem token de mirror salvo.
func (f *instanceForm) hasMirrorToken() bool {
	if f.editing == nil {
		return false
	}
	_, err := f.sh.env.Store.Get(secrets.MirrorKey(f.editing.Name))
	return err == nil
}

func (f *instanceForm) needsMirrorToken() bool {
	return f.effKind() == config.KindGitHub && !f.hasMirrorToken()
}

func (f *instanceForm) rebuild() {
	cur := ffKind
	if f.focus < len(f.fields) {
		cur = f.fields[f.focus]
	}
	f.fields = []formField{ffKind, ffURL}
	if f.editing == nil {
		f.fields = append(f.fields, ffName)
	}
	f.fields = append(f.fields, ffToken, ffMirrorToken, ffSave)
	f.focus = 0
	for i, ff := range f.fields {
		if ff == cur {
			f.focus = i
		}
	}
	f.updatePlaceholders()
}

func (f *instanceForm) updatePlaceholders() {
	switch {
	case f.editing != nil && f.editing.LegacyAuth != "":
		f.token.Placeholder = "obrigatório: a instância usava o login do " + f.editing.LegacyAuth
	case f.editing != nil:
		f.token.Placeholder = "deixe vazio para manter o atual"
	default:
		f.token.Placeholder = "cole o token aqui"
	}
	switch {
	case f.needsMirrorToken():
		f.mirrorToken.Placeholder = "obrigatório no GitHub"
	case f.hasMirrorToken():
		f.mirrorToken.Placeholder = "deixe vazio para manter o atual"
	default:
		f.mirrorToken.Placeholder = "opcional"
	}
}

func (f *instanceForm) current() formField { return f.fields[f.focus] }

func (f *instanceForm) inputFor(ff formField) *textinput.Model {
	switch ff {
	case ffURL:
		return &f.url
	case ffName:
		return &f.name
	case ffToken:
		return &f.token
	case ffMirrorToken:
		return &f.mirrorToken
	}
	return nil
}

func (f *instanceForm) input() *textinput.Model { return f.inputFor(f.current()) }

func (f *instanceForm) focusField(ff formField) tea.Cmd {
	for i, x := range f.fields {
		if x == ff {
			return f.move(i - f.focus)
		}
	}
	return nil
}

func (f *instanceForm) move(delta int) tea.Cmd {
	if in := f.input(); in != nil {
		in.Blur()
	}
	f.focus = (f.focus + delta + len(f.fields)) % len(f.fields)
	if in := f.input(); in != nil {
		in.CursorEnd()
		return in.Focus()
	}
	return nil
}

func (f *instanceForm) cycle(delta int) {
	switch f.current() {
	case ffKind:
		f.kindIdx = (f.kindIdx + delta + len(kindOptions)) % len(kindOptions)
		if f.kind() == config.KindGitHub && strings.TrimSpace(f.url.Value()) == "" {
			f.url.SetValue("https://github.com")
			f.suggestName()
		}
		f.rebuild()
	}
}

// suggestName propõe um nome a partir do host (git.faesa.br → faesa).
func (f *instanceForm) suggestName() {
	if f.nameTouched || f.editing != nil {
		return
	}
	u, err := url.Parse(strings.TrimSpace(f.url.Value()))
	if err != nil || !strings.Contains(u.Hostname(), ".") || net.ParseIP(u.Hostname()) != nil {
		f.name.SetValue("") // sem sugestão boa: não deixa uma parcial antiga
		return
	}
	labels := strings.Split(strings.TrimSuffix(u.Hostname(), "."), ".")
	for len(labels) > 2 {
		switch labels[0] {
		case "www", "git", "gitea", "forgejo", "code", "scm":
			labels = labels[1:]
			continue
		}
		break
	}
	f.name.SetValue(labels[0])
	f.name.CursorEnd()
}

func (f *instanceForm) Update(msg tea.Msg) (view, tea.Cmd) {
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
		text := msg.in.Name + " conectada como " + msg.in.User
		cmds := []tea.Cmd{closeFlow(), notify(toastOK, text)}
		if f.editing != nil && f.credChanged {
			// os espelhos que já enviam para cá guardam o token antigo
			name := msg.in.Name
			cmds = append(cmds, func() tea.Msg { return credChangedMsg{name} })
		}
		return f, tea.Batch(cmds...)
	case tea.KeyMsg:
		if f.saving {
			return f, nil
		}
		return f.handleKey(msg)
	}
	return f, nil
}

func (f *instanceForm) handleKey(msg tea.KeyMsg) (view, tea.Cmd) {
	switch {
	case key.Matches(msg, fkCancel):
		return f, closeFlow()
	case key.Matches(msg, fkSave):
		return f, f.save()
	case key.Matches(msg, fkReveal):
		f.showTokens = !f.showTokens
		mode := textinput.EchoPassword
		if f.showTokens {
			mode = textinput.EchoNormal
		}
		f.token.EchoMode, f.mirrorToken.EchoMode = mode, mode
		return f, nil
	case key.Matches(msg, fkNext):
		return f, f.move(1)
	case key.Matches(msg, fkPrev):
		return f, f.move(-1)
	case msg.String() == "enter":
		if f.current() == ffSave {
			return f, f.save()
		}
		return f, f.move(1)
	}

	if in := f.input(); in != nil {
		var cmd tea.Cmd
		*in, cmd = in.Update(msg)
		delete(f.fieldErr, f.current())
		switch f.current() {
		case ffURL:
			f.suggestName()
			f.updatePlaceholders()
		case ffName:
			f.nameTouched = true
		}
		return f, cmd
	}
	switch msg.String() {
	case "left", "h":
		f.cycle(-1)
	case "right", "l", " ":
		f.cycle(1)
	}
	return f, nil
}

func (f *instanceForm) validate() bool {
	f.fieldErr = map[formField]string{}
	u, err := url.Parse(strings.TrimSpace(f.url.Value()))
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
		f.fieldErr[ffURL] = "informe o endereço completo, com https://"
	}
	name := strings.TrimSpace(f.name.Value())
	switch {
	case name == "":
		f.fieldErr[ffName] = "dê um nome curto para reconhecer a instância"
	case strings.ContainsAny(name, " /"):
		f.fieldErr[ffName] = "use um nome sem espaços nem barras"
	case f.editing == nil:
		if _, exists := f.sh.env.Cfg.Find(name); exists {
			f.fieldErr[ffName] = "já existe uma instância com esse nome"
		}
	}
	if f.needsToken() && strings.TrimSpace(f.token.Value()) == "" {
		f.fieldErr[ffToken] = "cole o token de acesso"
	}
	if f.needsMirrorToken() && strings.TrimSpace(f.mirrorToken.Value()) == "" {
		f.fieldErr[ffMirrorToken] = "o GitHub precisa de um token de mirror"
	}
	for i, ff := range f.fields {
		if _, bad := f.fieldErr[ff]; bad {
			f.move(i - f.focus)
			return false
		}
	}
	return true
}

func (f *instanceForm) save() tea.Cmd {
	f.err = nil
	if !f.validate() {
		return nil
	}
	in := config.Instance{
		Name: strings.TrimSpace(f.name.Value()),
		URL:  strings.TrimSpace(f.url.Value()),
		Kind: f.kind(),
	}
	token, mirrorToken := strings.TrimSpace(f.token.Value()), strings.TrimSpace(f.mirrorToken.Value())
	f.credChanged = token != "" || mirrorToken != ""
	f.saving = true
	sh := f.sh
	return tea.Batch(f.spin.Tick, func() tea.Msg {
		saved, err := sh.env.AddInstance(sh.ctx, in, token, mirrorToken)
		return instanceSavedMsg{saved, err}
	})
}

func (f *instanceForm) hint(ff formField) string {
	switch ff {
	case ffKind:
		return "Gitea e Forgejo podem ser origem e destino. O GitHub é sempre destino."
	case ffURL:
		return "Endereço que você abre no navegador."
	case ffName:
		return "Apelido usado na interface. Sugerido a partir do endereço."
	}
	return ""
}

// guide é o painel sob o formulário: como criar o token do campo em foco
// ou, nos demais campos, quais tokens o tipo precisa.
func (f *instanceForm) guide(w int) string {
	base := strings.TrimSuffix(strings.TrimSpace(f.url.Value()), "/")
	if ff := f.current(); ff == ffToken || ff == ffMirrorToken {
		title, lines := tokenGuide(f.effKind(), base, ff == ffMirrorToken)
		return renderGuide(title, lines, w)
	}
	return sBold.Render("Tokens") + "\n" + wrap(tokenOverview(f.effKind()), w)
}

func (f *instanceForm) View(w, h int) string {
	cw := min(w-4, 92)
	labels := map[formField]string{
		ffKind: "Tipo", ffURL: "Endereço", ffName: "Nome",
		ffToken: "Token de acesso", ffMirrorToken: "Token de mirror",
	}
	var b strings.Builder
	b.WriteString("\n")
	for i, ff := range f.fields {
		focused := i == f.focus
		if ff == ffSave {
			btn := sButton.Render("Testar e salvar")
			if focused {
				btn = sButtonFocus.Foreground(colAccent).Render("Testar e salvar")
			}
			b.WriteString("\n  " + strings.Repeat(" ", 18) + btn + "\n")
			continue
		}
		label := sLabel.Width(18).Render(labels[ff])
		marker := "  "
		if focused {
			label = sAccent.Bold(true).Width(18).Render(labels[ff])
			marker = sAccent.Render("› ")
		}
		var value string
		switch ff {
		case ffKind:
			value = selector(kindOptions[f.kindIdx].label, focused)
		default:
			value = f.inputFor(ff).View()
		}
		b.WriteString(marker + label + value + "\n")
		if msg, bad := f.fieldErr[ff]; bad {
			b.WriteString(strings.Repeat(" ", 20) + sErr.Render("✘ "+msg) + "\n")
		} else if hint := f.hint(ff); focused && hint != "" {
			b.WriteString(indentBlock(sMuted.Render(wrap(hint, cw-22)), 20) + "\n")
		}
	}

	switch {
	case f.saving:
		b.WriteString("\n  " + f.spin.View() + " Testando a conexão…\n")
	case f.err != nil:
		fr := humanize(f.err)
		text := sErr.Render("✘ Não foi possível salvar: ") + fr.msg
		if fr.hint != "" {
			text += "\n" + sMuted.Render(wrap(fr.hint, cw-6))
		}
		b.WriteString("\n" + lipgloss.NewStyle().MarginLeft(2).Border(lipgloss.NormalBorder(), false, false, false, true).
			BorderForeground(colErr).PaddingLeft(1).Render(text) + "\n")
	}
	b.WriteString("\n" + lipgloss.NewStyle().MarginLeft(2).Border(lipgloss.NormalBorder(), false, false, false, true).
		BorderForeground(colAccent).PaddingLeft(1).Render(f.guide(cw-6)) + "\n")
	return b.String()
}

func selector(label string, focused bool) string {
	if focused {
		return sAccent.Render("‹ ") + sBold.Render(label) + sAccent.Render(" ›")
	}
	return label
}

func indentBlock(s string, n int) string {
	pad := strings.Repeat(" ", n)
	return pad + strings.ReplaceAll(s, "\n", "\n"+pad)
}
