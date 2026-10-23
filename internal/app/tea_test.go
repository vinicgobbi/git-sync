package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/vinicgobbi/git-sync/internal/config"
	"github.com/vinicgobbi/git-sync/internal/secrets"
)

// fakeTea instala um executável "tea" falso que responde ao credential helper,
// e um config.yml do tea com um login OAuth.
func fakeTea(t *testing.T) {
	t.Helper()
	bin, cfg := t.TempDir(), t.TempDir()
	script := "#!/bin/sh\ncat >/dev/null\nprintf 'protocol=https\\nhost=git.lan\\nusername=vini\\npassword=oauth-tok\\n'\n"
	if err := os.WriteFile(filepath.Join(bin, "tea"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(cfg, "tea"), 0o700); err != nil {
		t.Fatal(err)
	}
	teaCfg := "logins:\n- name: lan\n  url: https://git.lan\n  user: vini\n  auth_method: oauth\n"
	if err := os.WriteFile(filepath.Join(cfg, "tea", "config.yml"), []byte(teaCfg), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin)
	t.Setenv("XDG_CONFIG_HOME", cfg)
}

func TestTeaOAuth(t *testing.T) {
	fakeTea(t)
	ctx := context.Background()

	user, tok, err := secrets.TeaHelperToken(ctx, "https://git.lan")
	if err != nil || user != "vini" || tok != "oauth-tok" {
		t.Fatalf("TeaHelperToken = %q, %q, %v", user, tok, err)
	}

	env := &Env{Cfg: config.New(), Store: secrets.Memory{}}
	cands, err := env.Candidates(ctx)
	if err != nil || len(cands) != 1 || !cands[0].OAuth || cands[0].From != "tea" {
		t.Fatalf("Candidates = %+v, %v", cands, err)
	}

	in := config.Instance{Name: "lan", Kind: config.KindGitea, URL: "https://git.lan", Auth: config.AuthTea}
	if tok, err := APIToken(ctx, in, env.Store); err != nil || tok != "oauth-tok" {
		t.Fatalf("APIToken via tea = %q, %v", tok, err)
	}
	// OAuth expira: não pode virar credencial de espelho sem token dedicado
	if _, err := env.MirrorPassword(ctx, in); !errors.Is(err, ErrOAuthMirror) {
		t.Fatalf("MirrorPassword sem token dedicado: err = %v, want ErrOAuthMirror", err)
	}
	_ = env.Store.Set(secrets.MirrorKey("lan"), "pat")
	if p, err := env.MirrorPassword(ctx, in); err != nil || p != "pat" {
		t.Fatalf("MirrorPassword com token dedicado = %q, %v", p, err)
	}
	// nenhum token de API foi copiado para o keyring
	if _, err := env.Store.Get("lan"); !errors.Is(err, secrets.ErrNotFound) {
		t.Fatalf("token de API não deveria estar no keyring")
	}
}
