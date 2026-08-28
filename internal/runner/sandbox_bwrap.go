package runner

import (
	"os"
	"strings"
)

// bwrapSandbox runs the script under bubblewrap: every namespace unshared, the
// system directories bound read-only, a private /tmp, and the fixture as the
// only writable path. --die-with-parent guarantees teardown even if bt is
// killed mid-run.
type bwrapSandbox struct{ path string }

func (bwrapSandbox) Name() string { return "bwrap" }
func (bwrapSandbox) Describe() string {
	return "bubblewrap: all namespaces unshared, read-only system, fixture is the only writable path"
}
func (bwrapSandbox) ProcessLimit() int { return 64 }
func (bwrapSandbox) Confines() bool    { return true }

func (b *bwrapSandbox) Argv(root, script string) []string {
	argv := []string{
		b.path,
		"--die-with-parent",
		"--unshare-all",
		"--new-session",
		"--proc", "/proc",
		"--dev", "/dev",
		"--tmpfs", "/tmp",
	}
	// Bind only the system directories that exist; distributions differ on
	// which of /bin, /sbin and /lib are real and which are symlinks into /usr.
	for _, dir := range []string{"/usr", "/bin", "/sbin", "/lib", "/lib64", "/etc/alternatives"} {
		if fi, err := os.Lstat(dir); err == nil && fi.Mode()&os.ModeSymlink == 0 {
			argv = append(argv, "--ro-bind", dir, dir)
		}
	}
	argv = append(argv,
		"--bind", root, root,
		"--chdir", root,
		"--setenv", "PATH", sandboxPath,
		"--setenv", "HOME", root,
		"--setenv", "LANG", "C",
		"--setenv", "TZ", "UTC",
		"--", shellPath, "-c", script,
	)
	return argv
}

// unshareSandbox is the fallback when bubblewrap is not installed: new user,
// network and mount namespaces, but no filesystem rebinding, so the real
// filesystem is still visible read-only-by-permission. Weaker than bwrap, and
// `bt doctor` says so.
type unshareSandbox struct{ path string }

func (unshareSandbox) Name() string { return "unshare" }
func (unshareSandbox) Describe() string {
	return "unshare -Urnm: no network and a private mount namespace, but the filesystem is still visible"
}

// ProcessLimit is zero even though unshare creates a user namespace: without
// bubblewrap's uid mapping the process still runs under the learner's own uid
// for RLIMIT_NPROC accounting.
func (unshareSandbox) ProcessLimit() int { return 0 }
func (unshareSandbox) Confines() bool    { return true }

func (u *unshareSandbox) Argv(root, script string) []string {
	return []string{u.path, "-Urnm", "--", shellPath, "-c", "cd " + shellQuote(root) + " && " + script}
}

// shellQuote wraps s in single quotes so it survives a shell round trip. It is
// used only for paths this program created, never for learner input.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
