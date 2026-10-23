package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"net/url"
	"os"
	"strings"
	"text/tabwriter"

	"golang.org/x/term"

	"github.com/vinicgobbi/git-sync/internal/app"
	"github.com/vinicgobbi/git-sync/internal/config"
	"github.com/vinicgobbi/git-sync/internal/deps"
	"github.com/vinicgobbi/git-sync/internal/secrets"
)

func cmdDoctor(ctx context.Context, e *env) error {
	ok := true
	for _, c := range deps.Run(ctx) {
		mark := "✔"
		if !c.Found {
			mark = "–"
			if c.Required {
				mark, ok = "✘", false
			}
		}
		fmt.Printf("%s %-4s %s\n", mark, c.Name, c.Version)
		if c.Detail != "" {
			fmt.Printf("       %s\n", c.Detail)
		}
	}
	fmt.Printf("\nconfig: %s (%d instâncias)\n", e.cfgPath, len(e.cfg.Instances))
	for _, in := range e.cfg.Instances {
		fmt.Printf("  %s: %s\n", in.Name, checkInstance(ctx, e, in))
	}
	if !ok {
		return errors.New("dependências obrigatórias ausentes")
	}
	return nil
}

func checkInstance(ctx context.Context, e *env, in config.Instance) string {
	f, err := app.Connect(in, e.store)
	if err != nil {
		return "✘ " + err.Error()
	}
	user, err := f.Whoami(ctx)
	if err != nil {
		return "✘ " + err.Error()
	}
	msg := "✔ logado como " + user
	if _, err := e.store.Get(secrets.MirrorKey(in.Name)); err == nil {
		msg += ", token de mirror dedicado"
	}
	if in.CanBeSource() {
		src, _ := app.ConnectSource(in, e.store)
		if on, err := src.MirrorsEnabled(ctx); err != nil {
			msg += ", ✘ não foi possível checar mirrors: " + err.Error()
		} else if !on {
			msg += ", ✘ espelhamento DESABILITADO na instância"
		}
	}
	return msg
}

func cmdInstance(ctx context.Context, e *env, args []string) error {
	if len(args) == 0 {
		return errors.New("uso: git-sync instance list|add|rm|import")
	}
	switch args[0] {
	case "list", "ls":
		w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
		fmt.Fprintln(w, "NOME\tTIPO\tURL\tUSUÁRIO\tORIGEM?")
		for _, in := range e.cfg.Instances {
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%v\n", in.Name, in.Kind, in.URL, in.User, in.CanBeSource())
		}
		return w.Flush()
	case "add":
		return instanceAdd(ctx, e, args[1:])
	case "rm", "remove":
		if len(args) != 2 {
			return errors.New("uso: git-sync instance rm NOME")
		}
		if !e.cfg.Remove(args[1]) {
			return fmt.Errorf("instância %q não existe", args[1])
		}
		_ = e.store.Delete(args[1])
		_ = e.store.Delete(secrets.MirrorKey(args[1]))
		return e.save()
	case "import":
		return instanceImport(ctx, e, args[1:])
	}
	return fmt.Errorf("subcomando desconhecido: %q", args[0])
}

func instanceAdd(ctx context.Context, e *env, args []string) error {
	fs := flag.NewFlagSet("instance add", flag.ContinueOnError)
	name := fs.String("name", "", "nome da instância (ex.: casa, codeberg, github)")
	kind := fs.String("kind", "", "gitea|forgejo|github (vazio = detectar)")
	rawURL := fs.String("url", "", "URL web da instância (ex.: https://git.exemplo.com)")
	mirrorTok := fs.Bool("mirror-token", false, "também pedir um token dedicado para os push mirrors que apontam para cá")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *name == "" || *rawURL == "" {
		fs.Usage()
		return errors.New("--name e --url são obrigatórios")
	}
	in := config.Instance{Name: *name, Kind: config.Kind(*kind), URL: strings.TrimSuffix(*rawURL, "/")}
	if in.Kind == "" {
		in.Kind = guessKind(ctx, in.URL)
		fmt.Printf("tipo detectado: %s\n", in.Kind)
	}

	token, err := readSecret(fmt.Sprintf("token de API para %s: ", in.Name))
	if err != nil {
		return err
	}
	if err := e.store.Set(in.Name, token); err != nil {
		return fmt.Errorf("salvando no keyring: %w", err)
	}
	if *mirrorTok {
		mt, err := readSecret("token dedicado para mirrors: ")
		if err != nil {
			return err
		}
		if err := e.store.Set(secrets.MirrorKey(in.Name), mt); err != nil {
			return err
		}
	}
	return finishInstance(ctx, e, in)
}

// finishInstance valida o token (descobrindo o usuário) e salva a config.
func finishInstance(ctx context.Context, e *env, in config.Instance) error {
	f, err := app.Connect(in, e.store)
	if err != nil {
		return err
	}
	user, err := f.Whoami(ctx)
	if err != nil {
		return fmt.Errorf("%s: token inválido? %w", in.Name, err)
	}
	in.User = user
	e.cfg.Upsert(in)
	if err := e.save(); err != nil {
		return err
	}
	fmt.Printf("✔ %s (%s) salvo, logado como %s\n", in.Name, in.Kind, user)
	return nil
}

func instanceImport(ctx context.Context, e *env, args []string) error {
	fs := flag.NewFlagSet("instance import", flag.ContinueOnError)
	overwrite := fs.Bool("overwrite", false, "sobrescrever instâncias com o mesmo nome")
	if err := fs.Parse(args); err != nil {
		return err
	}
	exists := func(name string) bool { _, ok := e.cfg.Find(name); return ok && !*overwrite }

	logins, err := secrets.TeaLogins()
	if err != nil {
		fmt.Fprintln(os.Stderr, "aviso: lendo config do tea:", err)
	}
	for _, l := range logins {
		if exists(l.Name) {
			fmt.Printf("– %s já existe, pulando (use --overwrite)\n", l.Name)
			continue
		}
		in := config.Instance{Name: l.Name, URL: strings.TrimSuffix(l.URL, "/")}
		in.Kind = guessKind(ctx, in.URL)
		if err := e.store.Set(in.Name, l.Token); err != nil {
			return err
		}
		if err := finishInstance(ctx, e, in); err != nil {
			fmt.Fprintln(os.Stderr, "✘", err)
		}
	}

	if !exists("github") {
		if tok, err := secrets.GhToken(ctx, "github.com"); err == nil && tok != "" {
			if err := e.store.Set("github", tok); err != nil {
				return err
			}
			in := config.Instance{Name: "github", Kind: config.KindGitHub, URL: "https://github.com"}
			if err := finishInstance(ctx, e, in); err != nil {
				fmt.Fprintln(os.Stderr, "✘", err)
			} else {
				fmt.Println("  dica: o token do gh é OAuth e ficaria gravado nos mirrors. Prefira um fine-grained PAT")
				fmt.Println("  (Contents + Workflows: write) com: git-sync instance add --name github --url https://github.com --mirror-token")
			}
		}
	}
	return nil
}

func guessKind(ctx context.Context, rawURL string) config.Kind {
	if u, err := url.Parse(rawURL); err == nil && strings.EqualFold(u.Host, "github.com") {
		return config.KindGitHub
	}
	return app.DetectKind(ctx, rawURL)
}

// readSecret lê da variável GIT_SYNC_TOKEN, do terminal (sem eco) ou de stdin.
func readSecret(prompt string) (string, error) {
	if v := os.Getenv("GIT_SYNC_TOKEN"); v != "" {
		return v, nil
	}
	fd := int(os.Stdin.Fd())
	if term.IsTerminal(fd) {
		fmt.Fprint(os.Stderr, prompt)
		b, err := term.ReadPassword(fd)
		fmt.Fprintln(os.Stderr)
		if err != nil {
			return "", err
		}
		return strings.TrimSpace(string(b)), nil
	}
	line, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil && line == "" {
		return "", err
	}
	return strings.TrimSpace(line), nil
}
