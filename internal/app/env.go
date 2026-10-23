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
// Com Auth tea/gh nenhum token de API é salvo: ele é pedido à ferramenta a cada uso.
// token ou mirrorToken vazios mantêm os tokens já salvos, se houver.
func (e *Env) AddInstance(ctx context.Context, in config.Instance, token, mirrorToken string) (config.Instance, error) {
	in.URL = strings.TrimSuffix(strings.TrimSpace(in.URL), "/")
	if in.Name == "" || in.URL == "" {
		return in, errors.New("nome e URL são obrigatórios")
	}
	if in.Kind == "" {
		in.Kind = GuessKind(ctx, in.URL)
	}
	if in.Auth == config.AuthKeyring {
		if token == "" {
			token, _ = e.Store.Get(in.Name)
		}
		if token == "" {
			return in, errors.New("informe o token de API")
		}
		if err := e.Store.Set(in.Name, token); err != nil {
			return in, fmt.Errorf("salvando no keyring: %w", err)
		}
	} else {
		_ = e.Store.Delete(in.Name) // não deixa token antigo esquecido no keyring
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

// Candidate é um login encontrado no tea ou no gh que pode virar instância.
// A instância criada referencia o login (Auth tea/gh): nenhum token é copiado.
type Candidate struct {
	Name   string
	URL    string
	Kind   config.Kind // vazio = detectar ao importar
	User   string
	From   string // "tea" ou "gh"
	OAuth  bool   // login OAuth do tea (token expira; o tea renova)
	Exists bool   // já existe instância com esse nome
}

// Candidates lista os logins do tea e do gh disponíveis para importação.
func (e *Env) Candidates(ctx context.Context) ([]Candidate, error) {
	var out []Candidate
	logins, err := secrets.TeaLogins()
	if err != nil {
		return nil, fmt.Errorf("lendo config do tea: %w", err)
	}
	for _, l := range logins {
		out = append(out, Candidate{Name: l.Name, URL: strings.TrimSuffix(l.URL, "/"), User: l.User, From: "tea", OAuth: l.OAuth()})
	}
	if tok, err := secrets.GhToken(ctx, "github.com"); err == nil && tok != "" {
		out = append(out, Candidate{Name: "github", URL: "https://github.com", Kind: config.KindGitHub, From: "gh"})
	}
	for i := range out {
		_, out[i].Exists = e.Cfg.Find(out[i].Name)
	}
	return out, nil
}

// ImportCandidate cadastra o candidato (validando o acesso).
func (e *Env) ImportCandidate(ctx context.Context, c Candidate) (config.Instance, error) {
	auth := config.AuthTea
	if c.From == "gh" {
		auth = config.AuthGh
	}
	return e.AddInstance(ctx, config.Instance{Name: c.Name, URL: c.URL, Kind: c.Kind, Auth: auth}, "", "")
}

type ImportItem struct {
	Name    string
	From    string // "tea" ou "gh"
	Skipped bool   // já existia
	Inst    config.Instance
	Err     error
}

// Import importa todos os candidatos (pulando os já existentes, salvo overwrite).
func (e *Env) Import(ctx context.Context, overwrite bool) ([]ImportItem, error) {
	cands, err := e.Candidates(ctx)
	if err != nil {
		return nil, err
	}
	items := make([]ImportItem, len(cands))
	for i, c := range cands {
		items[i] = ImportItem{Name: c.Name, From: c.From, Skipped: c.Exists && !overwrite}
		if !items[i].Skipped {
			items[i].Inst, items[i].Err = e.ImportCandidate(ctx, c)
		}
	}
	return items, nil
}

type InstanceStatus struct {
	User            string
	MirrorToken     bool // tem token dedicado para mirrors
	OAuth           bool // credencial vem de login OAuth do tea (não serve para mirrors)
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
	st.OAuth = in.Auth == config.AuthTea && teaLoginIsOAuth(in)
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

// ErrOAuthMirror: um token OAuth expira e não pode ser gravado num push mirror.
var ErrOAuthMirror = errors.New("o login desta instância é OAuth (o token expira em poucas horas) e não serve como credencial do espelho; cadastre um token de mirror dedicado na instância")

// MirrorPassword resolve a credencial gravada nos push mirrors que apontam para in:
// o token de mirror dedicado ou, na falta dele, o token de API (exceto OAuth).
func (e *Env) MirrorPassword(ctx context.Context, in config.Instance) (string, error) {
	if tok, err := e.Store.Get(secrets.MirrorKey(in.Name)); err == nil {
		return tok, nil
	}
	if in.Auth == config.AuthTea && teaLoginIsOAuth(in) {
		return "", ErrOAuthMirror
	}
	return APIToken(ctx, in, e.Store)
}

func teaLoginIsOAuth(in config.Instance) bool {
	logins, _ := secrets.TeaLogins()
	for _, l := range logins {
		if strings.EqualFold(strings.TrimSuffix(l.URL, "/"), in.URL) || l.Name == in.Name {
			return l.OAuth()
		}
	}
	return false
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
	pass, err := e.MirrorPassword(ctx, in)
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
