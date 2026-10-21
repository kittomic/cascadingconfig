package main

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"path/filepath"
	"strings"

	"github.com/kittomic/cascadingconfig"
)

// secretsDir implements both interfaces a custom scope can.
var (
	_ cascadingconfig.Scope     = secretsDir{}
	_ cascadingconfig.Watchable = secretsDir{}
)

// secretsDir is a custom scope that reads a secrets directory: each file is one setting, and its name, split at dots,
// is the key. Names starting with a dot are skipped, such as the ..data directory Kubernetes keeps its files in.
type secretsDir struct{ dir string }

// path is the directory, with a relative one taken from the working directory.
func (s secretsDir) path(d cascadingconfig.Discovery) string {
	if filepath.IsAbs(s.dir) {
		return s.dir
	}
	return filepath.Join(d.StartDir, s.dir)
}

// Discover returns the directory's settings as one tree. A missing directory means no secrets are mounted.
func (s secretsDir) Discover(_ context.Context, d cascadingconfig.Discovery) ([]cascadingconfig.Source, error) {
	root, err := d.FS.OpenRoot(s.path(d))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer root.Close()
	entries, err := root.ReadDir(".")
	if err != nil {
		return nil, err
	}
	tree := cascadingconfig.Tree{}
	for _, e := range entries {
		// Stat follows symlinks: Kubernetes mounts each key as a link into ..data.
		if info, err := root.Stat(e.Name()); strings.HasPrefix(e.Name(), ".") || err != nil || !info.Mode().IsRegular() {
			continue
		}
		value, err := readSecret(root, e.Name())
		if err != nil {
			return nil, err
		}
		set(tree, strings.Split(e.Name(), "."), value)
	}
	if len(tree) == 0 {
		return nil, nil
	}
	return []cascadingconfig.Source{{Tree: tree}}, nil
}

// WatchDirs makes a watched load reload when anything in the directory changes, such as a rotation.
func (s secretsDir) WatchDirs(_ context.Context, d cascadingconfig.Discovery) ([]string, error) {
	return []string{s.path(d)}, nil
}

// readSecret returns a secret file's content, without a trailing newline. Secrets are small, so it reads at most
// 64 KiB.
func readSecret(root cascadingconfig.Root, name string) (string, error) {
	f, err := root.Open(name)
	if err != nil {
		return "", err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, 64<<10))
	return strings.TrimRight(string(data), "\r\n"), err
}

// set puts value at path in tree, making tables on the way.
func set(tree cascadingconfig.Tree, path []string, value string) {
	for _, key := range path[:len(path)-1] {
		next, ok := tree[key].(map[string]any)
		if !ok {
			next = map[string]any{}
			tree[key] = next
		}
		tree = next
	}
	tree[path[len(path)-1]] = value
}
