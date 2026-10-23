package tui

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
	"testing"

	"github.com/vinicgobbi/git-sync/internal/app"
	"github.com/vinicgobbi/git-sync/internal/forge"
	"github.com/vinicgobbi/git-sync/internal/secrets"
)

func TestHumanize(t *testing.T) {
	cases := []struct {
		err  error
		want string
	}{
		{&forge.APIError{Status: 401}, "Token inválido"},
		{&forge.APIError{Status: 403, Body: `{"message":"API rate limit exceeded"}`}, "Limite de requisições"},
		{&forge.APIError{Status: 403}, "não tem permissão"},
		{&forge.APIError{Status: 422, Body: `{"message":"name already exists on this account"}`}, "Já existe"},
		{fmt.Errorf("x: %w", secrets.ErrNotFound), "Token não encontrado"},
		{&url.Error{Op: "Get", URL: "https://git.lan/api/v1/user", Err: errors.New("dial tcp: connection refused")}, "conectar a git.lan"},
		{&forge.APIError{Status: 418, Body: `{"message":"sou um bule"}`}, "HTTP 418: sou um bule"},
		{&app.ScopeError{Token: "de mirror", Missing: []string{"workflow"}, Need: []string{"repo", "workflow"}}, "de mirror do GitHub não tem workflow"},
	}
	for _, c := range cases {
		if got := humanize(c.err).msg; !strings.Contains(got, c.want) {
			t.Errorf("humanize(%v) = %q, want contendo %q", c.err, got, c.want)
		}
	}
	// o GitHub diz o que aceitaria: vai para a dica
	h := humanize(&forge.APIError{Status: 403, Accepted: "escopos repo"})
	if !strings.Contains(h.hint, "escopos repo") {
		t.Errorf("dica do 403 com X-Accepted-OAuth-Scopes: %q", h.hint)
	}
}

func TestMirrorHint(t *testing.T) {
	if h := mirrorHint("refusing to allow a Personal Access Token to create or update workflow `.github/workflows/ci.yml` without `workflow` scope"); !strings.Contains(h, "Workflows: write") {
		t.Errorf("dica de workflow: %q", h)
	}
	if h := mirrorHint("fatal: Authentication failed for 'https://github.com/x/y.git/'"); !strings.Contains(h, "renove a credencial") {
		t.Errorf("dica de autenticação: %q", h)
	}
}

func TestWebURL(t *testing.T) {
	if got := webURL("https://user:tok@github.com/a/b.git"); got != "https://github.com/a/b" {
		t.Errorf("webURL = %q", got)
	}
}
