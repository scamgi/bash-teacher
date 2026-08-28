package runner

import "strings"

// seatbeltSandbox runs the script under sandbox-exec with a generated profile.
//
// The profile denies everything by default, then allows exactly what a
// coreutils pipeline needs: forking and executing the system binaries, reading
// the system directories those binaries need to start, and reading and writing
// the materialized fixture. Network access is denied explicitly as well as by
// default, because that denial is the one an exercise is most likely to test.
type seatbeltSandbox struct{ path string }

func (seatbeltSandbox) Name() string { return "sandbox-exec" }
func (seatbeltSandbox) Describe() string {
	return "macOS seatbelt: deny by default, no network, writable only inside the fixture"
}

// ProcessLimit is zero: seatbelt confines what a process may touch, not which
// uid it runs as, so RLIMIT_NPROC would still be accounted against the
// learner's own process count.
func (seatbeltSandbox) ProcessLimit() int { return 0 }
func (seatbeltSandbox) Confines() bool    { return true }

func (s *seatbeltSandbox) Argv(root, script string) []string {
	return []string{s.path, "-p", seatbeltProfile(root), shellPath, "-c", script}
}

// seatbeltProfile builds the policy for one run. root must already be resolved
// through symlinks: seatbelt matches on real paths, and on macOS the temp
// directory reached through /var is really under /private/var.
func seatbeltProfile(root string) string {
	// Reading these is what lets dyld start a binary at all; none of them
	// contain anything a learner could not already read outside the sandbox.
	// The root directory itself must be listed: without a readable "/", dyld
	// aborts every process before main, with no diagnostic.
	readable := []string{
		"/usr", "/bin", "/sbin", "/System", "/Library",
		"/private/var/db/dyld", "/private/var/select",
	}
	var b strings.Builder
	b.WriteString("(version 1)\n(deny default)\n(deny network*)\n")
	b.WriteString("(allow process-fork)\n(allow signal)\n(allow sysctl-read)\n(allow mach-lookup)\n")
	b.WriteString("(allow file-read-metadata)\n")
	b.WriteString("(allow process-exec (subpath \"/usr\") (subpath \"/bin\") (subpath \"/sbin\"))\n")
	b.WriteString("(allow file-read* (literal \"/\") (literal \"/dev/null\") (literal \"/dev/zero\") " +
		"(literal \"/dev/urandom\") (literal \"/dev/random\") (literal \"/dev/tty\")")
	for _, p := range readable {
		b.WriteString(" (subpath " + sbQuote(p) + ")")
	}
	b.WriteString(")\n")
	b.WriteString("(allow file-write-data (literal \"/dev/null\") (literal \"/dev/stdout\") (literal \"/dev/stderr\"))\n")
	b.WriteString("(allow file-read* file-write* (subpath " + sbQuote(root) + "))\n")
	return b.String()
}

// sbQuote renders a path as a seatbelt string literal.
func sbQuote(p string) string {
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(p) + `"`
}
