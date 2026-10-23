package tui

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/vinicgobbi/git-sync/internal/app"
	"github.com/vinicgobbi/git-sync/internal/config"
	"github.com/vinicgobbi/git-sync/internal/install"
	"github.com/vinicgobbi/git-sync/internal/secrets"
	"github.com/vinicgobbi/git-sync/internal/update"
)

// TestUpdateFlow: o cabeçalho avisa da versão nova e "Verificar
// atualizações" mostra o que muda e substitui o git-sync do PATH.
func TestUpdateFlow(t *testing.T) {
	const newBin = "BIN v1.3.0"
	asset := update.AssetName(runtime.GOOS, runtime.GOARCH)
	sum := sha256.Sum256([]byte(newBin))
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/latest":
			fmt.Fprintf(w, `{"tag_name":"v1.3.0","assets":[{"name":%q,"browser_download_url":"%s/bin"},{"name":"checksums.txt","browser_download_url":"%s/sums"}]}`,
				asset, srv.URL, srv.URL)
		case "/bin":
			fmt.Fprint(w, newBin)
		case "/sums":
			fmt.Fprintf(w, "%s  %s\n", hex.EncodeToString(sum[:]), asset)
		case "/changelog/v1.3.0":
			fmt.Fprint(w, "## v1.3.0 (2026-10-23)\n\n### Feat\n\n- **tui**: atualizar pela interface\n\n## v1.0.0 (2026-10-01)\n\n### Feat\n\n- antiga\n")
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	bin := t.TempDir()
	target := filepath.Join(bin, "git-sync")
	if err := os.WriteFile(target, []byte("BIN v1.0.0"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("PATH", bin)
	env := &app.Env{CfgPath: filepath.Join(t.TempDir(), "c.yaml"), Cfg: config.New(), Store: secrets.Memory{}}
	d := newDriver(t, env)
	d.key("esc", "esc")

	// simula uma build de release, com o GitHub falso
	d.m.sh.version = "1.0.0"
	d.m.sh.updates = update.Client{URL: srv.URL + "/latest", ChangelogURL: srv.URL + "/changelog/%s"}
	d.m.sh.installer = &install.Installer{GOOS: runtime.GOOS, Home: t.TempDir(), Self: "/nao/existe", PathEnv: bin, Getenv: os.Getenv}
	d.run(checkUpdate(d.m.sh))
	d.mustSee("v1.3.0 disponível")

	d.menu("Verificar atualizações")
	d.mustSee("Esta versão", "v1.0.0", "Última release", "Novidades", "tui: atualizar pela interface", "substitui "+target)
	d.key("enter")
	if b, _ := os.ReadFile(target); string(b) != newBin {
		t.Fatalf("o git-sync do PATH deveria ter sido substituído: %q\n%s", b, d.view())
	}
	d.mustSee("Atualizado para v1.3.0")
	if d.m.sh.updateAvailable() != "" {
		t.Fatal("depois de atualizar, o cabeçalho não deveria anunciar a versão")
	}
	d.key("enter")
	if len(d.m.flows) != 0 {
		t.Fatalf("enter deveria fechar o fluxo:\n%s", d.view())
	}
}
