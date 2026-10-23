package update

import (
	"context"
	"os"
	"path/filepath"
	"runtime"

	"github.com/vinicgobbi/git-sync/internal/install"
)

// Apply baixa a release r do sistema atual e a põe no lugar do git-sync do
// PATH (ou deste binário, se não há um no PATH). Devolve o arquivo
// substituído; a versão nova vale a partir da próxima execução.
func (c Client) Apply(ctx context.Context, r Release, in *install.Installer) (string, error) {
	target := in.UpdateTarget()
	tmp, err := c.Download(ctx, r, AssetName(runtime.GOOS, runtime.GOARCH), filepath.Dir(target))
	if err != nil {
		return "", err
	}
	if err := in.Replace(tmp, target); err != nil {
		os.Remove(tmp)
		return "", err
	}
	return target, nil
}
