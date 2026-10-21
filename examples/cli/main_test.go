package main

import (
	"bytes"
	"errors"
	"flag"
	"strings"
	"testing"

	"github.com/kittomic/cascadingconfig/cctest"
	"github.com/kittomic/cascadingconfig/examples/internal/exampletest"
)

var shipped = []string{".ccx-clirc", "team.yaml"}

func TestCLI(t *testing.T) {
	for _, c := range []struct {
		name string
		args []string
	}{
		{"shipped", nil},
		{"flag", []string{"-name", "Ada"}},
		{"config", []string{"-config", "team.yaml"}},
		{"config-and-flag", []string{"-config", "team.yaml", "-shout=false"}},
		{"explain-flag", []string{"-name", "Ada", "-explain", "name"}},
		{"explain-file", []string{"-explain", "greeting"}},
	} {
		t.Run(c.name, func(t *testing.T) {
			_, world := exampletest.World(t, shipped, nil, nil)
			var out bytes.Buffer
			if err := run(t.Context(), c.args, &out, world); err != nil {
				t.Fatal(err)
			}
			cctest.Golden(t, c.name+".golden", out.Bytes())
		})
	}
}

func TestCLIErrors(t *testing.T) {
	_, world := exampletest.World(t, shipped, nil, nil)
	if err := run(t.Context(), []string{"-explain", "nope"}, &bytes.Buffer{}, world); err == nil ||
		!strings.Contains(err.Error(), "nope isn't set") {
		t.Errorf("explaining an unset key: %v", err)
	}
	if err := run(t.Context(), []string{"-config", "missing.yaml"}, &bytes.Buffer{}, world); err == nil {
		t.Error("a missing -config file loaded")
	}
	var help bytes.Buffer
	if err := run(t.Context(), []string{"-h"}, &help, world); !errors.Is(err, flag.ErrHelp) ||
		!strings.Contains(help.String(), "-explain") {
		t.Errorf("-h: %v, printing %q", err, help.String())
	}
}
