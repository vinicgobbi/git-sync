package secrets

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// TeaLogin é uma entrada de ~/.config/tea/config.yml.
type TeaLogin struct {
	Name    string `yaml:"name"`
	URL     string `yaml:"url"`
	Token   string `yaml:"token"`
	User    string `yaml:"user"`
	Default bool   `yaml:"default"`
}

// TeaLogins lê os logins configurados no tea. Retorna nil se o arquivo não existir.
func TeaLogins() ([]TeaLogin, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(filepath.Join(dir, "tea", "config.yml"))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var cfg struct {
		Logins []TeaLogin `yaml:"logins"`
	}
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	return cfg.Logins, nil
}

// GhToken obtém o token do gh CLI para o host (ex.: "github.com").
func GhToken(ctx context.Context, host string) (string, error) {
	out, err := exec.CommandContext(ctx, "gh", "auth", "token", "--hostname", host).Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}
