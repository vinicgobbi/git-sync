package tui

import "github.com/charmbracelet/bubbles/key"

func bind(keys []string, k, desc string) key.Binding {
	return key.NewBinding(key.WithKeys(keys...), key.WithHelp(k, desc))
}

// Atalhos reutilizados por várias telas.
var (
	kUp      = bind([]string{"up", "k"}, "↑/k", "subir")
	kDown    = bind([]string{"down", "j"}, "↓/j", "descer")
	kEnter   = bind([]string{"enter"}, "enter", "continuar")
	kBack    = bind([]string{"esc"}, "esc", "voltar")
	kToggle  = bind([]string{" "}, "espaço", "marcar")
	kRefresh = bind([]string{"r"}, "r", "atualizar")
	kHelp    = bind([]string{"?"}, "?", "ajuda")
	kQuit    = bind([]string{"q"}, "q", "sair")
	kTabs    = bind([]string{"tab", "shift+tab", "1", "2", "3"}, "1-3/tab", "trocar aba")
)

// keyMap adapta uma lista de atalhos à interface do componente de ajuda.
type keyMap struct {
	view   []key.Binding
	global []key.Binding
}

func (k keyMap) ShortHelp() []key.Binding {
	return append(append([]key.Binding{}, k.view...), k.global...)
}

func (k keyMap) FullHelp() [][]key.Binding {
	var cols [][]key.Binding
	for i := 0; i < len(k.view); i += 6 {
		cols = append(cols, k.view[i:min(i+6, len(k.view))])
	}
	return append(cols, k.global)
}
