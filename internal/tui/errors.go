package tui

import (
	"context"
	"errors"
	"net"
	"net/url"
	"strconv"
	"strings"

	"github.com/vinicgobbi/git-sync/internal/app"
	"github.com/vinicgobbi/git-sync/internal/forge"
	"github.com/vinicgobbi/git-sync/internal/secrets"
)

// friendly traduz um erro técnico em uma mensagem curta e uma dica de solução.
type friendly struct {
	msg  string
	hint string
}

func humanize(err error) friendly {
	if err == nil {
		return friendly{}
	}
	var api *forge.APIError
	var uerr *url.Error
	var nerr net.Error
	var scope *app.ScopeError
	switch {
	case errors.As(err, &scope):
		return friendly{"O token " + scope.Token + " do GitHub não tem " + strings.Join(scope.Missing, ", "),
			"Gere outro token classic com os escopos " + strings.Join(scope.Need, ", ") + " e edite a instância."}
	case errors.Is(err, app.ErrLegacyAuth):
		return friendly{"Sem token de acesso", "Esta instância usava o login do tea ou do gh, que não é mais suportado. Edite-a e informe um token."}
	case errors.Is(err, app.ErrNoMirrorToken):
		return friendly{"Falta o token de mirror", "Destinos GitHub precisam de um token de mirror classic (escopos repo e workflow). Edite a instância e informe-o."}
	case errors.Is(err, secrets.ErrNotFound):
		return friendly{"Token não encontrado no keyring", "Edite a instância em Instâncias e informe o token."}
	case errors.Is(err, context.DeadlineExceeded):
		return friendly{"A instância demorou demais para responder", "Verifique sua conexão e tente de novo."}
	case errors.Is(err, context.Canceled):
		return friendly{"Operação cancelada", ""}
	case errors.Is(err, forge.ErrNotFound):
		return friendly{"Não encontrado", "Confira a URL, o usuário/organização e se o token tem acesso."}
	case errors.As(err, &api):
		return humanizeAPI(api)
	case errors.As(err, &uerr) && uerr.Timeout(), errors.As(err, &nerr) && nerr.Timeout():
		return friendly{"A instância demorou demais para responder", "Verifique sua conexão e tente de novo."}
	case errors.As(err, &uerr):
		host := uerr.URL
		if u, perr := url.Parse(uerr.URL); perr == nil {
			host = u.Host
		}
		return friendly{"Não foi possível conectar a " + host, "Confira a URL da instância e sua conexão de rede."}
	}
	return friendly{msg: oneLine(err.Error())}
}

func humanizeAPI(e *forge.APIError) friendly {
	body := strings.ToLower(e.Body)
	switch {
	case e.Status == 401:
		return friendly{"Token inválido ou expirado", "Gere um novo token e atualize a instância em Instâncias."}
	case e.Status == 403 && e.Accepted != "" && !strings.Contains(body, "rate limit"):
		return friendly{"O token não tem permissão para esta operação", "O GitHub aceita aqui: " + e.Accepted + ". Edite a instância com um token que tenha isso."}
	case e.Status == 403 && strings.Contains(body, "rate limit"):
		return friendly{"Limite de requisições da API atingido", "Aguarde alguns minutos e tente de novo."}
	case e.Status == 403:
		return friendly{"O token não tem permissão para esta operação", "Confira os escopos/permissões do token."}
	case e.Status == 409 || (e.Status == 422 && strings.Contains(body, "already exists")):
		return friendly{"Já existe um repositório com esse nome", "Escolha outro usuário/organização de destino."}
	case e.Status == 400 && strings.Contains(body, "mirror"):
		return friendly{"A instância recusou o espelhamento: " + firstSentence(e.Body), "Talvez o administrador tenha desativado push mirrors ou bloqueado o endereço de destino."}
	case e.Status >= 500:
		return friendly{"A instância respondeu com erro interno (HTTP " + strconv.Itoa(e.Status) + ")", "Tente de novo em instantes."}
	}
	return friendly{msg: "HTTP " + strconv.Itoa(e.Status) + ": " + firstSentence(e.Body)}
}

// mirrorHint sugere uma solução para o last_error de um push mirror.
func mirrorHint(lastError string) string {
	l := strings.ToLower(lastError)
	switch {
	case strings.Contains(l, "workflow") && (strings.Contains(l, "scope") || strings.Contains(l, "permission")):
		return "O token do destino não pode alterar .github/workflows. No GitHub, dê a permissão Workflows: write ao token de mirror."
	case strings.Contains(l, "authentication failed"), strings.Contains(l, "401"), strings.Contains(l, "invalid username or password"):
		return "A credencial gravada no espelho foi recusada (o token venceu ou foi revogado). Gere um token novo, " +
			"atualize a instância de destino em Instâncias e renove a credencial dos espelhos (c)."
	case strings.Contains(l, "403"), strings.Contains(l, "permission"), strings.Contains(l, "denied"):
		return "O token de mirror não tem permissão de escrita no repositório de destino."
	case strings.Contains(l, "could not resolve host"), strings.Contains(l, "connection refused"), strings.Contains(l, "timed out"):
		return "O servidor de origem não consegue alcançar o destino. Verifique a URL e a rede do servidor."
	case strings.Contains(l, "rejected"), strings.Contains(l, "protected branch"):
		return "O destino recusou o push, talvez por proteção de branch. Libere force-push para o token do espelho."
	}
	return ""
}

func firstSentence(s string) string {
	s = oneLine(s)
	// corpos JSON do Gitea/GitHub: {"message":"..."}
	if i := strings.Index(s, `"message":"`); i >= 0 {
		s = s[i+len(`"message":"`):]
		if j := strings.Index(s, `"`); j >= 0 {
			s = s[:j]
		}
	}
	if len(s) > 160 {
		s = s[:160] + "…"
	}
	return s
}
