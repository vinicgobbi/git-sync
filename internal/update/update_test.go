package update

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/vinicgobbi/git-sync/internal/install"
)

// fakeGitHub publica uma release com o binário deste sistema, o
// checksums.txt e o CHANGELOG.md.
func fakeGitHub(t *testing.T, tag, bin, sum string) Client {
	t.Helper()
	asset := AssetName(runtime.GOOS, runtime.GOARCH)
	if sum == "" {
		h := sha256.Sum256([]byte(bin))
		sum = hex.EncodeToString(h[:])
	}
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/latest":
			fmt.Fprintf(w, `{"tag_name":%q,"assets":[{"name":%q,"browser_download_url":"%s/bin"},{"name":"checksums.txt","browser_download_url":"%s/sums"}]}`,
				tag, asset, srv.URL, srv.URL)
		case "/bin":
			fmt.Fprint(w, bin)
		case "/sums":
			fmt.Fprintf(w, "%s  outro_arquivo\n%s  %s\n", strings.Repeat("0", 64), sum, asset)
		case "/changelog/" + tag:
			fmt.Fprint(w, "## v1.3.0 (2026-10-23)\n\n### Feat\n\n- **tui**: atualizar pela interface\n\n"+
				"## v1.2.0 (2026-10-22)\n\n### Fix\n\n- corrige o filtro\n  em duas linhas\n\n## v1.1.0 (2026-10-21)\n\n### Feat\n\n- antiga\n")
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return Client{URL: srv.URL + "/latest", ChangelogURL: srv.URL + "/changelog/%s"}
}

func TestVersions(t *testing.T) {
	for _, c := range []struct {
		v  string
		ok bool
	}{{"1.2.3", true}, {"v1.2.3", true}, {"dev", false}, {"1.2.3-0.2026", false}, {"1.2.3+dirty", false}, {"test", false}} {
		if Updatable(c.v) != c.ok {
			t.Errorf("Updatable(%q) != %v", c.v, c.ok)
		}
	}
	if !Newer("1.2.0", "v1.10.0") || Newer("1.2.0", "v1.2.0") || Newer("1.3.0", "v1.2.9") || Newer("dev", "v1.0.0") {
		t.Error("Newer")
	}
	if AssetName("windows", "amd64") != "git-sync_windows_amd64.exe" || AssetName("darwin", "arm64") != "git-sync_darwin_arm64" {
		t.Error("AssetName")
	}
}

func TestApplyReplacesTheBinaryOnPath(t *testing.T) {
	c := fakeGitHub(t, "v1.3.0", "BIN v1.3.0", "")
	ctx := context.Background()
	r, err := c.Latest(ctx)
	if err != nil || r.Version != "v1.3.0" {
		t.Fatalf("Latest = %+v, %v", r, err)
	}

	bin := t.TempDir()
	target := filepath.Join(bin, "git-sync")
	_ = os.WriteFile(target, []byte("BIN v1.0.0"), 0o755)
	self := filepath.Join(t.TempDir(), "git-sync_baixado")
	_ = os.WriteFile(self, []byte("loose"), 0o755)
	in := &install.Installer{GOOS: "linux", Home: t.TempDir(), Self: self, PathEnv: bin, Getenv: os.Getenv}

	got, err := c.Apply(ctx, r, in)
	if err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(target); got != target || string(b) != "BIN v1.3.0" {
		t.Fatalf("alvo %s = %q", got, b)
	}
	if b, _ := os.ReadFile(self); string(b) != "loose" {
		t.Fatal("a cópia solta não deveria mudar quando há um git-sync no PATH")
	}
	if leftovers, _ := filepath.Glob(filepath.Join(bin, ".*")); len(leftovers) > 0 {
		t.Fatalf("sobras: %v", leftovers)
	}
}

func TestApplyRejectsWrongChecksum(t *testing.T) {
	c := fakeGitHub(t, "v1.3.0", "BIN adulterado", strings.Repeat("a", 64))
	r, err := c.Latest(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	bin := t.TempDir()
	target := filepath.Join(bin, "git-sync")
	_ = os.WriteFile(target, []byte("BIN v1.0.0"), 0o755)
	in := &install.Installer{GOOS: "linux", Home: t.TempDir(), Self: target, PathEnv: bin, Getenv: os.Getenv}
	if _, err := c.Apply(context.Background(), r, in); err == nil || !strings.Contains(err.Error(), "não confere") {
		t.Fatalf("deveria recusar o SHA-256 errado: %v", err)
	}
	if b, _ := os.ReadFile(target); string(b) != "BIN v1.0.0" {
		t.Fatalf("o binário não deveria mudar: %q", b)
	}
	if entries, _ := os.ReadDir(bin); len(entries) != 1 {
		t.Fatalf("o download recusado deveria ser apagado: %v", entries)
	}
}

func TestChanges(t *testing.T) {
	c := fakeGitHub(t, "v1.3.0", "x", "")
	notes, err := c.Changes(context.Background(), "1.1.0", "v1.3.0")
	if err != nil {
		t.Fatal(err)
	}
	got := FormatNotes(notes, 0)
	want := "v1.3.0  (2026-10-23)\n  Novidades\n    • tui: atualizar pela interface\n" +
		"v1.2.0  (2026-10-22)\n  Correções\n    • corrige o filtro em duas linhas"
	if got != want {
		t.Fatalf("notas:\n%s\nquero:\n%s", got, want)
	}
	if cut := FormatNotes(notes, 1); !strings.Contains(cut, "e mais 1 mudança") {
		t.Fatalf("corte:\n%s", cut)
	}
	if dev, _ := c.Changes(context.Background(), "dev", "v1.3.0"); len(dev) != 1 {
		t.Fatalf("build dev deveria ver só a última: %+v", dev)
	}
}
