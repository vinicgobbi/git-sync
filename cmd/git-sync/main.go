package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"runtime/debug"
	"strings"

	"github.com/vinicgobbi/git-sync/internal/app"
	"github.com/vinicgobbi/git-sync/internal/config"
	"github.com/vinicgobbi/git-sync/internal/secrets"
	"github.com/vinicgobbi/git-sync/internal/tui"
)

const usage = `git-sync — gerencia push mirrors Gitea/Forgejo → GitHub/Gitea/Forgejo

Uso:
  git-sync [--config ARQ] [comando] [flags]

Sem comando, abre a interface (TUI).

Comandos:
  doctor                       verifica git, gh, tea e logins
  instance list                lista instâncias configuradas
  instance add  [flags]        adiciona/atualiza instância (pede o token)
  instance rm   NOME           remove instância e seus tokens
  instance import              importa logins do tea e do gh
  discover                     lista push mirrors existentes nas origens
  mirror [flags]               cria repos nos destinos e configura push mirrors
  install [flags]              instala este binário no seu PATH (sem admin)
  version                      mostra a versão

Use "git-sync <comando> -h" para ver as flags.
`

// version é preenchida no build de release (-ldflags "-X main.version=...").
var version = ""

func versionString() string {
	if version != "" {
		return version
	}
	// go install …@vX.Y.Z grava a versão do módulo no binário
	// (builds locais geram pseudo-versões como v1.0.1-0.2026…+dirty: mostra "dev")
	if bi, ok := debug.ReadBuildInfo(); ok {
		v := bi.Main.Version
		if v != "" && v != "(devel)" && !strings.Contains(v, "-0.") && !strings.Contains(v, "+dirty") {
			return strings.TrimPrefix(v, "v")
		}
	}
	return "dev"
}

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
	showVersion := fs.Bool("version", false, "mostra a versão")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *showVersion || fs.Arg(0) == "version" {
		fmt.Println("git-sync", versionString())
		return nil
	}
	if fs.Arg(0) == "install" {
		return cmdInstall(fs.Args()[1:])
	}
	cfg, err := config.Load(*cfgPath)
	if err != nil {
		return err
	}
	e := &app.Env{CfgPath: *cfgPath, Cfg: cfg, Store: secrets.Keyring{}}

	if fs.NArg() == 0 {
		return tui.Run(ctx, e, versionString())
	}
	cmd, rest := fs.Arg(0), fs.Args()[1:]
	switch cmd {
	case "tui":
		return tui.Run(ctx, e, versionString())
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
