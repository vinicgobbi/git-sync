package mirror

import (
	"context"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"sync"

	"github.com/vinicgobbi/git-sync/internal/config"
	"github.com/vinicgobbi/git-sync/internal/forge"
)

type Source struct {
	Instance config.Instance
	Forge    forge.Mirrorer
}

// Link é um push mirror existente: Source/Repo → Mirror.RemoteAddress.
type Link struct {
	Source config.Instance
	Repo   forge.Repo
	Mirror forge.PushMirror
	Target *config.Instance // instância conhecida que hospeda o destino (nil se externa)
}

type DiscoverResult struct {
	Links  []Link
	Errors []error
}

// Discover varre todos os repos (onde o usuário é admin) de cada origem e
// coleta os push mirrors configurados, inclusive os criados fora do git-sync.
func Discover(ctx context.Context, sources []Source, known []config.Instance, concurrency int) DiscoverResult {
	if concurrency <= 0 {
		concurrency = 4
	}
	var (
		mu  sync.Mutex
		res DiscoverResult
	)
	fail := func(err error) {
		mu.Lock()
		res.Errors = append(res.Errors, err)
		mu.Unlock()
	}

	type job struct {
		src  Source
		repo forge.Repo
	}
	var jobs []job
	for _, src := range sources {
		repos, err := src.Forge.ListRepos(ctx)
		if err != nil {
			fail(fmt.Errorf("%s: listando repos: %w", src.Instance.Name, err))
			continue
		}
		for _, r := range repos {
			if r.Admin {
				jobs = append(jobs, job{src, r})
			}
		}
	}

	forEach(ctx, len(jobs), concurrency, func(i int) {
		j := jobs[i]
		mirrors, err := j.src.Forge.ListPushMirrors(ctx, j.repo.Owner, j.repo.Name)
		if err != nil {
			fail(fmt.Errorf("%s/%s: %w", j.src.Instance.Name, j.repo.FullName(), err))
			return
		}
		mu.Lock()
		defer mu.Unlock()
		for _, m := range mirrors {
			res.Links = append(res.Links, Link{
				Source: j.src.Instance, Repo: j.repo, Mirror: m,
				Target: MatchInstance(m.RemoteAddress, known),
			})
		}
	})

	sort.Slice(res.Links, func(a, b int) bool {
		la, lb := res.Links[a], res.Links[b]
		if la.Source.Name != lb.Source.Name {
			return la.Source.Name < lb.Source.Name
		}
		if la.Repo.FullName() != lb.Repo.FullName() {
			return la.Repo.FullName() < lb.Repo.FullName()
		}
		return la.Mirror.RemoteAddress < lb.Mirror.RemoteAddress
	})
	return res
}

// MatchInstance encontra a instância conhecida cujo host bate com o remote.
func MatchInstance(remote string, known []config.Instance) *config.Instance {
	u, err := url.Parse(remote)
	if err != nil {
		return nil
	}
	for i := range known {
		if strings.EqualFold(known[i].Host(), u.Host) {
			return &known[i]
		}
	}
	return nil
}
