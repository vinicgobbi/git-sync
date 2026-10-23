package main

import (
	"flag"
	"fmt"
	"os"

	"golang.org/x/term"

	"github.com/vinicgobbi/git-sync/internal/install"
)

func cmdInstall(args []string) error {
	fs := flag.NewFlagSet("install", flag.ContinueOnError)
	dir := fs.String("dir", "", "pasta de instalação (padrão: a do git-sync já no PATH, ou ~/.local/bin;\nno Windows, %LOCALAPPDATA%\\Programs\\git-sync)")
	addPath := fs.Bool("add-to-path", false, "adiciona a pasta ao PATH sem perguntar")
	noPath := fs.Bool("no-path", false, "não mexe no PATH")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *addPath && *noPath {
		return fmt.Errorf("--add-to-path e --no-path não combinam")
	}
	in, err := install.New()
	if err != nil {
		return err
	}
	if *dir == "" {
		*dir = in.Status().Dir
	}
	dst, already, err := in.Copy(*dir)
	if err != nil {
		return fmt.Errorf("instalando em %s: %w", *dir, err)
	}
	if already {
		fmt.Println("✔ Já é este binário:", dst)
	} else {
		fmt.Printf("✔ git-sync %s instalado em %s\n", versionString(), dst)
	}
	if in.OnPath(*dir) {
		fmt.Println("Pronto: rode `git-sync` em qualquer terminal.")
		return nil
	}
	if *noPath {
		fmt.Printf("– %s não está no PATH (pulado por --no-path).\n", *dir)
		return nil
	}
	if !*addPath {
		change := in.PathChange(*dir)
		if !term.IsTerminal(int(os.Stdin.Fd())) {
			fmt.Printf("– %s não está no PATH. %s\n  (ou rode com --add-to-path)\n", *dir, change)
			return nil
		}
		fmt.Printf("%s não está no PATH. %s\n", *dir, change)
		if !confirm("Adicionar?") {
			fmt.Println("– Sem PATH: chame pelo caminho completo ou adicione a pasta manualmente.")
			return nil
		}
	}
	next, err := in.AddToPath(*dir)
	if err != nil {
		return err
	}
	fmt.Println("✔ " + next)
	return nil
}
