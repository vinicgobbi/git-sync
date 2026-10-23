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
	"time"

	"github.com/vinicgobbi/git-sync/internal/app"
	"github.com/vinicgobbi/git-sync/internal/forge"
	"github.com/vinicgobbi/git-sync/internal/mirror"
)

type multiFlag []string

func (m *multiFlag) String() string     { return strings.Join(*m, ",") }
func (m *multiFlag) Set(v string) error { *m = append(*m, v); return nil }

func cmdDiscover(ctx context.Context, e *app.Env, args []string) error {
	fs := flag.NewFlagSet("discover", flag.ContinueOnError)
	var only multiFlag
	fs.Var(&only, "source", "limitar a esta origem (repetível)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	sources, err := e.Sources(only...)
	if err != nil {
		return err
	}
	if len(sources) == 0 {
		return errors.New("nenhuma origem Gitea/Forgejo configurada")
	}

	res := mirror.Discover(ctx, sources, e.Cfg.Instances, e.Cfg.Defaults.Concurrency)
	w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(w, "ORIGEM\tREPO\tDESTINO\tREMOTE\tÚLTIMO SYNC\tERRO")
	for _, l := range res.Links {
		dst := "(externo)"
		if l.Target != nil {
			dst = l.Target.Name
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\n", l.Source.Name, l.Repo.FullName(), dst,
			l.Mirror.RemoteAddress, fmtTime(l.Mirror.LastUpdate), oneLine(l.Mirror.LastError))
	}
	w.Flush()
	for _, err := range res.Errors {
		fmt.Fprintln(os.Stderr, "aviso:", err)
	}
	return nil
}

func cmdMirror(ctx context.Context, e *app.Env, args []string) error {
	fs := flag.NewFlagSet("mirror", flag.ContinueOnError)
	from := fs.String("from", "", "instância de origem (Gitea/Forgejo)")
	var repos, targets multiFlag
	fs.Var(&repos, "repo", "repo de origem owner/nome (repetível)")
	fs.Var(&targets, "to", "destino INSTÂNCIA[:owner] (repetível)")
	all := fs.Bool("all", false, "todos os repos onde você é admin (exceto forks e pull mirrors)")
	owner := fs.String("owner", "", "com --all: só repos deste usuário/org")
	vis := fs.String("visibility", "inherit", "inherit|public|private")
	interval := fs.String("interval", e.Cfg.Defaults.Interval, "intervalo de sync (ex.: 8h0m0s)")
	onCommit := fs.Bool("sync-on-commit", e.Cfg.Defaults.SyncOnCommit, "sincronizar a cada push na origem")
	force := fs.Bool("force", false, "reaproveitar repo destino NÃO vazio (será sobrescrito)")
	rollback := fs.Bool("rollback", true, "apagar repo criado se o mirror falhar")
	dryRun := fs.Bool("dry-run", false, "só mostrar o plano")
	yes := fs.Bool("yes", false, "não pedir confirmação")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *from == "" || len(targets) == 0 || (len(repos) == 0 && !*all) {
		fs.Usage()
		return errors.New("informe --from, ao menos um --to e --repo ou --all")
	}

	srcIn, ok := e.Cfg.Find(*from)
	if !ok {
		return fmt.Errorf("instância %q não existe", *from)
	}
	src, err := app.ConnectSource(srcIn, e.Store)
	if err != nil {
		return err
	}

	req := &mirror.Request{
		Source: srcIn, SourceForge: src,
		Interval: *interval, SyncOnCommit: *onCommit,
		Force: *force, Concurrency: e.Cfg.Defaults.Concurrency,
	}
	switch *vis {
	case "inherit":
	case "public":
		req.Visibility = mirror.VisibilityPublic
	case "private":
		req.Visibility = mirror.VisibilityPrivate
	default:
		return fmt.Errorf("--visibility inválido: %q", *vis)
	}

	if req.Repos, err = selectRepos(ctx, src, repos, *all, *owner); err != nil {
		return err
	}
	for _, spec := range targets {
		name, owner, _ := strings.Cut(spec, ":")
		t, err := e.BuildTarget(ctx, name, owner)
		if err != nil {
			return err
		}
		req.Targets = append(req.Targets, t)
	}

	fmt.Printf("planejando %d repo(s) × %d destino(s)…\n", len(req.Repos), len(req.Targets))
	plan, err := mirror.BuildPlan(ctx, req)
	if err != nil {
		return err
	}
	printPlan(req, plan)

	runnable := plan.Count(mirror.ActionCreate) + plan.Count(mirror.ActionReuse)
	if *dryRun || runnable == 0 {
		if runnable == 0 {
			fmt.Println("nada a fazer.")
		}
		return nil
	}
	if !*yes && !confirm(fmt.Sprintf("executar %d passo(s)?", runnable)) {
		return errors.New("cancelado")
	}

	sum := mirror.Execute(ctx, req, plan, mirror.ExecOptions{Rollback: *rollback}, func(ev mirror.Event) {
		s := plan.Steps[ev.Index]
		label := s.Repo.FullName() + " → " + req.Targets[s.Target].Instance.Name
		switch ev.Status {
		case mirror.StatusRunning:
			fmt.Printf("  … %s: %s\n", label, ev.Msg)
		case mirror.StatusDone:
			fmt.Printf("  ✔ %s\n", label)
		case mirror.StatusFailed:
			fmt.Printf("  ✘ %s: %s\n", label, ev.Msg)
		}
	})
	fmt.Printf("\nconcluído: %d ok, %d falha(s), %d pulado(s)\n", sum.Done, sum.Failed, sum.Skipped)
	if sum.Failed > 0 {
		return errors.New("houve falhas")
	}
	return nil
}

func selectRepos(ctx context.Context, src forge.Mirrorer, names []string, all bool, owner string) ([]forge.Repo, error) {
	if !all {
		var out []forge.Repo
		for _, n := range names {
			o, r, ok := strings.Cut(n, "/")
			if !ok {
				return nil, fmt.Errorf("--repo deve ser owner/nome: %q", n)
			}
			repo, err := src.GetRepo(ctx, o, r)
			if err != nil {
				return nil, fmt.Errorf("%s: %w", n, err)
			}
			out = append(out, *repo)
		}
		return out, nil
	}
	list, err := src.ListRepos(ctx)
	if err != nil {
		return nil, err
	}
	var out []forge.Repo
	for _, r := range list {
		if r.Admin && !r.Fork && !r.Mirror && (owner == "" || strings.EqualFold(r.Owner, owner)) {
			out = append(out, r)
		}
	}
	return out, nil
}

func printPlan(req *mirror.Request, plan *mirror.Plan) {
	w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(w, "REPO\tDESTINO\tAÇÃO\tURL\tOBS")
	for _, s := range plan.Steps {
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", s.Repo.FullName(), req.Targets[s.Target].Instance.Name,
			s.Action, s.TargetURL, s.Reason)
	}
	w.Flush()
}

func confirm(q string) bool {
	fmt.Print(q + " [s/N] ")
	line, _ := bufio.NewReader(os.Stdin).ReadString('\n')
	switch strings.ToLower(strings.TrimSpace(line)) {
	case "s", "sim", "y", "yes":
		return true
	}
	return false
}

func fmtTime(t time.Time) string {
	if t.IsZero() {
		return "nunca"
	}
	return t.Local().Format("2006-01-02 15:04")
}

func oneLine(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	if len(s) > 60 {
		return s[:60] + "…"
	}
	return s
}
