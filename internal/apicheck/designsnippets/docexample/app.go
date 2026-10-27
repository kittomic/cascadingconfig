package main

import (
	"context"

	"github.com/kittomic/cascadingconfig"
)

// What api.md's example takes from the rest of the app.

type Config struct {
	Port int `koanf:"port"`
}

var (
	ctx             = context.Background()
	flagConfigFiles []string
	flagEnvs        []string
)

func run(Config) {}

func onChange(context.Context, cascadingconfig.Change[Config]) {}
