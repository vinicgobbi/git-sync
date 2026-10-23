// Package app conecta config + segredos aos clientes de forge.
package app

import (
	"context"
	"fmt"

	"github.com/vinicgobbi/git-sync/internal/config"
	"github.com/vinicgobbi/git-sync/internal/forge"
	"github.com/vinicgobbi/git-sync/internal/forge/gitea"
	"github.com/vinicgobbi/git-sync/internal/forge/github"
	"github.com/vinicgobbi/git-sync/internal/secrets"
)

// APIToken resolve o token de API conforme a forma de autenticação da instância.
// Para tea/gh o token é pedido na hora, então tokens OAuth renovados são respeitados.
func APIToken(ctx context.Context, in config.Instance, store secrets.Store) (string, error) {
	switch in.Auth {
	case config.AuthTea:
		_, tok, err := secrets.TeaHelperToken(ctx, in.URL)
		return tok, err
	case config.AuthGh:
		host := in.Host()
		return secrets.GhToken(ctx, host)
	}
	return secrets.APIToken(store, in.Name)
}

// Connect cria o cliente da instância.
func Connect(in config.Instance, store secrets.Store) (forge.Forge, error) {
	token, err := APIToken(context.Background(), in, store)
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
