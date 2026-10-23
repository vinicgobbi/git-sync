// Package mirror orquestra a criação e descoberta de push mirrors, sem UI.
package mirror

import (
	"context"
	"sync"

	"github.com/vinicgobbi/git-sync/internal/config"
	"github.com/vinicgobbi/git-sync/internal/forge"
)

type Visibility int

const (
	VisibilityInherit Visibility = iota
	VisibilityPublic
	VisibilityPrivate
)

// Target é um destino de espelhamento.
type Target struct {
	Instance config.Instance
	Forge    forge.Forge
	Owner    string // usuário ou org no destino; vazio = usuário autenticado
	// Credenciais gravadas no push mirror da origem para empurrar ao destino.
	Username string
	Password string
}

// Request descreve o que o usuário quer espelhar.
type Request struct {
	Source       config.Instance
	SourceForge  forge.Mirrorer
	Repos        []forge.Repo
	Targets      []Target
	Interval     string
	SyncOnCommit bool
	Visibility   Visibility
	Force        bool // permite reaproveitar repo destino NÃO vazio (será sobrescrito)
	Concurrency  int
}

func (r *Request) concurrency() int {
	if r.Concurrency <= 0 {
		return 4
	}
	return r.Concurrency
}

func (r *Request) private(src forge.Repo) bool {
	switch r.Visibility {
	case VisibilityPublic:
		return false
	case VisibilityPrivate:
		return true
	}
	return src.Private
}

// forEach executa fn(i) para i em [0,n) com no máximo `limit` goroutines.
func forEach(ctx context.Context, n, limit int, fn func(i int)) {
	sem := make(chan struct{}, limit)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		if ctx.Err() != nil {
			break
		}
		sem <- struct{}{}
		wg.Add(1)
		go func(i int) {
			defer func() { <-sem; wg.Done() }()
			fn(i)
		}(i)
	}
	wg.Wait()
}
