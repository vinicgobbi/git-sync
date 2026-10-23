package main

import (
	"context"
	"flag"
	"fmt"
	"time"

	"github.com/vinicgobbi/git-sync/internal/install"
	"github.com/vinicgobbi/git-sync/internal/update"
)

func cmdUpdate(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("update", flag.ContinueOnError)
	check := fs.Bool("check", false, "só verifica, sem baixar")
	if err := fs.Parse(args); err != nil {
		return err
	}
	version, c := versionString(), update.Client{}
	qctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	r, err := c.Latest(qctx)
	if err != nil {
		return err
	}
	if !update.Updatable(version) {
		fmt.Printf("Esta é uma build de desenvolvimento (%s); a última release é %s: %s\n", version, r.Version, update.ReleasesURL)
		return nil
	}
	if !update.Newer(version, r.Version) {
		fmt.Printf("✔ Já está na versão mais recente (%s).\n", update.Tag(version))
		return nil
	}
	if notes, err := c.Changes(qctx, version, r.Version); err != nil {
		fmt.Printf("– Não consegui baixar as notas da versão (%v); veja em %s\n\n", err, update.ReleasesURL)
	} else if len(notes) > 0 {
		fmt.Printf("O que muda de %s para %s:\n\n%s\n\n", update.Tag(version), r.Version, update.FormatNotes(notes, 0))
	}
	if *check {
		fmt.Printf("Nova versão: %s (esta é %s). Atualize com: git-sync update\n", r.Version, update.Tag(version))
		return nil
	}
	in, err := install.New()
	if err != nil {
		return err
	}
	dctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	target, err := c.Apply(dctx, r, in)
	if err != nil {
		return err
	}
	fmt.Printf("✔ git-sync atualizado: %s → %s (%s).\n", update.Tag(version), r.Version, target)
	return nil
}
