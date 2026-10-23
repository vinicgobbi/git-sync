// Package secrets guarda tokens no keyring do sistema operacional.
//
// Cada instância tem duas chaves:
//   - "<nome>"        token de API (só quando a instância não usa tea/gh)
//   - "<nome>/mirror" token opcional gravado nos push mirrors que apontam para esta
//     instância (se ausente, usa o token de API)
package secrets

import (
	"errors"

	"github.com/zalando/go-keyring"
)

const service = "git-sync"

var ErrNotFound = errors.New("token não encontrado no keyring")

type Store interface {
	Get(key string) (string, error)
	Set(key, value string) error
	Delete(key string) error
}

func MirrorKey(instance string) string { return instance + "/mirror" }

type Keyring struct{}

func (Keyring) Get(key string) (string, error) {
	v, err := keyring.Get(service, key)
	if errors.Is(err, keyring.ErrNotFound) {
		return "", ErrNotFound
	}
	return v, err
}

func (Keyring) Set(key, value string) error { return keyring.Set(service, key, value) }

func (Keyring) Delete(key string) error {
	err := keyring.Delete(service, key)
	if errors.Is(err, keyring.ErrNotFound) {
		return nil
	}
	return err
}

// APIToken retorna o token de API da instância.
func APIToken(s Store, instance string) (string, error) { return s.Get(instance) }

// Memory é um Store em memória, útil para testes.
type Memory map[string]string

func (m Memory) Get(key string) (string, error) {
	v, ok := m[key]
	if !ok {
		return "", ErrNotFound
	}
	return v, nil
}
func (m Memory) Set(key, value string) error { m[key] = value; return nil }
func (m Memory) Delete(key string) error     { delete(m, key); return nil }
