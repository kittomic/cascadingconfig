// Package exampletest builds the fake host the examples' tests run on, so no test reads the real machine: the config
// files an example ships, placed in the working directory of a Linux host that has nothing else.
package exampletest

import (
	"os"
	"path"
	"testing"

	"github.com/kittomic/cascadingconfig"
	"github.com/kittomic/cascadingconfig/cctest"
)

// Home and Cwd are the fake host's home and working directories.
const (
	Home = "/home/me"
	Cwd  = Home + "/project"
)

// World reads the files named in shipped from the example's directory, places them under Cwd in a cctest.FS with
// extra (absolute paths) added, and returns the FS, for tests that edit it, and the options that load from it on a
// Linux host with env.
func World(t testing.TB, shipped []string, extra, env map[string]string) (*cctest.FS, cascadingconfig.Option) {
	t.Helper()
	files := map[string]string{}
	for _, name := range shipped {
		data, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		files[path.Join(Cwd, name)] = string(data)
	}
	for p, data := range extra {
		files[p] = data
	}
	fsys := cctest.NewFS(files)
	host := cctest.Host{GOOS: "linux", GOARCH: "amd64", Home: Home, Cwd: Cwd, Env: env}
	return fsys, cascadingconfig.Options(cascadingconfig.WithFileSystem(fsys), cctest.WithHost(host))
}
