package runner

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"bash-teacher/internal/content"
	"bash-teacher/internal/shellparse"
)

// The resource ceilings applied to every run. They are deliberately far above
// what any exercise needs and far below what a runaway pipeline would take.
const (
	// DefaultTimeout is the wall clock a run gets before its whole process
	// group is killed. Every exercise in the library finishes in milliseconds.
	DefaultTimeout = 3 * time.Second
	// cpuSeconds backstops the wall clock for a process that spins without
	// yielding, in case the kill signal is delayed.
	cpuSeconds = 5
	// fileBlocks caps what a single redirection can write, in 512-byte
	// blocks, so `yes > file` cannot fill the disk.
	fileBlocks = 8192
	// addressKB caps the address space of each process, so a runaway
	// allocation fails instead of swapping the machine to death.
	addressKB = 256 * 1024
	// outputLimit caps captured stdout and stderr each. Beyond it the stream
	// is truncated and the result says so.
	outputLimit = 1 << 20
	// killGrace is how long the runner waits for a killed process group to be
	// reaped before giving up on collecting its output.
	killGrace = 2 * time.Second
	// sandboxPath is the PATH the sandbox runs with: system binaries only,
	// nothing the learner installed.
	sandboxPath = "/usr/bin:/bin:/usr/sbin:/sbin"
)

// Job is one thing to run: some learner input, against one fixture, under any
// constraints the exercise imposes.
type Job struct {
	Input   string
	Fixture string
	MustUse []string
	Forbid  []string
}

// Result is what came of a Job. A result with Violations or a ParseError never
// reached the sandbox at all.
type Result struct {
	Input   string
	Sandbox string

	// ParseError is set when the input is not syntax the runner understands.
	ParseError *shellparse.Error
	// Violations lists every static rule the input broke. Non-empty means
	// nothing was executed.
	Violations []Violation
	// Skipped is set when the runner is in --no-exec mode.
	Skipped bool

	Stdout          string
	Stderr          string
	StdoutTruncated bool
	StderrTruncated bool
	ExitCode        int
	TimedOut        bool
	Duration        time.Duration
}

// Ran reports whether the input actually reached the sandbox.
func (r *Result) Ran() bool {
	return r.ParseError == nil && len(r.Violations) == 0 && !r.Skipped
}

// Refusal returns the learner-facing explanation for input that was refused
// before execution, or "" when the input ran.
func (r *Result) Refusal() string {
	switch {
	case r.ParseError != nil:
		return r.ParseError.Msg
	case len(r.Violations) > 0:
		msgs := make([]string, 0, len(r.Violations))
		for _, v := range r.Violations {
			msgs = append(msgs, v.Message)
		}
		return strings.Join(msgs, "\n")
	case r.Skipped:
		return "execution is disabled (--no-exec)"
	}
	return ""
}

// Outcome is a Result plus the verdict on an exercise.
type Outcome struct {
	*Result
	Exercise *content.Exercise
	Passed   bool
	Diff     *Diff
}

// Runner executes learner input. One is built at startup and shared: detecting
// the sandbox backend costs a PATH lookup, and the answer cannot change while
// the program runs.
type Runner struct {
	lib     *content.Library
	policy  *Policy
	sandbox Sandbox
	timeout time.Duration
	noExec  bool
}

// Option configures a Runner.
type Option func(*Runner)

// WithSandbox forces a particular backend, which is how the adversarial tests
// check that the static allowlist alone still blocks the command-level cases.
func WithSandbox(s Sandbox) Option { return func(r *Runner) { r.sandbox = s } }

// WithTimeout overrides the wall-clock limit.
func WithTimeout(d time.Duration) Option { return func(r *Runner) { r.timeout = d } }

// WithNoExec disables execution entirely, for learners who want the dictionary
// and the flashcards without ever running a subprocess.
func WithNoExec(off bool) Option { return func(r *Runner) { r.noExec = off } }

// New builds a Runner over a content library.
func New(lib *content.Library, opts ...Option) *Runner {
	r := &Runner{
		lib:     lib,
		policy:  NewPolicy(lib),
		sandbox: DetectSandbox(),
		timeout: DefaultTimeout,
	}
	for _, o := range opts {
		o(r)
	}
	return r
}

// Policy exposes the static allowlist, for `bt doctor`.
func (r *Runner) Policy() *Policy { return r.policy }

// Sandbox reports the backend in use.
func (r *Runner) Sandbox() Sandbox { return r.sandbox }

// NoExec reports whether execution is disabled.
func (r *Runner) NoExec() bool { return r.noExec }

// Run parses, statically checks, and — only if both pass — executes a job.
//
// The returned error is reserved for the runner's own failures, such as being
// unable to create the fixture directory. Everything the learner can get wrong
// comes back in the Result.
func (r *Runner) Run(ctx context.Context, job Job) (*Result, error) {
	res := &Result{Input: job.Input, Sandbox: r.sandbox.Name()}

	script, err := shellparse.Parse(job.Input)
	if err != nil {
		var pe *shellparse.Error
		if errors.As(err, &pe) {
			res.ParseError = pe
			return res, nil
		}
		return nil, err
	}

	res.Violations = append(res.Violations, r.policy.Check(script)...)
	res.Violations = append(res.Violations, r.policy.CheckConstraints(script, job.MustUse, job.Forbid)...)
	if len(res.Violations) > 0 {
		return res, nil
	}
	if r.noExec {
		res.Skipped = true
		return res, nil
	}

	root, err := materialize(r.lib.Files(), job.Fixture)
	if err != nil {
		return nil, err
	}
	// Teardown is unconditional: a panic below still removes the directory.
	defer root.Close()

	r.execute(ctx, res, root.Dir, job.Input)
	return res, nil
}

// RunExercise runs input against an exercise and diffs the output.
func (r *Runner) RunExercise(ctx context.Context, ex *content.Exercise, input string) (*Outcome, error) {
	res, err := r.Run(ctx, Job{
		Input:   input,
		Fixture: ex.Fixture,
		MustUse: ex.MustUse,
		Forbid:  ex.Forbid,
	})
	if err != nil {
		return nil, err
	}
	out := &Outcome{Result: res, Exercise: ex}
	if !res.Ran() {
		return out, nil
	}
	expected, err := r.lib.ExpectedOutput(ex)
	if err != nil {
		return nil, err
	}
	out.Diff = Compare(expected, res.Stdout, ex.Match)
	out.Passed = out.Diff.Equal
	return out, nil
}

// execute runs the script under the sandbox and fills in res.
//
// The learner's text is handed to /bin/sh verbatim rather than reassembled
// from the parse tree: reassembly would risk executing something subtly
// different from what was checked. The parser is an approximation of sh, so
// the OS sandbox — not the parse tree — is what bounds any divergence.
func (r *Runner) execute(ctx context.Context, res *Result, root, input string) {
	script := r.prelude() + input
	argv := r.sandbox.Argv(root, script)

	cmd := exec.Command(argv[0], argv[1:]...) //#nosec G204 -- argv is built from the sandbox backend and the input has passed the parser and the static allowlist; confinement is the sandbox's job, not this call's
	cmd.Dir = root
	cmd.Env = []string{
		"PATH=" + sandboxPath,
		"HOME=" + root,
		"LANG=C",
		"LC_ALL=C",
		"TZ=UTC",
	}
	cmd.Stdin = nil // an unset Stdin is /dev/null
	stdout := &limitedBuffer{limit: outputLimit}
	stderr := &limitedBuffer{limit: outputLimit}
	cmd.Stdout, cmd.Stderr = stdout, stderr
	cmd.SysProcAttr = newProcessGroup()

	start := time.Now()
	if err := cmd.Start(); err != nil {
		res.Stderr = "could not start the sandbox: " + err.Error()
		res.ExitCode = -1
		return
	}

	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()

	timer := time.NewTimer(r.timeout)
	defer timer.Stop()

	var waitErr error
	select {
	case waitErr = <-done:
	case <-timer.C:
		res.TimedOut = true
		if err := killGroup(cmd.Process.Pid); err != nil {
			res.Stderr = "could not kill the timed-out process group: " + err.Error() + "\n"
		}
		select {
		case waitErr = <-done:
		case <-time.After(killGrace):
			// The group would not die and still holds the output pipes.
			// Report what is known rather than blocking the UI forever.
		}
	case <-ctx.Done():
		if err := killGroup(cmd.Process.Pid); err != nil {
			res.Stderr = "could not kill the cancelled process group: " + err.Error() + "\n"
		}
		<-done
		waitErr = ctx.Err()
	}

	res.Duration = time.Since(start)
	res.Stdout, res.StdoutTruncated = stdout.String(), stdout.truncated
	res.Stderr, res.StderrTruncated = res.Stderr+stderr.String(), stderr.truncated
	res.ExitCode = exitCodeOf(cmd, waitErr)
	if res.TimedOut {
		res.Stderr = strings.TrimRight(res.Stderr, "\n")
		if res.Stderr != "" {
			res.Stderr += "\n"
		}
		res.Stderr += fmt.Sprintf("timed out after %s and was killed", r.timeout)
	}
}

// prelude is the ulimit line prepended to every script. It runs before the
// learner's text, and `ulimit` itself is on the dangerous list, so the limits
// cannot be raised again from inside.
func (r *Runner) prelude() string {
	limits := []string{
		"ulimit -c 0",
		fmt.Sprintf("ulimit -t %d", cpuSeconds),
		fmt.Sprintf("ulimit -f %d", fileBlocks),
		fmt.Sprintf("ulimit -v %d", addressKB),
	}
	if n := r.sandbox.ProcessLimit(); n > 0 {
		limits = append(limits, fmt.Sprintf("ulimit -u %d", n))
	}
	// Each limit is best-effort: a shell that does not support one must not
	// abort the run, and a limit that cannot be lowered is still bounded by
	// the wall clock and the sandbox.
	for i, l := range limits {
		limits[i] = l + " 2>/dev/null"
	}
	return strings.Join(limits, "; ") + "\n"
}

func exitCodeOf(cmd *exec.Cmd, waitErr error) int {
	if cmd.ProcessState != nil {
		return cmd.ProcessState.ExitCode()
	}
	if waitErr != nil {
		return -1
	}
	return 0
}

// limitedBuffer accumulates output up to a ceiling and then quietly drops the
// rest, so a pipeline that prints forever cannot exhaust memory.
type limitedBuffer struct {
	buf       []byte
	limit     int
	truncated bool
}

func (b *limitedBuffer) Write(p []byte) (int, error) {
	if room := b.limit - len(b.buf); room > 0 {
		if len(p) > room {
			b.buf = append(b.buf, p[:room]...)
			b.truncated = true
		} else {
			b.buf = append(b.buf, p...)
		}
	} else if len(p) > 0 {
		b.truncated = true
	}
	// The write is always reported as fully consumed: returning short would
	// make the child see a write error and change its behaviour.
	return len(p), nil
}

func (b *limitedBuffer) String() string { return string(b.buf) }
