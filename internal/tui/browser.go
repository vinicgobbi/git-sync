package tui

import (
	"errors"
	"os/exec"
	"runtime"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// openBrowser abre a URL no navegador padrão do sistema.
func openBrowser(url string) tea.Cmd {
	return func() tea.Msg {
		if url == "" {
			return toastMsg{"Sem URL para abrir", toastErr}
		}
		var cmd *exec.Cmd
		switch runtime.GOOS {
		case "darwin":
			cmd = exec.Command("open", url)
		case "windows":
			cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
		default:
			cmd = exec.Command("xdg-open", url)
		}
		if err := cmd.Start(); err != nil {
			if errors.Is(err, exec.ErrNotFound) {
				return toastMsg{"Nenhum navegador encontrado. Endereço: " + url, toastInfo}
			}
			return toastMsg{"Não foi possível abrir o navegador: " + err.Error(), toastErr}
		}
		go cmd.Wait() //nolint:errcheck // não bloqueia a TUI
		return toastMsg{"Abrindo " + url, toastInfo}
	}
}

// webURL converte uma URL de clone (https://host/owner/repo.git) na página web.
func webURL(clone string) string {
	s := strings.TrimSuffix(strings.TrimSuffix(clone, "/"), ".git")
	if i := strings.Index(s, "://"); i >= 0 {
		rest := s[i+3:]
		if at := strings.Index(rest, "@"); at >= 0 && at < strings.Index(rest+"/", "/") {
			s = s[:i+3] + rest[at+1:]
		}
	}
	return s
}
