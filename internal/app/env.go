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

// ErrNoMirrorToken: um destino GitHub precisa de token de mirror dedicado.
var ErrNoMirrorToken = errors.New("destinos GitHub precisam de um token de mirror dedicado (Contents e Workflows: read and write); edite a instância e informe-o")

// AddInstance grava os tokens, valida o login (preenchendo User) e salva a config.
// Ao editar, token ou mirrorToken vazios mantêm os já salvos. No GitHub o
// token de mirror é obrigatório: os espelhos não podem usar o token de API.
func (e *Env) AddInstance(ctx context.Context, in config.Instance, token, mirrorToken string) (config.Instance, error) {
	in.URL = strings.TrimSuffix(strings.TrimSpace(in.URL), "/")
	if in.Name == "" || in.URL == "" {
		return in, errors.New("nome e URL são obrigatórios")
	}
	if in.Kind == "" {
		in.Kind = GuessKind(ctx, in.URL)
	}
	if token == "" && in.LegacyAuth == "" {
		token, _ = e.Store.Get(in.Name)
	}
	if token == "" {
		return in, errors.New("informe o token de acesso")
	}
	if in.Kind == config.KindGitHub && mirrorToken == "" {
		if _, err := e.Store.Get(secrets.MirrorKey(in.Name)); err != nil {
			return in, ErrNoMirrorToken
		}
	}
	in.LegacyAuth = ""
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
		return in, err
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

// MirrorPassword resolve a credencial gravada nos push mirrors que apontam para in:
// o token de mirror dedicado ou, na falta dele, o token de API (só no
// Gitea/Forgejo; no GitHub o token de mirror é obrigatório).
func (e *Env) MirrorPassword(in config.Instance) (string, error) {
	if tok, err := e.Store.Get(secrets.MirrorKey(in.Name)); err == nil {
		return tok, nil
	}
	if in.Kind == config.KindGitHub {
		return "", ErrNoMirrorToken
	}
	return APIToken(in, e.Store)
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
	pass, err := e.MirrorPassword(in)
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

// RenewResult é o resultado de RenewMirrors.
type RenewResult struct {
	Found   int     // espelhos que enviam para a instância
	Renewed int     // recriados com a credencial atual
	Errors  []error // falhas ao recriar e origens que não puderam ser lidas
}

// RenewMirrors grava a credencial atual de name (ver MirrorPassword) em todos
// os push mirrors que enviam para ela, em todas as origens. Serve para depois
// de trocar o token: os espelhos guardam uma cópia da credencial de quando
// foram criados.
func (e *Env) RenewMirrors(ctx context.Context, name string) (RenewResult, error) {
	var res RenewResult
	t, err := e.BuildTarget(ctx, name, "")
	if err != nil {
		return res, err
	}
	srcs, err := e.Sources()
	if err != nil {
		return res, err
	}
	forges := map[string]forge.Mirrorer{}
	for _, s := range srcs {
		forges[s.Instance.Name] = s.Forge
	}
	found := mirror.Discover(ctx, srcs, e.Cfg.Instances, e.Cfg.Defaults.Concurrency)
	res.Errors = found.Errors
	var links []mirror.Link
	for _, l := range found.Links {
		if l.Target != nil && l.Target.Name == name {
			links = append(links, l)
		}
	}
	res.Found = len(links)
	var errs []error
	res.Renewed, errs = mirror.Renew(ctx, forges, links, t.Username, t.Password, e.Cfg.Defaults.Concurrency)
	res.Errors = append(res.Errors, errs...)
	return res, nil
}
