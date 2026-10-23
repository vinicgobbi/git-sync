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
	"github.com/vinicgobbi/git-sync/internal/secrets"
)

func cmdDoctor(ctx context.Context, e *app.Env) error {
	ok := true
	if _, err := e.Store.Get("__git-sync-probe__"); err != nil && !errors.Is(err, secrets.ErrNotFound) {
		fmt.Printf("✘ keyring do sistema indisponível: %v\n", err)
		ok = false
	} else {
		fmt.Println("✔ keyring do sistema disponível")
	}
	fmt.Printf("\nconfig: %s (%d instâncias)\n", e.CfgPath, len(e.Cfg.Instances))
	for _, in := range e.Cfg.Instances {
		st := e.CheckInstance(ctx, in)
		ok = ok && st.Err == nil
		fmt.Printf("  %s: %s\n", in.Name, describeStatus(in, st))
	}
	if !ok {
		return errors.New("há problemas a corrigir")
	}
	return nil
}

func describeStatus(in config.Instance, st app.InstanceStatus) string {
	if st.Err != nil {
		return "✘ " + st.Err.Error()
	}
	msg := "✔ logado como " + st.User
	switch {
	case st.MirrorToken:
		msg += ", token de mirror dedicado"
	case in.Kind == config.KindGitHub:
		msg += ", ✘ sem token de mirror (obrigatório para ser destino)"
	}
	if st.MirrorsDisabled {
		msg += ", ✘ espelhamento DESABILITADO na instância"
	}
	return msg
}

func cmdInstance(ctx context.Context, e *app.Env, args []string) error {
	if len(args) == 0 {
		return errors.New("uso: git-sync instance list|add|rm|renew")
	}
	switch args[0] {
	case "list", "ls":
		w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
		fmt.Fprintln(w, "NOME\tTIPO\tURL\tUSUÁRIO\tTOKEN DE MIRROR\tORIGEM?")
		for _, in := range e.Cfg.Instances {
			mt := "não"
			if _, err := e.Store.Get(secrets.MirrorKey(in.Name)); err == nil {
				mt = "sim"
			}
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%v\n", in.Name, in.Kind, in.URL, in.User, mt, in.CanBeSource())
		}
		return w.Flush()
	case "add":
		return instanceAdd(ctx, e, args[1:])
	case "rm", "remove":
		if len(args) != 2 {
			return errors.New("uso: git-sync instance rm NOME")
		}
		return e.RemoveInstance(args[1])
	case "renew":
		if len(args) != 2 {
			return errors.New("uso: git-sync instance renew NOME")
		}
		return instanceRenew(ctx, e, args[1])
	}
	return fmt.Errorf("subcomando desconhecido: %q", args[0])
}

func instanceAdd(ctx context.Context, e *app.Env, args []string) error {
	fs := flag.NewFlagSet("instance add", flag.ContinueOnError)
	name := fs.String("name", "", "nome da instância (ex.: casa, codeberg, github)")
	kind := fs.String("kind", "", "gitea|forgejo|github (vazio = detectar)")
	rawURL := fs.String("url", "", "URL web da instância (ex.: https://git.exemplo.com)")
	mirrorTok := fs.Bool("mirror-token", false, "também pedir um token dedicado para os push mirrors que apontam para cá\n(sempre pedido no GitHub, onde é obrigatório)")
	fs.Usage = func() {
		fmt.Fprint(os.Stderr, `uso: git-sync instance add --name NOME --url URL [--kind TIPO] [--mirror-token]

Pede o token de acesso (ou lê GIT_SYNC_TOKEN) e, no GitHub ou com
--mirror-token, o token de mirror (ou GIT_SYNC_MIRROR_TOKEN). Rodar de novo
com o mesmo nome atualiza a instância. As permissões de cada token estão no
README e na tela "Adicionar instância" da interface.

`)
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *name == "" || *rawURL == "" {
		fs.Usage()
		return errors.New("--name e --url são obrigatórios")
	}
	in := config.Instance{Name: *name, Kind: config.Kind(*kind), URL: *rawURL}
	if in.Kind == "" {
		in.Kind = app.GuessKind(ctx, in.URL)
	}
	prompt := fmt.Sprintf("token de acesso para %s: ", *name)
	if in.Kind == config.KindGitHub {
		prompt = fmt.Sprintf("token de acesso para %s (classic, com repo e read:org; delete_repo recomendado): ", *name)
	}
	token, err := readSecret(prompt, "GIT_SYNC_TOKEN")
	if err != nil {
		return err
	}
	var mt string
	if *mirrorTok || in.Kind == config.KindGitHub {
		perms := "repository: leitura e escrita"
		if in.Kind == config.KindGitHub {
			perms = "classic, com repo e workflow"
		}
		if mt, err = readSecret("token de mirror ("+perms+"): ", "GIT_SYNC_MIRROR_TOKEN"); err != nil {
			return err
		}
	}
	_, existed := e.Cfg.Find(*name)
	in, err = e.AddInstance(ctx, in, token, mt)
	if err != nil {
		return err
	}
	fmt.Printf("✔ %s (%s) salvo, logado como %s\n", in.Name, in.Kind, in.User)
	if existed {
		fmt.Printf("  Os espelhos que já enviam para %s guardam o token antigo. Para atualizá-los: git-sync instance renew %s\n", in.Name, in.Name)
	}
	return nil
}

func instanceRenew(ctx context.Context, e *app.Env, name string) error {
	res, err := e.RenewMirrors(ctx, name)
	if err != nil {
		return err
	}
	for _, err := range res.Errors {
		fmt.Println("✘", err)
	}
	switch {
	case res.Found == 0:
		fmt.Printf("Nenhum espelho envia para %s.\n", name)
	case res.Renewed == res.Found:
		fmt.Printf("✔ %d espelho(s) para %s atualizados com a credencial atual.\n", res.Renewed, name)
	default:
		fmt.Printf("%d de %d espelho(s) para %s atualizados.\n", res.Renewed, res.Found, name)
	}
	if len(res.Errors) > 0 {
		return errors.New("alguns espelhos não puderam ser atualizados")
	}
	return nil
}

// readSecret lê da variável env, do terminal (sem eco) ou de stdin.
func readSecret(prompt, env string) (string, error) {
	if v := os.Getenv(env); v != "" {
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
