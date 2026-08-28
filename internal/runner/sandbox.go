package runner

import (
	"os/exec"
	"runtime"
)

// Sandbox is the OS-level confinement layer. It turns a shell script and a
// fixture directory into the argv that actually gets executed.
//
// Implementations are selected once at startup and reported by `bt doctor`,
// so a learner can always find out how much confinement they are getting.
type Sandbox interface {
	// Name is the short identifier shown in `bt doctor`.
	Name() string
	// Describe is a one-line summary of what this backend confines.
	Describe() string
	// Argv builds the command line that runs script with root as the working
	// directory.
	Argv(root, script string) []string
	// ProcessLimit is the RLIMIT_NPROC value to apply, or 0 for none.
	//
	// The limit is per real user id, not per process tree, so setting it
	// outside a fresh user namespace would count every process the learner
	// already has running and make the sandbox unable to fork at all. Only a
	// backend that isolates the user id may ask for one.
	ProcessLimit() int
	// Confines reports whether this backend provides OS-level containment.
	// When false, the app shows the "no OS sandbox" banner.
	Confines() bool
}

// DetectSandbox picks the strongest backend available on this machine.
func DetectSandbox() Sandbox {
	switch runtime.GOOS {
	case "linux":
		if p, err := exec.LookPath("bwrap"); err == nil {
			return &bwrapSandbox{path: p}
		}
		if p, err := exec.LookPath("unshare"); err == nil {
			return &unshareSandbox{path: p}
		}
	case "darwin":
		if p, err := exec.LookPath("sandbox-exec"); err == nil {
			return &seatbeltSandbox{path: p}
		}
	}
	return bareSandbox{}
}

// AvailableSandboxes reports every backend and whether it could be used here,
// for `bt doctor`.
func AvailableSandboxes() []SandboxStatus {
	var out []SandboxStatus
	add := func(name, why string, ok bool) {
		out = append(out, SandboxStatus{Name: name, Available: ok, Note: why})
	}
	switch runtime.GOOS {
	case "linux":
		_, bwrapErr := exec.LookPath("bwrap")
		add("bwrap", "install bubblewrap for the strongest confinement", bwrapErr == nil)
		_, unshareErr := exec.LookPath("unshare")
		add("unshare", "util-linux unshare, used when bubblewrap is missing", unshareErr == nil)
	case "darwin":
		_, sbErr := exec.LookPath("sandbox-exec")
		add("sandbox-exec", "ships with macOS", sbErr == nil)
	}
	add("bare", "always available; static allowlist and rlimits only", true)
	return out
}

// SandboxStatus is one line of the backend report in `bt doctor`.
type SandboxStatus struct {
	Name      string
	Available bool
	Note      string
}

// BareSandbox returns the unconfined backend. It exists so that tests — and
// only tests — can force the weakest configuration and assert that the static
// layers still hold on their own.
func BareSandbox() Sandbox { return bareSandbox{} }

// bareSandbox is the fallback: no OS confinement at all. The static allowlist,
// the fixture copy, and the rlimits still apply, but a command that slips past
// the allowlist runs with the learner's own permissions. The app says so.
type bareSandbox struct{}

func (bareSandbox) Name() string { return "bare" }
func (bareSandbox) Describe() string {
	return "no OS sandbox: allowlist, fixture copy and rlimits only"
}
func (bareSandbox) Argv(_, script string) []string { return []string{shellPath, "-c", script} }
func (bareSandbox) ProcessLimit() int              { return 0 }
func (bareSandbox) Confines() bool                 { return false }

// shellPath is the shell every backend runs. /bin/sh is guaranteed present on
// both supported platforms and is the shell the exercises are written against.
const shellPath = "/bin/sh"
