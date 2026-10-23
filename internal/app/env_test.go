package app

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/vinicgobbi/git-sync/internal/config"
	"github.com/vinicgobbi/git-sync/internal/secrets"
)

// fakeGitea responde só ao GET /api/v1/user, como o Gitea e o Forgejo.
func fakeGitea(t *testing.T) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/user" || r.Header.Get("Authorization") == "" {
			http.NotFound(w, r)
			return
		}
		fmt.Fprint(w, `{"login":"vini"}`)
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}

func newEnv(t *testing.T) *Env {
	return &Env{CfgPath: filepath.Join(t.TempDir(), "c.yaml"), Cfg: config.New(), Store: secrets.Memory{}}
}

func TestAddInstanceRequiresTokens(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	url := fakeGitea(t)

	if _, err := e.AddInstance(ctx, config.Instance{Name: "casa", Kind: config.KindGitea, URL: url}, "", ""); err == nil {
		t.Fatal("sem token de acesso deveria falhar")
	}
	in, err := e.AddInstance(ctx, config.Instance{Name: "casa", Kind: config.KindGitea, URL: url}, "tok", "")
	if err != nil || in.User != "vini" {
		t.Fatalf("AddInstance = %+v, %v", in, err)
	}
	// Gitea/Forgejo sem token de mirror: os espelhos usam o token de API
	if p, err := e.MirrorPassword(in); err != nil || p != "tok" {
		t.Fatalf("MirrorPassword = %q, %v", p, err)
	}
	// editar sem informar o token mantém o salvo
	if _, err := e.AddInstance(ctx, config.Instance{Name: "casa", Kind: config.KindGitea, URL: url}, "", "mtok"); err != nil {
		t.Fatal(err)
	}
	if p, _ := e.MirrorPassword(in); p != "mtok" {
		t.Fatalf("com token de mirror, os espelhos devem usá-lo: %q", p)
	}

	// GitHub: o token de mirror é obrigatório, e nada é salvo sem ele
	gh := config.Instance{Name: "github", Kind: config.KindGitHub, URL: "https://github.com"}
	if _, err := e.AddInstance(ctx, gh, "ghtok", ""); !errors.Is(err, ErrNoMirrorToken) {
		t.Fatalf("GitHub sem token de mirror: err = %v", err)
	}
	if _, err := e.Store.Get("github"); err == nil {
		t.Fatal("nada deveria ter sido salvo no keyring")
	}
	if _, err := e.MirrorPassword(gh); !errors.Is(err, ErrNoMirrorToken) {
		t.Fatalf("MirrorPassword do GitHub sem token de mirror: %v", err)
	}
}

// TestLegacyAuth: instâncias de configurações antigas (auth: tea|gh) ficam sem
// credencial até ganharem um token de acesso.
func TestLegacyAuth(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	url := fakeGitea(t)
	old := config.Instance{Name: "casa", Kind: config.KindGitea, URL: url, LegacyAuth: "tea"}
	e.Cfg.Upsert(old)
	if _, err := Connect(old, e.Store); !errors.Is(err, ErrLegacyAuth) {
		t.Fatalf("Connect = %v, want ErrLegacyAuth", err)
	}
	if st := e.CheckInstance(ctx, old); !errors.Is(st.Err, ErrLegacyAuth) {
		t.Fatalf("CheckInstance = %+v", st)
	}
	in, err := e.AddInstance(ctx, old, "tok", "")
	if err != nil || in.LegacyAuth != "" {
		t.Fatalf("com token, a instância deveria deixar de ser legada: %+v, %v", in, err)
	}
	if saved, _ := e.Cfg.Find("casa"); saved.LegacyAuth != "" {
		t.Fatal("a config salva ainda tem auth legado")
	}
}
