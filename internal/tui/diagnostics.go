package tui

import (
	"errors"
	"strings"

	"github.com/vinicgobbi/git-sync/internal/secrets"
)

func runDiagnostics(sh *shared) *diagReport {
	r := &diagReport{}
	// uma leitura de chave inexistente distingue "keyring funcionando" de "sem keyring"
	if _, err := sh.env.Store.Get("__git-sync-probe__"); err != nil && !errors.Is(err, secrets.ErrNotFound) {
		r.keyringErr = errors.New(strings.TrimSpace(err.Error()))
	}
	return r
}
