package tui

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
)

type installedMsg struct {
	text string
	err  error
}

// installItem copia este binário para o PATH. Some do menu quando o
// git-sync que o PATH encontra já é este (ou uma cópia idêntica).
func installItem(sh *shared) item {
	return item{
		label: "Instalar no PATH", view: -1,
		hidden: func() bool { return sh.installer == nil || sh.install.Installed },
		desc: func() string {
			in, st := sh.installer, sh.install
			s := "Copia este git-sync (v" + sh.version + ") para " + st.Dir + " e garante que a pasta está no " +
				"PATH, para chamar o git-sync de qualquer terminal. Não precisa de admin."
			if st.Existing != "" {
				s += "\n\nJá existe outra versão em " + st.Existing + ": ela será substituída por esta."
			}
			if !in.OnPath(st.Dir) {
				s += "\n\n" + st.Dir + " ainda não está no PATH. " + in.PathChange(st.Dir)
			}
			return s
		},
		cli: "git-sync install [--dir pasta]",
		run: func() tea.Cmd {
			in, dir := sh.installer, sh.install.Dir
			body := "O git-sync será copiado para " + dir + "."
			if sh.install.Existing != "" {
				body += "\n\nA versão em " + sh.install.Existing + " será substituída."
			}
			if !in.OnPath(dir) {
				body += "\n\n" + in.PathChange(dir)
			}
			return confirm("Instalar o git-sync?", body, "Instalar", false, func() tea.Msg {
				dst, _, err := in.Copy(dir)
				if err != nil {
					return installedMsg{err: fmt.Errorf("instalando em %s: %w", dir, err)}
				}
				if in.OnPath(dir) {
					return installedMsg{text: "git-sync instalado em " + dst + ". Rode `git-sync` em qualquer terminal."}
				}
				next, err := in.AddToPath(dir)
				if err != nil {
					return installedMsg{err: err}
				}
				return installedMsg{text: "git-sync instalado em " + dst + ". " + next}
			})
		},
	}
}
