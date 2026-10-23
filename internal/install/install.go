// Package install copia o binário do git-sync para uma pasta de programas
// do usuário (sem admin) e garante que ela está no PATH.
package install

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// Installer guarda tudo o que depende do sistema em campos, para poder ser
// testado em qualquer plataforma.
type Installer struct {
	GOOS    string
	Home    string
	Self    string // caminho do binário em execução
	PathEnv string // $PATH
	Shell   string // $SHELL (unix)
	Getenv  func(string) string
}

func New() (*Installer, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	self, err := os.Executable()
	if err != nil {
		return nil, err
	}
	return &Installer{
		GOOS: runtime.GOOS, Home: home, Self: cleanReal(self),
		PathEnv: os.Getenv("PATH"), Shell: os.Getenv("SHELL"), Getenv: os.Getenv,
	}, nil
}

func (in *Installer) BinName() string {
	if in.GOOS == "windows" {
		return "git-sync.exe"
	}
	return "git-sync"
}

// DefaultDir é uma pasta de programas do usuário, que não exige admin.
func (in *Installer) DefaultDir() string {
	if in.GOOS == "windows" {
		base := in.Getenv("LOCALAPPDATA")
		if base == "" {
			base = filepath.Join(in.Home, "AppData", "Local")
		}
		return filepath.Join(base, "Programs", "git-sync")
	}
	return filepath.Join(in.Home, ".local", "bin")
}

func (in *Installer) OnPath(dir string) bool {
	want := filepath.Clean(dir)
	for _, d := range in.pathDirs() {
		if in.GOOS == "windows" {
			if strings.EqualFold(d, want) {
				return true
			}
		} else if d == want {
			return true
		}
	}
	return false
}

func (in *Installer) pathDirs() []string {
	var dirs []string
	for _, d := range filepath.SplitList(in.PathEnv) {
		if d = strings.TrimSpace(d); d != "" {
			dirs = append(dirs, filepath.Clean(strings.Replace(d, "~", in.Home, 1)))
		}
	}
	return dirs
}

// Status é a situação da instalação vista pelo PATH.
type Status struct {
	// Installed: o git-sync que o PATH encontra é este binário, ou uma
	// cópia idêntica dele. Não há o que instalar.
	Installed bool
	// Existing é outro git-sync no PATH (outra versão); "" se não há.
	Existing string
	// Dir é onde instalar: a pasta do git-sync que já está no PATH, para
	// substituí-lo, ou a pasta padrão do usuário.
	Dir string
}

func (in *Installer) Status() Status {
	found := in.lookPath()
	switch {
	case found == "":
		return Status{Dir: in.DefaultDir()}
	case cleanReal(found) == cleanReal(in.Self) || sameContent(found, in.Self):
		return Status{Installed: true, Dir: filepath.Dir(found)}
	}
	return Status{Existing: found, Dir: filepath.Dir(found)}
}

// lookPath é o exec.LookPath sobre PathEnv: o primeiro git-sync do PATH.
func (in *Installer) lookPath() string {
	for _, d := range in.pathDirs() {
		p := filepath.Join(d, in.BinName())
		fi, err := os.Stat(p)
		if err != nil || !fi.Mode().IsRegular() {
			continue
		}
		if in.GOOS != "windows" && fi.Mode().Perm()&0o111 == 0 {
			continue
		}
		return p
	}
	return ""
}

func sameContent(a, b string) bool {
	fa, errA := os.Stat(a)
	fb, errB := os.Stat(b)
	if errA != nil || errB != nil || fa.Size() != fb.Size() {
		return false
	}
	ha, errA := fileHash(a)
	hb, errB := fileHash(b)
	return errA == nil && errB == nil && bytes.Equal(ha, hb)
}

func fileHash(p string) ([]byte, error) {
	f, err := os.Open(p)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return nil, err
	}
	return h.Sum(nil), nil
}

// Copy instala Self como dir/<BinName>. O arquivo novo é gravado ao lado do
// destino e renomeado por cima dele; no Windows um git-sync.exe em execução
// não pode ser sobrescrito, então ele é movido para o lado antes.
func (in *Installer) Copy(dir string) (dst string, already bool, err error) {
	dst = filepath.Join(dir, in.BinName())
	if cleanReal(in.Self) == cleanReal(dst) {
		return dst, true, nil
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", false, err
	}
	tmp := dst + ".new"
	if err := copyFile(in.Self, tmp); err != nil {
		_ = os.Remove(tmp)
		return "", false, err
	}
	if err := in.Replace(tmp, dst); err != nil {
		_ = os.Remove(tmp)
		return "", false, err
	}
	return dst, false, nil
}

// Replace move src (na mesma pasta de dst) por cima de dst. No Windows um
// .exe em execução não pode ser sobrescrito, então dst vai antes para o
// lado, como dst.old.
func (in *Installer) Replace(src, dst string) error {
	if in.GOOS == "windows" {
		old := dst + ".old"
		_ = os.Remove(old)
		if _, err := os.Stat(dst); err == nil {
			if err := os.Rename(dst, old); err != nil {
				return err
			}
		}
	}
	if err := os.Rename(src, dst); err != nil {
		return err
	}
	if in.GOOS == "darwin" {
		clearQuarantine(dst)
	}
	return nil
}

// UpdateTarget é o binário que uma atualização substitui: o git-sync que o
// PATH encontra (o arquivo de verdade, se for um link) ou, sem nenhum no
// PATH, este mesmo.
func (in *Installer) UpdateTarget() string {
	if p := in.lookPath(); p != "" {
		return cleanReal(p)
	}
	return in.Self
}

func cleanReal(p string) string {
	if r, err := filepath.EvalSymlinks(p); err == nil {
		p = r
	}
	return filepath.Clean(p)
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	return os.Chmod(dst, 0o755)
}

// PathChange descreve como dir entra no PATH, para mostrar antes de fazer.
func (in *Installer) PathChange(dir string) string {
	if in.GOOS == "windows" {
		return dir + " será adicionada ao PATH do usuário."
	}
	file, line := in.ShellRC(dir)
	return "Esta linha será adicionada ao " + file + ":\n    " + line
}

// AddToPath coloca dir no PATH do usuário: no registro, no Windows, e no
// arquivo de inicialização do shell nos demais. Devolve o que o usuário
// precisa fazer para valer.
func (in *Installer) AddToPath(dir string) (string, error) {
	if in.GOOS == "windows" {
		if _, err := addToUserPath(dir); err != nil {
			return "", fmt.Errorf("adicionando ao PATH: %w", err)
		}
		return "Abra um terminal novo para o PATH valer.", nil
	}
	file, line := in.ShellRC(dir)
	if _, err := AddToShellRC(file, line); err != nil {
		return "", err
	}
	return "Abra um terminal novo (ou rode `source " + file + "`) para o PATH valer.", nil
}

// ShellRC devolve o arquivo de inicialização do shell do usuário e a linha
// que põe dir no PATH (só unix).
func (in *Installer) ShellRC(dir string) (file, line string) {
	shown := dir
	if rel, err := filepath.Rel(in.Home, dir); err == nil && !strings.HasPrefix(rel, "..") {
		shown = "$HOME/" + filepath.ToSlash(rel)
	}
	switch filepath.Base(in.Shell) {
	case "fish":
		return filepath.Join(in.Home, ".config", "fish", "config.fish"), "fish_add_path " + strings.Replace(shown, "$HOME", "~", 1)
	case "bash":
		rc := ".bashrc"
		if in.GOOS == "darwin" {
			rc = ".bash_profile"
		}
		return filepath.Join(in.Home, rc), `export PATH="` + shown + `:$PATH"`
	default: // zsh é o padrão no macOS e comum no Linux
		return filepath.Join(in.Home, ".zshrc"), `export PATH="` + shown + `:$PATH"`
	}
}

// AddToShellRC acrescenta line ao arquivo, a menos que já esteja lá.
func AddToShellRC(file, line string) (bool, error) {
	b, err := os.ReadFile(file)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return false, err
	}
	if strings.Contains(string(b), line) {
		return false, nil
	}
	if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		return false, err
	}
	f, err := os.OpenFile(file, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return false, err
	}
	defer f.Close()
	prefix := "\n"
	if len(b) == 0 || strings.HasSuffix(string(b), "\n") {
		prefix = ""
	}
	_, err = fmt.Fprintf(f, "%s\n# git-sync\n%s\n", prefix, line)
	return err == nil, err
}
