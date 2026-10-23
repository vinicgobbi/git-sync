package mirror

import (
	"context"
	"fmt"
	"sync"

	"github.com/vinicgobbi/git-sync/internal/forge"
)

// Renew regrava a credencial (username/password) nos push mirrors de links.
// A API não edita a credencial de um push mirror, então cada um é recriado
// com o mesmo endereço, intervalo e envio a cada push: o novo é criado antes
// de o antigo ser apagado, e se a criação falhar o antigo fica como estava.
// forges dá o cliente de cada origem, pelo nome da instância.
func Renew(ctx context.Context, forges map[string]forge.Mirrorer, links []Link, username, password string, concurrency int) (renewed int, errs []error) {
	if concurrency <= 0 {
		concurrency = 4
	}
	var mu sync.Mutex
	forEach(ctx, len(links), concurrency, func(i int) {
		l := links[i]
		err := renewOne(ctx, forges[l.Source.Name], l, username, password)
		mu.Lock()
		defer mu.Unlock()
		if err != nil {
			errs = append(errs, fmt.Errorf("%s/%s → %s: %w", l.Source.Name, l.Repo.FullName(), l.Mirror.RemoteAddress, err))
			return
		}
		renewed++
	})
	return renewed, errs
}

func renewOne(ctx context.Context, src forge.Mirrorer, l Link, username, password string) error {
	if src == nil {
		return fmt.Errorf("origem %q não está conectada", l.Source.Name)
	}
	if _, err := src.AddPushMirror(ctx, l.Repo.Owner, l.Repo.Name, forge.AddMirrorOpts{
		RemoteAddress: l.Mirror.RemoteAddress,
		Username:      username,
		Password:      password,
		Interval:      l.Mirror.Interval,
		SyncOnCommit:  l.Mirror.SyncOnCommit,
	}); err != nil {
		return fmt.Errorf("recriando o espelho: %w", err)
	}
	if err := src.DeletePushMirror(ctx, l.Repo.Owner, l.Repo.Name, l.Mirror.RemoteName); err != nil {
		return fmt.Errorf("o espelho novo foi criado, mas o antigo (%s) não pôde ser removido: %w", l.Mirror.RemoteName, err)
	}
	// best-effort: já envia com a credencial nova, sem esperar o intervalo
	_ = src.SyncPushMirrors(ctx, l.Repo.Owner, l.Repo.Name)
	return nil
}
