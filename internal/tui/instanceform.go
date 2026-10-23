package tui

import (
	"net"
	"net/url"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/vinicgobbi/git-sync/internal/config"
)

type formField int

const (
	ffKind formField = iota
	ffURL
	ffName
	ffAuth
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
	auth        config.Auth
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

func newInput(placeholder string, password bool) textinput.Model {
	ti := textinput.New()
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
		f.auth = existing.Auth
		if existing.Auth == config.AuthKeyring {
			f.token.Placeholder = "deixe vazio para manter o atual"
		}
		f.mirrorToken.Placeholder = "deixe vazio para manter o atual"
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

// authOptions depende do tipo: tea serve Gitea/Forgejo, gh serve GitHub.
func (f *instanceForm) authOptions() []config.Auth {
	switch f.kind() {
	case config.KindGitHub:
		return []config.Auth{config.AuthKeyring, config.AuthGh}
	case config.KindGitea, config.KindForgejo:
		return []config.Auth{config.AuthKeyring, config.AuthTea}
	}
	return []config.Auth{config.AuthKeyring, config.AuthTea, config.AuthGh}
}

func authOptionLabel(a config.Auth) string {
	switch a {
	case config.AuthTea:
		return "Usar o login do tea"
	case config.AuthGh:
		return "Usar o login do gh"
	}
	return "Token de acesso"
}

func (f *instanceForm) rebuild() {
	cur := ffKind
	if f.focus < len(f.fields) {
		cur = f.fields[f.focus]
	}
	opts := f.authOptions()
	valid := false
	for _, a := range opts {
		valid = valid || a == f.auth
	}
	if !valid {
		f.auth = config.AuthKeyring
	}
	f.fields = []formField{ffKind, ffURL}
	if f.editing == nil {
		f.fields = append(f.fields, ffName)
	}
	f.fields = append(f.fields, ffAuth)
	if f.auth == config.AuthKeyring {
		f.fields = append(f.fields, ffToken)
	}
	f.fields = append(f.fields, ffMirrorToken, ffSave)
	f.focus = 0
	for i, ff := range f.fields {
		if ff == cur {
			f.focus = i
		}
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
	case ffAuth:
		opts := f.authOptions()
		i := 0
		for j, a := range opts {
			if a == f.auth {
				i = j
			}
		}
		f.auth = opts[(i+delta+len(opts))%len(opts)]
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
		return f, tea.Batch(closeFlow(), notify(toastOK, text))
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
	if f.auth == config.AuthKeyring && f.editing == nil && strings.TrimSpace(f.token.Value()) == "" {
		f.fieldErr[ffToken] = "cole o token de acesso"
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
		Auth: f.auth,
	}
	token, mirrorToken := strings.TrimSpace(f.token.Value()), strings.TrimSpace(f.mirrorToken.Value())
	f.saving = true
	sh := f.sh
	return tea.Batch(f.spin.Tick, func() tea.Msg {
		saved, err := sh.env.AddInstance(sh.ctx, in, token, mirrorToken)
		return instanceSavedMsg{saved, err}
	})
}

func (f *instanceForm) hint(ff formField) string {
	base := strings.TrimSuffix(strings.TrimSpace(f.url.Value()), "/")
	if base == "" {
		base = "<endereço>"
	}
	switch ff {
	case ffKind:
		return "Gitea e Forgejo podem ser origem e destino. O GitHub é sempre destino."
	case ffURL:
		return "Endereço que você abre no navegador."
	case ffName:
		return "Apelido usado na interface. Sugerido a partir do endereço."
	case ffAuth:
		switch f.auth {
		case config.AuthTea:
			return "Usa o login que o tea já tem para este endereço, inclusive OAuth. Nenhum token é copiado: o git-sync pede ao tea quando precisa, e o tea renova o OAuth sozinho."
		case config.AuthGh:
			return "Usa o login do GitHub CLI (gh auth login). Nenhum token é copiado."
		}
		return "Um token que você cria na instância. Fica guardado no keyring do sistema."
	case ffToken:
		if f.kind() == config.KindGitHub {
			return "Crie em github.com/settings/personal-access-tokens/new com acesso aos repositórios e permissões Administration: write (criar repositórios) e Contents: read."
		}
		return "Crie em " + base + "/user/settings/applications com leitura e escrita em repository e leitura em user e organization."
	case ffMirrorToken:
		if f.kind() == config.KindGitHub {
			return "Credencial que a origem usa para enviar para cá. Use um fine-grained PAT com Contents: write e Workflows: write; sem Workflows, pushes que alteram .github/workflows falham."
		}
		if f.auth == config.AuthTea {
			return "Necessário para usar esta instância como destino se o login do tea for OAuth (tokens OAuth expiram). Token com escrita em repository."
		}
		return "Credencial que a origem usa para enviar para cá. Se vazio, usa a credencial da API. Token com escrita em repository."
	}
	return ""
}

func (f *instanceForm) View(w, h int) string {
	cw := min(w-4, 92)
	labels := map[formField]string{
		ffKind: "Tipo", ffURL: "Endereço", ffName: "Nome", ffAuth: "Credencial",
		ffToken: "Token de acesso", ffMirrorToken: "Token de mirror",
	}
	var b strings.Builder
	b.WriteString("\n")
	for i, ff := range f.fields {
		focused := i == f.focus
		if ff == ffSave {
			btn := lipgloss.NewStyle().Padding(0, 2).Foreground(colMuted).Background(colFaint).Render("Testar e salvar")
			if focused {
				btn = lipgloss.NewStyle().Padding(0, 2).Bold(true).Foreground(lipgloss.Color("#FFFFFF")).Background(colAccent).Render("Testar e salvar")
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
		case ffAuth:
			value = selector(authOptionLabel(f.auth), focused)
		default:
			value = f.inputFor(ff).View()
		}
		b.WriteString(marker + label + value + "\n")
		if msg, bad := f.fieldErr[ff]; bad {
			b.WriteString(strings.Repeat(" ", 20) + sErr.Render("✘ "+msg) + "\n")
		} else if focused {
			b.WriteString(indentBlock(sMuted.Render(wrap(f.hint(ff), cw-22)), 20) + "\n")
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
