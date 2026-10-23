// Package update procura no GitHub uma release mais nova do git-sync e
// baixa o binário do sistema, conferido pelo SHA-256 do checksums.txt
// publicado com ela. Não substitui nada sozinho: quem chama instala o
// arquivo baixado (ver install.Installer.Replace).
package update

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

// LatestURL é o endereço da API do GitHub para a última release.
const LatestURL = "https://api.github.com/repos/vinicgobbi/git-sync/releases/latest"

// ReleasesURL é a página das releases, para as notas completas.
const ReleasesURL = "https://github.com/vinicgobbi/git-sync/releases"

// ChecksumsAsset é o arquivo de SHA-256 que o goreleaser publica.
const ChecksumsAsset = "checksums.txt"

// Release é uma versão publicada e seus arquivos.
type Release struct {
	Version string            // "v1.2.3"
	Assets  map[string]string // nome do arquivo → URL de download
}

// Client conversa com o GitHub; os campos vazios usam os padrões.
type Client struct {
	HTTP *http.Client
	URL  string // LatestURL quando vazio
	// ChangelogURL é um formato com a tag (%s); quando ele e URL estão
	// vazios, vale ChangelogURL (ver Changes).
	ChangelogURL string
}

func (c Client) http() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return &http.Client{Timeout: 2 * time.Minute}
}

// Latest devolve a última release.
func (c Client) Latest(ctx context.Context) (Release, error) {
	url := c.URL
	if url == "" {
		url = LatestURL
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return Release{}, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := c.http().Do(req)
	if err != nil {
		return Release{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return Release{}, fmt.Errorf("o GitHub respondeu %s ao consultar a última versão", resp.Status)
	}
	var body struct {
		TagName string `json:"tag_name"`
		Assets  []struct {
			Name string `json:"name"`
			URL  string `json:"browser_download_url"`
		} `json:"assets"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(&body); err != nil {
		return Release{}, fmt.Errorf("resposta inesperada do GitHub: %w", err)
	}
	if _, ok := parse(body.TagName); !ok {
		return Release{}, fmt.Errorf("versão inesperada na última release: %q", body.TagName)
	}
	r := Release{Version: body.TagName, Assets: map[string]string{}}
	for _, a := range body.Assets {
		r.Assets[a.Name] = a.URL
	}
	return r, nil
}

// AssetName é o binário da release para goos/goarch, como o goreleaser
// publica (git-sync_linux_amd64, git-sync_windows_amd64.exe...).
func AssetName(goos, goarch string) string {
	name := "git-sync_" + goos + "_" + goarch
	if goos == "windows" {
		name += ".exe"
	}
	return name
}

// Download grava o asset de r num arquivo executável novo em dir e devolve
// o caminho. O conteúdo tem de bater com o SHA-256 do checksums.txt, ou
// nada fica no disco.
func (c Client) Download(ctx context.Context, r Release, asset, dir string) (string, error) {
	url, ok := r.Assets[asset]
	if !ok {
		return "", fmt.Errorf("a release %s não tem %s", r.Version, asset)
	}
	sumsURL, ok := r.Assets[ChecksumsAsset]
	if !ok {
		return "", fmt.Errorf("a release %s não tem %s para conferir o download", r.Version, ChecksumsAsset)
	}
	sums, err := c.get(ctx, sumsURL, 1<<20)
	if err != nil {
		return "", err
	}
	want := checksumOf(sums, asset)
	if len(want) != 64 {
		return "", fmt.Errorf("%s não tem o SHA-256 de %s", ChecksumsAsset, asset)
	}

	f, err := os.CreateTemp(dir, "."+asset+".*.download")
	if err != nil {
		return "", err
	}
	path := f.Name()
	ok = false
	defer func() {
		if !ok {
			f.Close()
			os.Remove(path)
		}
	}()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	resp, err := c.http().Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("download de %s: %s", asset, resp.Status)
	}
	h := sha256.New()
	if _, err := io.Copy(io.MultiWriter(f, h), io.LimitReader(resp.Body, 256<<20)); err != nil {
		return "", err
	}
	if got := hex.EncodeToString(h.Sum(nil)); got != want {
		return "", fmt.Errorf("o SHA-256 de %s não confere com o publicado (%s, esperado %s): download descartado", asset, got[:12], want[:12])
	}
	if err := f.Close(); err != nil {
		return "", err
	}
	if err := os.Chmod(path, 0o755); err != nil {
		return "", err
	}
	ok = true
	return path, nil
}

// checksumOf acha a linha "<sha256>  <arquivo>" de asset no checksums.txt.
func checksumOf(sums []byte, asset string) string {
	sc := bufio.NewScanner(bytes.NewReader(sums))
	for sc.Scan() {
		f := strings.Fields(sc.Text())
		if len(f) == 2 && strings.TrimPrefix(f[1], "*") == asset {
			return strings.ToLower(f[0])
		}
	}
	return ""
}

func (c Client) get(ctx context.Context, url string, limit int64) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.http().Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download de %s: %s", url, resp.Status)
	}
	return io.ReadAll(io.LimitReader(resp.Body, limit))
}

// Tag põe a versão do binário ("1.2.3") no formato das tags ("v1.2.3").
func Tag(version string) string {
	if strings.HasPrefix(version, "v") {
		return version
	}
	return "v" + version
}

// Updatable diz se uma build pode se atualizar: só builds de release
// ("1.2.3"), nunca "dev" nem pseudo-versões ("1.2.3-0.2026...", "+dirty").
func Updatable(version string) bool {
	_, ok := parse(Tag(version))
	return ok && !strings.ContainsAny(version, "-+")
}

// Newer diz se latest é uma release mais nova que current.
func Newer(current, latest string) bool {
	c, ok := compare(Tag(latest), Tag(current))
	return ok && c > 0
}

// compare compara "vX.Y.Z[-qualquer]"; ok=false quando algum não é versão.
func compare(a, b string) (cmp int, ok bool) {
	pa, oka := parse(a)
	pb, okb := parse(b)
	if !oka || !okb {
		return 0, false
	}
	for i := range pa {
		if pa[i] != pb[i] {
			if pa[i] < pb[i] {
				return -1, true
			}
			return 1, true
		}
	}
	return 0, true
}

func parse(v string) ([3]int, bool) {
	var out [3]int
	if !strings.HasPrefix(v, "v") {
		return out, false
	}
	v = v[1:]
	if i := strings.IndexAny(v, "-+"); i >= 0 {
		v = v[:i]
	}
	parts := strings.Split(v, ".")
	if len(parts) != 3 {
		return out, false
	}
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return out, false
		}
		out[i] = n
	}
	return out, true
}
