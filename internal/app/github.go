package app

import (
	"context"
	"fmt"
	"strings"

	"github.com/vinicgobbi/git-sync/internal/forge/github"
)

// scopeNeed é um escopo de token classic do GitHub e os que também o cobrem.
type scopeNeed struct {
	name  string
	also  []string
	about string // para que serve, nas mensagens
}

var (
	// GitHubAPIScopes: o token de acesso cria repositórios (repo) e lista as
	// organizações onde criá-los (read:org).
	GitHubAPIScopes = []scopeNeed{
		{name: "repo", about: "criar e ler repositórios"},
		{name: "read:org", also: []string{"write:org", "admin:org"}, about: "listar suas organizações"},
	}
	// GitHubAPIRecommended: sem delete_repo, um repositório criado para um
	// espelho que falhou não pode ser apagado (rollback).
	GitHubAPIRecommended = []scopeNeed{{name: "delete_repo", about: "desfazer o repositório criado quando o espelho falha"}}
	// GitHubMirrorScopes: o token de mirror só envia commits, inclusive os que
	// mudam .github/workflows.
	GitHubMirrorScopes = []scopeNeed{
		{name: "repo", about: "enviar commits"},
		{name: "workflow", about: "enviar mudanças em .github/workflows"},
	}
)

// ScopeError: um token classic do GitHub sem os escopos necessários.
type ScopeError struct {
	Token   string // "de acesso" ou "de mirror"
	Missing []string
	Need    []string
}

func (e *ScopeError) Error() string {
	return fmt.Sprintf("o token %s do GitHub não tem %s; gere outro (classic) com os escopos %s",
		e.Token, strings.Join(e.Missing, ", "), strings.Join(e.Need, ", "))
}

func missingScopes(have []string, need []scopeNeed) []string {
	set := map[string]bool{}
	for _, s := range have {
		set[s] = true
	}
	var out []string
	for _, n := range need {
		ok := set[n.name]
		for _, a := range n.also {
			ok = ok || set[a]
		}
		if !ok {
			out = append(out, n.name)
		}
	}
	return out
}

func scopeNames(need []scopeNeed) []string {
	out := make([]string, len(need))
	for i, n := range need {
		out[i] = n.name
	}
	return out
}

// githubScopes devolve os escopos de um token; classic=false para tokens
// fine-grained, que não têm escopos para conferir.
func githubScopes(ctx context.Context, url, token string) (scopes []string, classic bool, err error) {
	return github.New(url, token).Scopes(ctx)
}

// checkGitHubToken confere se um token classic tem os escopos de need.
// Tokens fine-grained passam: não há como conferi-los.
func checkGitHubToken(ctx context.Context, url, token, which string, need []scopeNeed) error {
	scopes, classic, err := githubScopes(ctx, url, token)
	if err != nil {
		return fmt.Errorf("conferindo o token %s: %w", which, err)
	}
	if !classic {
		return nil
	}
	if m := missingScopes(scopes, need); len(m) > 0 {
		return &ScopeError{Token: which, Missing: m, Need: scopeNames(need)}
	}
	return nil
}

// githubWarnings aponta o que é aceito mas atrapalha nos tokens já salvos
// de uma instância GitHub: escopos recomendados faltando e tokens
// fine-grained, que não listam organizações nem alcançam sempre os
// repositórios criados depois.
func githubWarnings(ctx context.Context, url, token, mirrorToken string) (warnings []string, err error) {
	scopes, classic, err := githubScopes(ctx, url, token)
	if err != nil {
		return nil, err
	}
	switch {
	case !classic:
		warnings = append(warnings, "O token de acesso é fine-grained: as organizações não aparecem como destino. "+
			"Recomendado: um token classic com repo, read:org e delete_repo.")
	case len(missingScopes(scopes, GitHubAPIScopes)) > 0:
		err := &ScopeError{Token: "de acesso", Missing: missingScopes(scopes, GitHubAPIScopes), Need: scopeNames(GitHubAPIScopes)}
		return nil, err
	case len(missingScopes(scopes, GitHubAPIRecommended)) > 0:
		warnings = append(warnings, "O token de acesso não tem delete_repo: se um espelho falhar, o repositório "+
			"criado no GitHub não é apagado.")
	}
	if mirrorToken == "" {
		return warnings, nil
	}
	ms, mclassic, merr := githubScopes(ctx, url, mirrorToken)
	switch {
	case merr != nil:
		warnings = append(warnings, "Não deu para conferir o token de mirror: "+merr.Error())
	case !mclassic:
		warnings = append(warnings, "O token de mirror é fine-grained: sem acesso a All repositories, os "+
			"repositórios criados depois não recebem os envios. Recomendado: um token classic com repo e workflow.")
	case len(missingScopes(ms, GitHubMirrorScopes)) > 0:
		warnings = append(warnings, "O token de mirror não tem "+strings.Join(missingScopes(ms, GitHubMirrorScopes), ", ")+
			": os envios para o GitHub vão falhar. Gere outro com repo e workflow e renove os espelhos.")
	}
	return warnings, nil
}
