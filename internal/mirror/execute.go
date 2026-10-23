package mirror

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/vinicgobbi/git-sync/internal/forge"
)

type Status int

const (
	StatusRunning Status = iota
	StatusDone
	StatusFailed
	StatusSkipped
)

// Event informa o progresso de um passo do plano (Index = posição em Plan.Steps).
type Event struct {
	Index  int
	Status Status
	Msg    string
	Err    error
}

type ExecOptions struct {
	// Rollback apaga o repo destino criado neste passo se o mirror falhar.
	Rollback bool
}

type Summary struct{ Done, Failed, Skipped int }

// Execute aplica o plano. emit é chamado de forma serializada (nunca em paralelo).
func Execute(ctx context.Context, req *Request, plan *Plan, opts ExecOptions, emit func(Event)) Summary {
	var mu sync.Mutex
	var sum Summary
	send := func(e Event) {
		mu.Lock()
		defer mu.Unlock()
		switch e.Status {
		case StatusDone:
			sum.Done++
		case StatusFailed:
			sum.Failed++
		case StatusSkipped:
			sum.Skipped++
		}
		if emit != nil {
			emit(e)
		}
	}

	forEach(ctx, len(plan.Steps), req.concurrency(), func(i int) {
		s := plan.Steps[i]
		if !s.Runnable() {
			send(Event{Index: i, Status: StatusSkipped, Msg: s.Reason})
			return
		}
		if err := runStep(ctx, req, s, opts, func(msg string) {
			send(Event{Index: i, Status: StatusRunning, Msg: msg})
		}); err != nil {
			send(Event{Index: i, Status: StatusFailed, Err: err, Msg: err.Error()})
			return
		}
		send(Event{Index: i, Status: StatusDone, Msg: "mirror configurado"})
	})
	return sum
}

func runStep(ctx context.Context, req *Request, s Step, opts ExecOptions, progress func(string)) (err error) {
	t := req.Targets[s.Target]
	targetURL := s.TargetURL

	if s.Action == ActionCreate {
		progress("criando repo em " + t.Instance.Name)
		created, cerr := t.Forge.CreateRepo(ctx, forge.CreateRepoOpts{
			Owner:       s.TargetOwner,
			Name:        s.TargetName,
			Description: mirrorDescription(s.Repo),
			Private:     req.private(s.Repo),
		})
		if cerr != nil {
			return fmt.Errorf("criando repo destino: %w", cerr)
		}
		if created.CloneURL != "" {
			targetURL = created.CloneURL
		}
		if opts.Rollback {
			defer func() {
				if err == nil {
					return
				}
				// contexto próprio: o rollback deve rodar mesmo se ctx foi cancelado
				if derr := t.Forge.DeleteRepo(context.WithoutCancel(ctx), s.TargetOwner, s.TargetName); derr != nil {
					err = errors.Join(err, fmt.Errorf("rollback falhou: %w", derr))
				}
			}()
		}
	}

	progress("configurando push mirror")
	if _, err := req.SourceForge.AddPushMirror(ctx, s.Repo.Owner, s.Repo.Name, forge.AddMirrorOpts{
		RemoteAddress: targetURL,
		Username:      t.Username,
		Password:      t.Password,
		Interval:      req.Interval,
		SyncOnCommit:  req.SyncOnCommit,
	}); err != nil {
		return fmt.Errorf("adicionando push mirror: %w", err)
	}

	// Sync imediato é best-effort: o mirror já existe e sincronizará no intervalo.
	progress("disparando primeira sincronização")
	_ = req.SourceForge.SyncPushMirrors(ctx, s.Repo.Owner, s.Repo.Name)
	return nil
}

func mirrorDescription(src forge.Repo) string {
	if src.HTMLURL == "" {
		return src.Description
	}
	if src.Description == "" {
		return "Mirror of " + src.HTMLURL
	}
	return src.Description + " (mirror of " + src.HTMLURL + ")"
}
