package app

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/vinicgobbi/git-sync/internal/config"
	"github.com/vinicgobbi/git-sync/internal/forge"
	"github.com/vinicgobbi/git-sync/internal/mirror"
	"github.com/vinicgobbi/git-sync/internal/secrets"
)

// Env é o estado compartilhado entre CLI e TUI.
type Env struct {
	CfgPath string
	Cfg     *config.Config
	Store   secrets.Store
}

func (e *Env) Save() error { return e.Cfg.Save(e.CfgPath) }

// GuessKind identifica o tipo pela URL (github.com) ou consultando a API.
func GuessKind(ctx context.Context, rawURL string) config.Kind {
	if u, err := url.Parse(rawURL); err == nil && strings.EqualFold(u.Host, "github.com") {
		return config.KindGitHub
	}
	return DetectKind(ctx, rawURL)
}

// AddInstance grava os tokens, valida o login (preenchendo User) e salva a config.
// token ou mirrorToken vazios mantêm os tokens já salvos, se houver.
func (e *Env) AddInstance(ctx context.Context, in config.Instance, token, mirrorToken string) (config.Instance, error) {
	in.URL = strings.TrimSuffix(strings.TrimSpace(in.URL), "/")
	if token == "" && in.Name != "" {
		token, _ = e.Store.Get(in.Name)
	}
	if in.Name == "" || in.URL == "" || token == "" {
		return in, errors.New("nome, URL e token são obrigatórios")
	}
	if in.Kind == "" {
		in.Kind = GuessKind(ctx, in.URL)
	}
	if err := e.Store.Set(in.Name, token); err != nil {
		return in, fmt.Errorf("salvando no keyring: %w", err)
	}
	if mirrorToken != "" {
		if err := e.Store.Set(secrets.MirrorKey(in.Name), mirrorToken); err != nil {
			return in, fmt.Errorf("salvando no keyring: %w", err)
		}
	}
	f, err := Connect(in, e.Store)
	if err != nil {
		return in, err
	}
	user, err := f.Whoami(ctx)
	if err != nil {
		return in, fmt.Errorf("%s: token inválido? %w", in.Name, err)
	}
	in.User = user
	e.Cfg.Upsert(in)
	return in, e.Save()
}

// RemoveInstance apaga a instância da config e seus tokens do keyring.
func (e *Env) RemoveInstance(name string) error {
	if !e.Cfg.Remove(name) {
		return fmt.Errorf("instância %q não existe", name)
	}
	_ = e.Store.Delete(name)
	_ = e.Store.Delete(secrets.MirrorKey(name))
	return e.Save()
}

type ImportItem struct {
	Name    string
	From    string // "tea" ou "gh"
	Skipped bool   // já existia
	Inst    config.Instance
	Err     error
}

// Import importa logins do tea e o token do gh (como instância "github").
func (e *Env) Import(ctx context.Context, overwrite bool) ([]ImportItem, error) {
	exists := func(name string) bool { _, ok := e.Cfg.Find(name); return ok && !overwrite }
	var items []ImportItem

	logins, err := secrets.TeaLogins()
	if err != nil {
		return nil, fmt.Errorf("lendo config do tea: %w", err)
	}
	for _, l := range logins {
		it := ImportItem{Name: l.Name, From: "tea"}
		if exists(l.Name) {
			it.Skipped = true
		} else {
			it.Inst, it.Err = e.AddInstance(ctx, config.Instance{Name: l.Name, URL: l.URL}, l.Token, "")
		}
		items = append(items, it)
	}

	if tok, err := secrets.GhToken(ctx, "github.com"); err == nil && tok != "" {
		it := ImportItem{Name: "github", From: "gh"}
		if exists("github") {
			it.Skipped = true
		} else {
			in := config.Instance{Name: "github", Kind: config.KindGitHub, URL: "https://github.com"}
			it.Inst, it.Err = e.AddInstance(ctx, in, tok, "")
		}
		items = append(items, it)
	}
	return items, nil
}

type InstanceStatus struct {
	User            string
	MirrorToken     bool // tem token dedicado para mirrors
	MirrorsDisabled bool
	Err             error
}

// CheckInstance testa o token e, para origens, se o espelhamento está habilitado.
func (e *Env) CheckInstance(ctx context.Context, in config.Instance) InstanceStatus {
	var st InstanceStatus
	f, err := Connect(in, e.Store)
	if err != nil {
		st.Err = err
		return st
	}
	if st.User, st.Err = f.Whoami(ctx); st.Err != nil {
		return st
	}
	_, err = e.Store.Get(secrets.MirrorKey(in.Name))
	st.MirrorToken = err == nil
	if src, ok := f.(forge.Mirrorer); ok && in.CanBeSource() {
		on, err := src.MirrorsEnabled(ctx)
		st.Err, st.MirrorsDisabled = err, err == nil && !on
	}
	return st
}

// Sources conecta todas as origens configuradas (ou só as nomeadas em only).
func (e *Env) Sources(only ...string) ([]mirror.Source, error) {
	var out []mirror.Source
	for _, in := range e.Cfg.Sources() {
		if len(only) > 0 && !contains(only, in.Name) {
			continue
		}
		f, err := ConnectSource(in, e.Store)
		if err != nil {
			return nil, err
		}
		out = append(out, mirror.Source{Instance: in, Forge: f})
	}
	return out, nil
}

// BuildTarget monta um destino com as credenciais que serão gravadas no mirror.
func (e *Env) BuildTarget(ctx context.Context, name, owner string) (mirror.Target, error) {
	in, ok := e.Cfg.Find(name)
	if !ok {
		return mirror.Target{}, fmt.Errorf("destino %q não existe", name)
	}
	f, err := Connect(in, e.Store)
	if err != nil {
		return mirror.Target{}, err
	}
	pass, err := secrets.MirrorToken(e.Store, in.Name)
	if err != nil {
		return mirror.Target{}, fmt.Errorf("destino %q: %w", name, err)
	}
	user := in.User
	if user == "" {
		if user, err = f.Whoami(ctx); err != nil {
			return mirror.Target{}, err
		}
	}
	return mirror.Target{Instance: in, Forge: f, Owner: owner, Username: user, Password: pass}, nil
}

func contains(xs []string, v string) bool {
	for _, x := range xs {
		if x == v {
			return true
		}
	}
	return false
}
