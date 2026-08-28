package runner

import (
	"strings"
	"testing"

	"bash-teacher/internal/shellparse"
)

func testPolicy(t *testing.T) *Policy {
	t.Helper()
	return NewPolicy(testLibrary(t))
}

func check(t *testing.T, p *Policy, input string) []Violation {
	t.Helper()
	s, err := shellparse.Parse(input)
	if err != nil {
		t.Fatalf("Parse(%q): %v", input, err)
	}
	return p.Check(s)
}

func TestPolicyAcceptsOrdinaryPipelines(t *testing.T) {
	p := testPolicy(t)
	ok := []string{
		`cut -d' ' -f1 access.log | sort | uniq -c | sort -rn | head -5`,
		`grep -v '^$' notes.txt | wc -l`,
		`awk -F, '{print $3}' data.csv | sort -u`,
		`find . -name '*.log' | xargs wc -l`,
		`sed 's/../X/' f`,    // ".." here is a regex, not a path
		`tr '/' '-' < f`,     // a lone "/" is a separator, not a path
		`ls -la 2>/dev/null`, // redirecting to /dev/null is a taught idiom
		`sort f > out.txt`,
		`echo hi && printf '%s\n' done`,
		`wc -l ./sub/f`,
		`sed 's|a/../b|x|' f`, // resolves to "a/b", never leaves the fixture
		// The script operand of sed, awk and grep is a program, not a path,
		// and a program routinely starts with a slash.
		`sed -n '/09:00:04/,/09:00:07/p' build.log`,
		`grep '^/usr/bin' shells.txt`,
		`grep -A 1 /health access.log`,
		`awk '/^\/api/ { print $1 }' access.log`,
		`grep -- /health access.log`,
	}
	for _, in := range ok {
		if vs := check(t, p, in); len(vs) > 0 {
			t.Errorf("Check(%q) refused it: %v", in, vs)
		}
	}
}

func TestPolicyRefusals(t *testing.T) {
	p := testPolicy(t)
	tests := []struct {
		in   string
		kind Kind
		want string // substring of the message
	}{
		{"cat /etc/shadow", KindForbiddenPath, "absolute path"},
		{"cat ../../../etc/passwd", KindForbiddenPath, "climbs out"},
		{"rm -rf ~", KindForbiddenPath, "home directory"},
		{"wc -l --files0-from=/etc/passwd", KindForbiddenPath, "absolute path"},
		{"yes > /dev/full", KindForbiddenPath, "absolute path"},
		{"sort f > ../escape.txt", KindForbiddenPath, "climbs out"},
		{"curl https://example.com", KindNotExecutable, "never executes it"},
		{"wget https://example.com", KindNotExecutable, "never executes it"},
		{"sudo rm -rf .", KindDangerous, "no privilege to escalate"},
		{"eval 'rm -rf .'", KindDangerous, "hides from the static check"},
		{"exec sh", KindDangerous, "replace the sandbox shell"},
		{"ulimit -v unlimited", KindDangerous, "sets its own resource limits"},
		{"chmod 4755 f", KindDangerous, "setuid"},
		{"chmod u+s f", KindDangerous, "setuid"},
		{"python3 -c 'print(1)'", KindUnknownCommand, "not one of the commands"},
		{"bash -c 'ls'", KindUnknownCommand, "not one of the commands"},
		{"PATH=/tmp ls", KindAssignment, "variable assignments"},
		// Exempting the script operand must not exempt the files beside it.
		{"sed 's/a/b/' /etc/passwd", KindForbiddenPath, "absolute path"},
		{"grep root /etc/passwd", KindForbiddenPath, "absolute path"},
		{"awk '{print}' /etc/passwd", KindForbiddenPath, "absolute path"},
		{"grep -f /etc/passwd data.txt", KindForbiddenPath, "absolute path"},
		{"sed -f /etc/sed.script data.txt", KindForbiddenPath, "absolute path"},
		{"awk -f /tmp/prog.awk data.txt", KindForbiddenPath, "absolute path"},
		{"grep -e root /etc/passwd", KindForbiddenPath, "absolute path"},
		{"sed 's/a/b/' ../../etc/passwd", KindForbiddenPath, "climbs out"},
	}
	for _, tc := range tests {
		vs := check(t, p, tc.in)
		if len(vs) == 0 {
			t.Errorf("Check(%q) allowed it, want %s", tc.in, tc.kind)
			continue
		}
		found := false
		for _, v := range vs {
			if v.Kind == tc.kind && strings.Contains(v.Message, tc.want) {
				found = true
			}
		}
		if !found {
			t.Errorf("Check(%q) = %v, want a %s violation containing %q", tc.in, vs, tc.kind, tc.want)
		}
	}
}

// TestAllowlistIsDerivedFromTheDictionary guards the rule in CLAUDE.md: the
// executable set is exactly the dictionary's executable commands plus the
// shell builtins, and nothing was pasted in by hand.
func TestAllowlistIsDerivedFromTheDictionary(t *testing.T) {
	lib := testLibrary(t)
	p := NewPolicy(lib)

	want := map[string]bool{}
	for _, name := range lib.Allowlist() {
		want[name] = true
	}
	for _, b := range Builtins {
		want[b] = true
	}
	for _, name := range p.Names() {
		if !want[name] {
			t.Errorf("%q is executable but is not in the dictionary or the builtin list", name)
		}
		delete(want, name)
	}
	for name := range want {
		t.Errorf("%q is executable per the dictionary but the policy does not allow it", name)
	}
}

// TestDangerousListDoesNotContradictTheDictionary keeps the two sources of
// truth from disagreeing: a command the dictionary says is executable must not
// also be on the hard-refusal list, or the exercise that teaches it could
// never pass.
func TestDangerousListDoesNotContradictTheDictionary(t *testing.T) {
	lib := testLibrary(t)
	for _, c := range lib.Commands {
		if c.CanExecute() && dangerous[c.Name] != "" {
			t.Errorf("%q is executable in the dictionary but hard-refused by the policy", c.Name)
		}
	}
}

// TestNetworkCommandsAreNeverExecutable restates the invariant the M3 sandbox
// depends on, from the runner's side rather than the library's.
func TestNetworkCommandsAreNeverExecutable(t *testing.T) {
	lib := testLibrary(t)
	p := NewPolicy(lib)
	found := 0
	for _, c := range lib.Commands {
		if c.Category != "network" {
			continue
		}
		found++
		if p.Allows(c.Name) {
			t.Errorf("network command %q is on the allowlist", c.Name)
		}
	}
	if found == 0 {
		t.Fatal("the dictionary has no network commands, so this invariant is untested")
	}
}
