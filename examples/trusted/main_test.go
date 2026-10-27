package main

import (
	"bytes"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kittomic/cascadingconfig/cctest"
	"github.com/kittomic/cascadingconfig/examples/internal/exampletest"
)

// A project file is skipped until trusted, applies once trusted, and is skipped again after an edit until it's
// trusted again. The store's file lives in a temporary directory, never the real user config directory.
func TestTrusted(t *testing.T) {
	t.Parallel()
	fsys, world := exampletest.World(t, []string{"ccx-trusted.toml"}, nil, nil)
	trustFile := "-trust-file=" + filepath.Join(t.TempDir(), "trusted.json")
	var out bytes.Buffer
	step := func(note string, args ...string) {
		t.Helper()
		fmt.Fprintf(&out, "$ ccx-trusted %s  # %s\n", strings.Join(args, " "), note)
		if err := run(t.Context(), append([]string{trustFile}, args...), &out, world); err != nil {
			t.Fatalf("%s: %v", note, err)
		}
	}
	step("before trusting")
	step("approve", "trust")
	step("after trusting")
	step("nothing changed", "trust")
	fsys.WriteFile(exampletest.Cwd+"/ccx-trusted.toml", []byte("[deploy]\ntarget = \"production\"\n"), 0o644)
	step("after an edit")
	step("approve the edit", "trust")
	step("after trusting the edit")
	cctest.Golden(t, "trusted.golden", out.Bytes())
}

func TestTrustedUsage(t *testing.T) {
	t.Parallel()
	_, world := exampletest.World(t, nil, nil, nil)
	trustFile := "-trust-file=" + filepath.Join(t.TempDir(), "trusted.json")
	for _, args := range [][]string{{trustFile, "distrust"}, {trustFile, "trust", "extra"}} {
		if err := run(t.Context(), args, &bytes.Buffer{}, world); err == nil || !strings.Contains(err.Error(), "usage") {
			t.Errorf("%q: %v", args, err)
		}
	}
}
