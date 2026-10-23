package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"

	"github.com/vinicgobbi/git-sync/internal/config"
	"github.com/vinicgobbi/git-sync/internal/secrets"
)

const usage = `git-sync — gerencia push mirrors Gitea/Forgejo → GitHub/Gitea/Forgejo

Uso:
  git-sync [--config ARQ] <comando> [flags]

Comandos:
  doctor                       verifica git, gh, tea e logins
  instance list                lista instâncias configuradas
  instance add  [flags]        adiciona/atualiza instância (pede o token)
  instance rm   NOME           remove instância e seus tokens
  instance import              importa logins do tea e do gh
  discover                     lista push mirrors existentes nas origens
  mirror [flags]               cria repos nos destinos e configura push mirrors

Use "git-sync <comando> -h" para ver as flags.
`

type env struct {
	cfgPath string
	cfg     *config.Config
	store   secrets.Store
}

func (e *env) save() error { return e.cfg.Save(e.cfgPath) }

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	if err := run(ctx, os.Args[1:]); err != nil {
		if !errors.Is(err, flag.ErrHelp) {
			fmt.Fprintln(os.Stderr, "erro:", err)
		}
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string) error {
	defPath, err := config.DefaultPath()
	if err != nil {
		return err
	}
	fs := flag.NewFlagSet("git-sync", flag.ContinueOnError)
	fs.Usage = func() { fmt.Fprint(os.Stderr, usage) }
	cfgPath := fs.String("config", defPath, "arquivo de configuração")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() == 0 {
		fs.Usage()
		return flag.ErrHelp
	}

	cfg, err := config.Load(*cfgPath)
	if err != nil {
		return err
	}
	e := &env{cfgPath: *cfgPath, cfg: cfg, store: secrets.Keyring{}}

	cmd, rest := fs.Arg(0), fs.Args()[1:]
	switch cmd {
	case "doctor":
		return cmdDoctor(ctx, e)
	case "instance":
		return cmdInstance(ctx, e, rest)
	case "discover":
		return cmdDiscover(ctx, e, rest)
	case "mirror":
		return cmdMirror(ctx, e, rest)
	}
	fs.Usage()
	return fmt.Errorf("comando desconhecido: %q", cmd)
}
