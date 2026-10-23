package mirror

import (
	"context"
	"errors"
	"fmt"

	"github.com/vinicgobbi/git-sync/internal/forge"
)

type Action int

const (
	ActionCreate   Action = iota // criar repo no destino + push mirror
	ActionReuse                  // repo destino já existe (vazio, ou Force) → só push mirror
	ActionSkip                   // push mirror para esse destino já existe
	ActionConflict               // repo destino existe e não está vazio
	ActionInvalid                // não dá para executar (erro de API, sem permissão…)
)

func (a Action) String() string {
	return [...]string{"criar", "reaproveitar", "pular", "conflito", "inválido"}[a]
}

type Step struct {
	Repo        forge.Repo
	Target      int // índice em Request.Targets
	TargetOwner string
	TargetName  string
	TargetURL   string
	Action      Action
	Reason      string
}

func (s Step) Runnable() bool { return s.Action == ActionCreate || s.Action == ActionReuse }

type Plan struct{ Steps []Step }

func (p *Plan) Count(a Action) int {
	n := 0
	for _, s := range p.Steps {
		if s.Action == a {
			n++
		}
	}
	return n
}

// BuildPlan consulta origem e destinos (somente leitura) e decide o que fazer
// para cada par (repo, destino). Rodar duas vezes seguidas após Execute deve
// resultar só em ActionSkip.
func BuildPlan(ctx context.Context, req *Request) (*Plan, error) {
	if len(req.Targets) == 0 {
		return nil, errors.New("nenhum destino selecionado")
	}
	if ok, err := req.SourceForge.MirrorsEnabled(ctx); err != nil {
		return nil, fmt.Errorf("origem %s: %w", req.Source.Name, err)
	} else if !ok {
		return nil, fmt.Errorf("origem %s: espelhamento está desabilitado na instância", req.Source.Name)
	}

	owners := make([]string, len(req.Targets))
	for i, t := range req.Targets {
		owners[i] = t.Owner
		if owners[i] == "" {
			u, err := t.Forge.Whoami(ctx)
			if err != nil {
				return nil, fmt.Errorf("destino %s: %w", t.Instance.Name, err)
			}
			owners[i] = u
		}
	}

	steps := make([]Step, len(req.Repos)*len(req.Targets))
	forEach(ctx, len(req.Repos), req.concurrency(), func(ri int) {
		repo := req.Repos[ri]
		existing, listErr := req.SourceForge.ListPushMirrors(ctx, repo.Owner, repo.Name)
		for ti := range req.Targets {
			s := &steps[ri*len(req.Targets)+ti]
			*s = Step{Repo: repo, Target: ti, TargetOwner: owners[ti], TargetName: repo.Name}
			s.TargetURL = req.Targets[ti].Instance.RepoCloneURL(s.TargetOwner, s.TargetName)
			planStep(ctx, req, s, existing, listErr)
		}
	})
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return &Plan{Steps: steps}, nil
}

func planStep(ctx context.Context, req *Request, s *Step, existing []forge.PushMirror, listErr error) {
	t := req.Targets[s.Target]
	switch {
	case !s.Repo.Admin:
		s.Action, s.Reason = ActionInvalid, "sem permissão de admin no repo de origem"
		return
	case listErr != nil:
		s.Action, s.Reason = ActionInvalid, "erro listando mirrors: "+listErr.Error()
		return
	case t.Instance.Name == req.Source.Name && s.TargetOwner == s.Repo.Owner:
		s.Action, s.Reason = ActionInvalid, "destino é o próprio repo de origem"
		return
	}

	want := forge.NormalizeURL(s.TargetURL)
	for _, m := range existing {
		if forge.NormalizeURL(m.RemoteAddress) == want {
			s.Action, s.Reason = ActionSkip, "mirror já configurado ("+m.RemoteName+")"
			return
		}
	}

	dst, err := t.Forge.GetRepo(ctx, s.TargetOwner, s.TargetName)
	switch {
	case errors.Is(err, forge.ErrNotFound):
		s.Action = ActionCreate
	case err != nil:
		s.Action, s.Reason = ActionInvalid, "erro consultando destino: "+err.Error()
	case dst.Empty:
		s.Action, s.Reason = ActionReuse, "repo destino já existe (vazio)"
		if dst.CloneURL != "" {
			s.TargetURL = dst.CloneURL
		}
	case req.Force:
		s.Action, s.Reason = ActionReuse, "repo destino NÃO vazio será sobrescrito (force)"
		if dst.CloneURL != "" {
			s.TargetURL = dst.CloneURL
		}
	default:
		s.Action, s.Reason = ActionConflict, "repo destino já existe e tem conteúdo"
	}
}
