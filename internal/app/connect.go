// Package app conecta config + segredos aos clientes de forge.
package app

import (
	"errors"
	"fmt"

	"github.com/vinicgobbi/git-sync/internal/config"
	"github.com/vinicgobbi/git-sync/internal/forge"
	"github.com/vinicgobbi/git-sync/internal/forge/gitea"
	"github.com/vinicgobbi/git-sync/internal/forge/github"
	"github.com/vinicgobbi/git-sync/internal/secrets"
)

// ErrLegacyAuth: a instância vem de uma configuração antiga, que pegava o
// token do login do tea ou do gh.
var ErrLegacyAuth = errors.New("a instância usava o login do tea ou do gh, que não é mais suportado; edite-a e informe um token de acesso")

// APIToken devolve o token de API da instância, guardado no keyring.
func APIToken(in config.Instance, store secrets.Store) (string, error) {
	if in.LegacyAuth != "" {
		return "", ErrLegacyAuth
	}
	return secrets.APIToken(store, in.Name)
}

// Connect cria o cliente da instância.
func Connect(in config.Instance, store secrets.Store) (forge.Forge, error) {
	token, err := APIToken(in, store)
	if err != nil {
		return nil, fmt.Errorf("instância %q: %w", in.Name, err)
	}
	switch in.Kind {
	case config.KindGitea, config.KindForgejo:
		return gitea.New(in.URL, token), nil
	case config.KindGitHub:
		return github.New(in.URL, token), nil
	}
	return nil, fmt.Errorf("instância %q: tipo desconhecido %q", in.Name, in.Kind)
}

// ConnectSource é como Connect, mas exige uma instância que suporte push mirrors.
func ConnectSource(in config.Instance, store secrets.Store) (forge.Mirrorer, error) {
	if !in.CanBeSource() {
		return nil, fmt.Errorf("instância %q (%s) não pode ser origem: só Gitea/Forgejo suportam push mirror", in.Name, in.Kind)
	}
	f, err := Connect(in, store)
	if err != nil {
		return nil, err
	}
	return f.(forge.Mirrorer), nil
}
