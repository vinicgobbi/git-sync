// Package deps verifica as ferramentas externas (git, gh, tea) e seus logins.
package deps

import (
	"context"
	"fmt"
	"os/exec"
	"regexp"
	"strings"

	"github.com/vinicgobbi/git-sync/internal/secrets"
)

type Check struct {
	Name     string
	Required bool
	Found    bool
	Path     string
	Version  string
	Detail   string // logins encontrados, avisos, etc.
}

func (c Check) OK() bool { return c.Found || !c.Required }

func Run(ctx context.Context) []Check {
	return []Check{
		tool(ctx, "git", true, "--version"),
		ghCheck(ctx),
		teaCheck(ctx),
	}
}

func tool(ctx context.Context, name string, required bool, versionArgs ...string) Check {
	c := Check{Name: name, Required: required}
	path, err := exec.LookPath(name)
	if err != nil {
		c.Detail = "não encontrado no PATH"
		return c
	}
	c.Found, c.Path = true, path
	if out, err := exec.CommandContext(ctx, path, versionArgs...).Output(); err == nil {
		c.Version = firstLine(string(out))
	}
	return c
}

func ghCheck(ctx context.Context) Check {
	c := tool(ctx, "gh", false, "--version")
	if !c.Found {
		c.Detail = "opcional: usado para importar o token do GitHub"
		return c
	}
	if _, err := secrets.GhToken(ctx, "github.com"); err != nil {
		c.Detail = "sem login (rode `gh auth login`)"
	} else {
		c.Detail = "logado em github.com"
	}
	return c
}

func teaCheck(ctx context.Context) Check {
	c := tool(ctx, "tea", false, "--version")
	if !c.Found {
		c.Detail = "opcional: usado para importar logins Gitea/Forgejo"
		return c
	}
	logins, err := secrets.TeaLogins()
	switch {
	case err != nil:
		c.Detail = fmt.Sprintf("erro lendo config: %v", err)
	case len(logins) == 0:
		c.Detail = "nenhum login (rode `tea login add`)"
	default:
		names := make([]string, len(logins))
		for i, l := range logins {
			names[i] = l.Name + " (" + l.URL + ")"
		}
		c.Detail = "logins: " + strings.Join(names, ", ")
	}
	return c
}

var ansi = regexp.MustCompile(`\x1b\[[0-9;]*m`)

func firstLine(s string) string {
	s, _, _ = strings.Cut(strings.TrimSpace(ansi.ReplaceAllString(s, "")), "\n")
	return strings.Join(strings.Fields(s), " ")
}
