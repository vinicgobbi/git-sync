// Package forge define a interface comum para Gitea, Forgejo e GitHub.
package forge

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

var ErrNotFound = errors.New("não encontrado")

type APIError struct {
	Status int
	Method string
	Path   string
	Body   string
}

func (e *APIError) Error() string {
	body := strings.TrimSpace(e.Body)
	if len(body) > 300 {
		body = body[:300] + "…"
	}
	return fmt.Sprintf("%s %s: HTTP %d: %s", e.Method, e.Path, e.Status, body)
}

type Repo struct {
	Owner       string
	Name        string
	Description string
	CloneURL    string
	HTMLURL     string
	Private     bool
	Empty       bool
	Mirror      bool // pull mirror (repo espelhado de outro lugar)
	Fork        bool
	Admin       bool // usuário autenticado é admin (necessário para gerenciar mirrors)
}

func (r Repo) FullName() string { return r.Owner + "/" + r.Name }

type CreateRepoOpts struct {
	Owner       string // vazio ou igual ao usuário => conta pessoal; senão, organização
	Name        string
	Description string
	Private     bool
}

type PushMirror struct {
	RemoteName    string
	RemoteAddress string
	Interval      string
	SyncOnCommit  bool
	Created       time.Time
	LastUpdate    time.Time
	LastError     string
}

type AddMirrorOpts struct {
	RemoteAddress string
	Username      string
	Password      string
	Interval      string
	SyncOnCommit  bool
}

// Forge é o que qualquer instância (origem ou destino) precisa oferecer.
type Forge interface {
	Whoami(ctx context.Context) (string, error)
	ListRepos(ctx context.Context) ([]Repo, error)
	GetRepo(ctx context.Context, owner, name string) (*Repo, error) // ErrNotFound se não existir
	CreateRepo(ctx context.Context, opts CreateRepoOpts) (*Repo, error)
	DeleteRepo(ctx context.Context, owner, name string) error
}

// Mirrorer é implementado apenas por Gitea/Forgejo (origens).
type Mirrorer interface {
	Forge
	MirrorsEnabled(ctx context.Context) (bool, error)
	ListPushMirrors(ctx context.Context, owner, repo string) ([]PushMirror, error)
	AddPushMirror(ctx context.Context, owner, repo string, opts AddMirrorOpts) (*PushMirror, error)
	SyncPushMirrors(ctx context.Context, owner, repo string) error
	DeletePushMirror(ctx context.Context, owner, repo, remoteName string) error
}

// FlexTime aceita RFC3339, string vazia ou null (versões antigas do Gitea variam).
type FlexTime struct{ time.Time }

func (t *FlexTime) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err != nil || s == "" {
		return nil
	}
	if p, err := time.Parse(time.RFC3339, s); err == nil {
		t.Time = p
	}
	return nil
}

// NormalizeURL reduz uma URL de repo a "host/owner/name" para comparação
// (ignora esquema, credenciais, maiúsculas, ".git" e barra final).
func NormalizeURL(raw string) string {
	s := strings.TrimSpace(raw)
	if i := strings.Index(s, "://"); i >= 0 {
		s = s[i+3:]
	}
	if i := strings.LastIndex(s, "@"); i >= 0 && i < strings.Index(s+"/", "/") {
		s = s[i+1:]
	}
	s = strings.TrimSuffix(s, "/")
	s = strings.TrimSuffix(s, ".git")
	return strings.ToLower(s)
}
