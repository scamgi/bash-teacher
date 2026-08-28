package runner

import (
	"os/exec"
	"testing"
)

// runProbe executes a sandbox argv directly, bypassing the parser and the
// allowlist, so the confinement layer can be tested on its own.
func runProbe(t *testing.T, argv []string, root string) (string, error) {
	t.Helper()
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Dir = root
	cmd.Env = []string{"PATH=" + sandboxPath, "HOME=" + root, "HOME_REAL=/Users", "LANG=C", "TZ=UTC"}
	out, err := cmd.CombinedOutput()
	return string(out), err
}
