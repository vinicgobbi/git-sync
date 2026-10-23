package tui

import (
	"errors"
	"strings"

	"github.com/vinicgobbi/git-sync/internal/deps"
	"github.com/vinicgobbi/git-sync/internal/secrets"
)

var depPurpose = map[string]string{
	"git": "Obrigatório.",
	"gh":  "Opcional: permite usar o login do GitHub CLI, sem copiar tokens.",
	"tea": "Opcional: permite usar os logins do Gitea/Forgejo do tea, inclusive OAuth.",
}

func runDiagnostics(sh *shared) *diagReport {
	r := &diagReport{}
	for _, c := range deps.Run(sh.ctx) {
		r.checks = append(r.checks, depCheck{
			name: c.Name, version: c.Version, detail: c.Detail, purpose: depPurpose[c.Name],
			found: c.Found, required: c.Required,
		})
	}
	// uma leitura de chave inexistente distingue "keyring funcionando" de "sem keyring"
	if _, err := sh.env.Store.Get("__git-sync-probe__"); err != nil && !errors.Is(err, secrets.ErrNotFound) {
		r.keyringErr = errors.New(strings.TrimSpace(err.Error()))
	}
	return r
}
