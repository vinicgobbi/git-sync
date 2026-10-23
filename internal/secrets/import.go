package secrets

import (
	"bytes"
	"context"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// TeaLogin é uma entrada de ~/.config/tea/config.yml.
type TeaLogin struct {
	Name    string `yaml:"name"`
	URL     string `yaml:"url"`
	Token   string `yaml:"token"`
	User    string `yaml:"user"`
	Default bool   `yaml:"default"`
	// "oauth": o token não fica no config.yml, e sim no armazenamento
	// criptografado do tea; obtenha-o com TeaHelperToken.
	AuthMethod string `yaml:"auth_method"`
}

func (l TeaLogin) OAuth() bool { return l.AuthMethod == "oauth" }

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

// TeaHelperToken pede ao tea o token da instância em rawURL usando o
// protocolo de credential helper do git. Funciona com login OAuth (o tea
// renova o token se preciso) e com token comum.
func TeaHelperToken(ctx context.Context, rawURL string) (user, token string, err error) {
	u, err := url.Parse(rawURL)
	if err != nil || u.Host == "" {
		return "", "", fmt.Errorf("URL inválida: %q", rawURL)
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "tea", "login", "helper", "get")
	cmd.Stdin = strings.NewReader("protocol=" + u.Scheme + "\nhost=" + u.Host + "\n\n")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return "", "", fmt.Errorf("tea não forneceu credencial para %s: %s", u.Host, msg)
	}
	for _, line := range strings.Split(string(out), "\n") {
		k, v, _ := strings.Cut(strings.TrimSpace(line), "=")
		switch k {
		case "username":
			user = v
		case "password":
			token = v
		}
	}
	if token == "" {
		return "", "", fmt.Errorf("tea não tem login para %s (rode `tea login add`)", u.Host)
	}
	return user, token, nil
}

// GhToken obtém o token do gh CLI para o host (ex.: "github.com").
func GhToken(ctx context.Context, host string) (string, error) {
	out, err := exec.CommandContext(ctx, "gh", "auth", "token", "--hostname", host).Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}
