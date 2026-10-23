package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"

	"golang.org/x/term"

	"github.com/vinicgobbi/git-sync/internal/app"
	"github.com/vinicgobbi/git-sync/internal/config"
	"github.com/vinicgobbi/git-sync/internal/deps"
)

func cmdDoctor(ctx context.Context, e *app.Env) error {
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
	fmt.Printf("\nconfig: %s (%d instâncias)\n", e.CfgPath, len(e.Cfg.Instances))
	for _, in := range e.Cfg.Instances {
		fmt.Printf("  %s: %s\n", in.Name, describeStatus(e.CheckInstance(ctx, in)))
	}
	if !ok {
		return errors.New("dependências obrigatórias ausentes")
	}
	return nil
}

func describeStatus(st app.InstanceStatus) string {
	if st.Err != nil {
		return "✘ " + st.Err.Error()
	}
	msg := "✔ logado como " + st.User
	if st.MirrorToken {
		msg += ", token de mirror dedicado"
	}
	if st.MirrorsDisabled {
		msg += ", ✘ espelhamento DESABILITADO na instância"
	}
	return msg
}

func cmdInstance(ctx context.Context, e *app.Env, args []string) error {
	if len(args) == 0 {
		return errors.New("uso: git-sync instance list|add|rm|import")
	}
	switch args[0] {
	case "list", "ls":
		w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
		fmt.Fprintln(w, "NOME\tTIPO\tURL\tUSUÁRIO\tCREDENCIAL\tORIGEM?")
		for _, in := range e.Cfg.Instances {
			auth := "keyring"
			if in.Auth != config.AuthKeyring {
				auth = "via " + string(in.Auth)
			}
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%v\n", in.Name, in.Kind, in.URL, in.User, auth, in.CanBeSource())
		}
		return w.Flush()
	case "add":
		return instanceAdd(ctx, e, args[1:])
	case "rm", "remove":
		if len(args) != 2 {
			return errors.New("uso: git-sync instance rm NOME")
		}
		return e.RemoveInstance(args[1])
	case "import":
		return instanceImport(ctx, e, args[1:])
	}
	return fmt.Errorf("subcomando desconhecido: %q", args[0])
}

func instanceAdd(ctx context.Context, e *app.Env, args []string) error {
	fs := flag.NewFlagSet("instance add", flag.ContinueOnError)
	name := fs.String("name", "", "nome da instância (ex.: casa, codeberg, github)")
	kind := fs.String("kind", "", "gitea|forgejo|github (vazio = detectar)")
	rawURL := fs.String("url", "", "URL web da instância (ex.: https://git.exemplo.com)")
	auth := fs.String("auth", "", "de onde vem o token: vazio = pedir e guardar no keyring; tea = login do tea (aceita OAuth); gh = login do gh")
	mirrorTok := fs.Bool("mirror-token", false, "também pedir um token dedicado para os push mirrors que apontam para cá")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *name == "" || *rawURL == "" {
		fs.Usage()
		return errors.New("--name e --url são obrigatórios")
	}
	in := config.Instance{Name: *name, Kind: config.Kind(*kind), URL: *rawURL, Auth: config.Auth(*auth)}
	if !in.Auth.Valid() {
		return fmt.Errorf("--auth inválido: %q", *auth)
	}
	var token string
	var err error
	if in.Auth == config.AuthKeyring {
		if token, err = readSecret(fmt.Sprintf("token de API para %s: ", *name)); err != nil {
			return err
		}
	}
	var mt string
	if *mirrorTok {
		if mt, err = readSecret("token dedicado para mirrors: "); err != nil {
			return err
		}
	}
	in, err = e.AddInstance(ctx, in, token, mt)
	if err != nil {
		return err
	}
	fmt.Printf("✔ %s (%s) salvo, logado como %s\n", in.Name, in.Kind, in.User)
	return nil
}

func instanceImport(ctx context.Context, e *app.Env, args []string) error {
	fs := flag.NewFlagSet("instance import", flag.ContinueOnError)
	overwrite := fs.Bool("overwrite", false, "sobrescrever instâncias com o mesmo nome")
	if err := fs.Parse(args); err != nil {
		return err
	}
	items, err := e.Import(ctx, *overwrite)
	if err != nil {
		return err
	}
	for _, it := range items {
		switch {
		case it.Skipped:
			fmt.Printf("– %s já existe, pulando (use --overwrite)\n", it.Name)
		case it.Err != nil:
			fmt.Printf("✘ %s (%s): %v\n", it.Name, it.From, it.Err)
		default:
			fmt.Printf("✔ %s (%s, credencial via %s) logado como %s\n", it.Name, it.Inst.Kind, it.From, it.Inst.User)
			if it.Inst.Kind == config.KindGitHub {
				fmt.Println("  dica: para usar como destino, cadastre um fine-grained PAT (Contents + Workflows: write)")
				fmt.Println("  como token de mirror: git-sync instance add --name github --url https://github.com --auth gh --mirror-token")
			}
		}
	}
	return nil
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
