//go:build !windows

package install

import (
	"errors"
	"os/exec"
)

// addToUserPath só é usado no Windows; no unix o PATH vem do ShellRC.
func addToUserPath(string) (bool, error) {
	return false, errors.New("PATH do usuário via registro só existe no Windows")
}

// clearQuarantine tira a marca de quarentena que os navegadores do macOS
// põem em downloads, que bloquearia a primeira execução. Melhor esforço.
func clearQuarantine(path string) {
	_ = exec.Command("xattr", "-d", "com.apple.quarantine", path).Run()
}
