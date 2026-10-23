// Package config carrega e salva a configuração do git-sync (instâncias e padrões).
// Tokens NÃO ficam aqui: ficam no keyring (ver pacote secrets).
package config

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

type Kind string

const (
	KindGitea   Kind = "gitea"
	KindForgejo Kind = "forgejo"
	KindGitHub  Kind = "github"
)

func (k Kind) Valid() bool {
	switch k {
	case KindGitea, KindForgejo, KindGitHub:
		return true
	}
	return false
}

// Instance é um servidor git onde o usuário tem conta.
type Instance struct {
	Name string `yaml:"name"`
	Kind Kind   `yaml:"kind"`
	URL  string `yaml:"url"`
	User string `yaml:"user"`
	// LegacyAuth é o "auth: tea|gh" de configurações antigas, quando o token
	// vinha do login dessas ferramentas. Não é mais suportado: a instância
	// fica sem credencial até ganhar um token de acesso (que limpa o campo).
	LegacyAuth string `yaml:"auth,omitempty"`
}

// CanBeSource indica se a instância suporta push mirrors nativos.
func (i Instance) CanBeSource() bool {
	return i.Kind == KindGitea || i.Kind == KindForgejo
}

// Host retorna o host da URL da instância (ex.: "git.exemplo.com").
func (i Instance) Host() string {
	u, err := url.Parse(i.URL)
	if err != nil {
		return ""
	}
	return strings.ToLower(u.Host)
}

// RepoCloneURL monta a URL HTTPS de clone de owner/name nesta instância.
func (i Instance) RepoCloneURL(owner, name string) string {
	return strings.TrimSuffix(i.URL, "/") + "/" + owner + "/" + name + ".git"
}

type Defaults struct {
	Interval     string `yaml:"interval"`
	SyncOnCommit bool   `yaml:"sync_on_commit"`
	Concurrency  int    `yaml:"concurrency"`
}

type Config struct {
	Instances []Instance `yaml:"instances"`
	Defaults  Defaults   `yaml:"defaults"`
}

func New() *Config {
	return &Config{Defaults: Defaults{Interval: "8h0m0s", SyncOnCommit: true, Concurrency: 4}}
}

// DefaultPath retorna ~/.config/git-sync/config.yaml (respeitando XDG_CONFIG_HOME).
func DefaultPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "git-sync", "config.yaml"), nil
}

// Load lê o arquivo; se não existir, retorna uma config vazia com padrões.
func Load(path string) (*Config, error) {
	cfg := New()
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return cfg, nil
	}
	if err != nil {
		return nil, err
	}
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("config %s: %w", path, err)
	}
	if cfg.Defaults.Interval == "" {
		cfg.Defaults.Interval = "8h0m0s"
	}
	if cfg.Defaults.Concurrency <= 0 {
		cfg.Defaults.Concurrency = 4
	}
	return cfg, cfg.Validate()
}

func (c *Config) Save(path string) error {
	if err := c.Validate(); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	data, err := yaml.Marshal(c)
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func (c *Config) Validate() error {
	seen := map[string]bool{}
	for _, in := range c.Instances {
		if in.Name == "" {
			return errors.New("instância sem nome")
		}
		if seen[in.Name] {
			return fmt.Errorf("instância duplicada: %q", in.Name)
		}
		seen[in.Name] = true
		if !in.Kind.Valid() {
			return fmt.Errorf("instância %q: tipo inválido %q (use gitea, forgejo ou github)", in.Name, in.Kind)
		}
		u, err := url.Parse(in.URL)
		if err != nil || u.Scheme == "" || u.Host == "" {
			return fmt.Errorf("instância %q: URL inválida %q", in.Name, in.URL)
		}
	}
	return nil
}

func (c *Config) Find(name string) (Instance, bool) {
	for _, in := range c.Instances {
		if in.Name == name {
			return in, true
		}
	}
	return Instance{}, false
}

// Upsert adiciona ou substitui a instância com o mesmo nome.
func (c *Config) Upsert(in Instance) {
	in.URL = strings.TrimSuffix(in.URL, "/")
	for i := range c.Instances {
		if c.Instances[i].Name == in.Name {
			c.Instances[i] = in
			return
		}
	}
	c.Instances = append(c.Instances, in)
}

func (c *Config) Remove(name string) bool {
	for i, in := range c.Instances {
		if in.Name == name {
			c.Instances = append(c.Instances[:i], c.Instances[i+1:]...)
			return true
		}
	}
	return false
}

// Sources retorna as instâncias que podem ser origem de mirror.
func (c *Config) Sources() []Instance {
	var out []Instance
	for _, in := range c.Instances {
		if in.CanBeSource() {
			out = append(out, in)
		}
	}
	return out
}
