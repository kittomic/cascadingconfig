package main

import (
	"bytes"
	"testing"

	"github.com/kittomic/cascadingconfig/cctest"
	"github.com/kittomic/cascadingconfig/examples/internal/exampletest"
)

var shipped = []string{"ccx-basic.toml", "ccx-basic.dev.toml"}

func TestBasic(t *testing.T) {
	for _, c := range []struct {
		name string
		env  map[string]string
	}{
		{"shipped", nil},
		{"dev", map[string]string{"CCX_BASIC_ENV": "dev"}},
		{"env-var", map[string]string{"CCX_BASIC_ENV": "dev", "CCX_BASIC_SERVER__PORT": "9090"}},
	} {
		t.Run(c.name, func(t *testing.T) {
			_, world := exampletest.World(t, shipped, nil, c.env)
			var out bytes.Buffer
			if err := run(t.Context(), nil, &out, world); err != nil {
				t.Fatal(err)
			}
			cctest.Golden(t, c.name+".golden", out.Bytes())
		})
	}
}

func TestBasicRejectsArguments(t *testing.T) {
	_, world := exampletest.World(t, shipped, nil, nil)
	if err := run(t.Context(), []string{"extra"}, &bytes.Buffer{}, world); err == nil {
		t.Error("run accepted an argument")
	}
}
