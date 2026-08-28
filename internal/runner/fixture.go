package runner

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
)

// maxFixtureFileBytes mirrors the ceiling the content linter enforces. It is
// repeated here because materialize also runs against fixtures in tests, which
// do not go through the linter.
const maxFixtureFileBytes = 256 * 1024

// fixtureRoot is a materialized copy of one fixture directory, living in a
// temp directory that is destroyed after the run.
type fixtureRoot struct {
	// Dir is the path handed to the sandbox as the working directory. It is
	// fully resolved, because the macOS sandbox profile matches on real paths
	// and /var is a symlink to /private/var.
	Dir string
	tmp string
}

// materialize copies content/fixtures/<name> into a fresh temp directory.
//
// The copy is what makes exercises safe to mutate: a pipeline is free to
// rewrite or delete anything it finds, and the next run starts from the
// pristine embedded tree again. Only regular files are copied, so a fixture
// can never introduce a symlink pointing out of the sandbox.
func materialize(src fs.FS, name string) (*fixtureRoot, error) {
	if name == "" {
		return nil, errors.New("exercise has no fixture")
	}
	entries, err := fs.ReadDir(src, path.Join("fixtures", name))
	if err != nil {
		return nil, fmt.Errorf("fixture %q: %w", name, err)
	}

	tmp, err := os.MkdirTemp("", "bt-fixture-")
	if err != nil {
		return nil, fmt.Errorf("create sandbox directory: %w", err)
	}
	root := &fixtureRoot{tmp: tmp, Dir: tmp}
	// A failure anywhere below leaves nothing behind.
	defer func() {
		if err != nil {
			root.Close()
		}
	}()

	if resolved, rerr := filepath.EvalSymlinks(tmp); rerr == nil {
		root.Dir = resolved
	}

	for _, e := range entries {
		if e.IsDir() {
			err = fmt.Errorf("fixture %q contains a subdirectory %q; fixtures are flat", name, e.Name())
			return nil, err
		}
		var data []byte
		if data, err = fs.ReadFile(src, path.Join("fixtures", name, e.Name())); err != nil {
			return nil, fmt.Errorf("read fixture file %q: %w", e.Name(), err)
		}
		if len(data) > maxFixtureFileBytes {
			err = fmt.Errorf("fixture %q: %q is %d bytes, over the %d-byte limit",
				name, e.Name(), len(data), maxFixtureFileBytes)
			return nil, err
		}
		if err = os.WriteFile(filepath.Join(root.Dir, e.Name()), data, 0o600); err != nil {
			return nil, fmt.Errorf("write fixture file %q: %w", e.Name(), err)
		}
	}
	return root, nil
}

// Close removes the materialized copy. It is called unconditionally, including
// on panic, so a crashed run cannot leak a directory.
func (f *fixtureRoot) Close() {
	if f == nil || f.tmp == "" {
		return
	}
	_ = os.RemoveAll(f.tmp)
	f.tmp, f.Dir = "", ""
}
