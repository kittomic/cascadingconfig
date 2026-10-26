package main

import (
	"os"
	"regexp"
	"testing"

	"github.com/kittomic/cascadingconfig/examples/internal/exampletest"
)

func TestQuickstart(t *testing.T) {
	for _, c := range []struct {
		name  string
		env   map[string]string
		port  int
		level string
	}{
		{"shipped", nil, 8080, "info"},
		{"env vars", map[string]string{"MYAPP_SERVER__PORT": "9090", "MYAPP_LOG__LEVEL": "debug"}, 9090, "debug"},
	} {
		t.Run(c.name, func(t *testing.T) {
			_, world := exampletest.World(t, []string{"myapp.toml"}, nil, c.env)
			cfg, err := load(t.Context(), world)
			if err != nil {
				t.Fatal(err)
			}
			if cfg.Server.Port != c.port || cfg.Log.Level != c.level {
				t.Errorf("port %d, level %q; want %d, %q", cfg.Server.Port, cfg.Log.Level, c.port, c.level)
			}
		})
	}
}

// DL-265: the README's quick start is this program, exactly, so copying it out always gives one that works.
func TestREADMEHoldsTheQuickstart(t *testing.T) {
	readme, err := os.ReadFile("../../README.md")
	if err != nil {
		t.Fatal(err)
	}
	src, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatal(err)
	}
	m := regexp.MustCompile("(?s)<!-- quickstart -->\\s*```go\\n(.*?)```").FindSubmatch(readme)
	if m == nil {
		t.Fatal("README.md has no Go block after a <!-- quickstart --> marker")
	}
	if string(m[1]) != string(src) {
		t.Error("README.md's quick start differs from examples/quickstart/main.go; copy the file into the README")
	}
}
