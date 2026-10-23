package app

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/vinicgobbi/git-sync/internal/config"
	"github.com/vinicgobbi/git-sync/internal/secrets"
)

// fakeGitHub responde GET /api/v3/user com os escopos de cada token, como
// o GitHub: tokens classic têm X-OAuth-Scopes; fine-grained, não.
func fakeGitHub(t *testing.T, scopes map[string]string) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tok := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		if r.URL.Path != "/api/v3/user" {
			http.NotFound(w, r)
			return
		}
		if s, ok := scopes[tok]; ok {
			w.Header().Set("X-OAuth-Scopes", s)
		} else if !strings.HasPrefix(tok, "github_pat_") {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		fmt.Fprint(w, `{"login":"vini"}`)
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}

func TestGitHubScopes(t *testing.T) {
	ctx := context.Background()
	url := fakeGitHub(t, map[string]string{
		"api-ok":      "repo, read:org, delete_repo",
		"api-admin":   "repo, admin:org",
		"api-sem-org": "repo",
		"mirror-ok":   "repo, workflow",
		"mirror-sem":  "repo",
	})
	gh := func(name string) config.Instance {
		return config.Instance{Name: name, Kind: config.KindGitHub, URL: url}
	}
	e := newEnv(t)

	var se *ScopeError
	if _, err := e.AddInstance(ctx, gh("a"), "api-sem-org", "mirror-ok"); !errors.As(err, &se) || se.Token != "de acesso" || se.Missing[0] != "read:org" {
		t.Fatalf("token de acesso sem read:org: %v", err)
	}
	if _, err := e.AddInstance(ctx, gh("b"), "api-ok", "mirror-sem"); !errors.As(err, &se) || se.Token != "de mirror" || se.Missing[0] != "workflow" {
		t.Fatalf("token de mirror sem workflow: %v", err)
	}
	if _, err := e.Store.Get("a"); !errors.Is(err, secrets.ErrNotFound) {
		t.Fatal("um token recusado não pode ser salvo")
	}

	// admin:org cobre read:org; sem delete_repo é aceito, com aviso
	in, err := e.AddInstance(ctx, gh("c"), "api-admin", "mirror-ok")
	if err != nil {
		t.Fatal(err)
	}
	st := e.CheckInstance(ctx, in)
	if st.Err != nil || len(st.Warnings) != 1 || !strings.Contains(st.Warnings[0], "delete_repo") {
		t.Fatalf("CheckInstance = %+v", st)
	}

	// tudo certo: sem avisos
	in, err = e.AddInstance(ctx, gh("d"), "api-ok", "mirror-ok")
	if err != nil {
		t.Fatal(err)
	}
	if st := e.CheckInstance(ctx, in); st.Err != nil || len(st.Warnings) != 0 {
		t.Fatalf("CheckInstance = %+v", st)
	}

	// fine-grained: aceito (não há escopos para conferir), com aviso
	in, err = e.AddInstance(ctx, gh("f"), "github_pat_api", "github_pat_mirror")
	if err != nil {
		t.Fatal(err)
	}
	if st := e.CheckInstance(ctx, in); st.Err != nil || len(st.Warnings) != 2 || !strings.Contains(st.Warnings[0], "organizações") {
		t.Fatalf("fine-grained: %+v", st)
	}
}
