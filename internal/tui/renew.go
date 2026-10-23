package tui

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/vinicgobbi/git-sync/internal/app"
)

type (
	// credChangedMsg: um token de name foi trocado ao editar a instância.
	credChangedMsg struct{ name string }
	renewedMsg     struct {
		name string
		res  app.RenewResult
		err  error
	}
)

var kRenew = bind([]string{"c"}, "c", "renovar credencial")

// renew pede confirmação e grava a credencial atual de name em todos os
// espelhos que enviam para ela. n é quantos são, ou -1 se não se sabe.
func renew(sh *shared, name string, n int) tea.Cmd {
	title := "Renovar a credencial dos espelhos para " + name + "?"
	if n > 0 {
		title = fmt.Sprintf("Renovar a credencial de %s para %s?", plural(n, "espelho", "espelhos"), name)
	}
	body := "Cada espelho guarda uma cópia do token de quando foi criado. Os que enviam para " + name +
		" serão recriados com o token atual, com o mesmo endereço, intervalo e envio a cada push.\n\n" +
		"O espelho novo é criado antes de o antigo ser apagado: se algo falhar, o antigo continua."
	return confirm(title, body, "Renovar", false, tea.Batch(
		notify(toastInfo, "Renovando a credencial dos espelhos para "+name+"…"),
		func() tea.Msg {
			res, err := sh.env.RenewMirrors(sh.ctx, name)
			return renewedMsg{name, res, err}
		},
	))
}

func renewedToast(msg renewedMsg) tea.Cmd {
	res := msg.res
	switch {
	case msg.err != nil:
		return notifyErr(msg.err)
	case res.Found == 0 && len(res.Errors) == 0:
		return notify(toastInfo, "Nenhum espelho envia para "+msg.name+".")
	case len(res.Errors) > 0:
		return notify(toastErr, fmt.Sprintf("%d de %d espelhos para %s renovados. Primeiro erro: %s",
			res.Renewed, res.Found, msg.name, humanize(res.Errors[0]).msg))
	}
	return notify(toastOK, plural(res.Renewed, "espelho para "+msg.name+" renovado", "espelhos para "+msg.name+" renovados")+
		" com o token atual.")
}
