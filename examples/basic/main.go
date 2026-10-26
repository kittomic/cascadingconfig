// Command ccx-basic loads layered configuration the way most tools want it: built-in defaults, then the config files
// that mise.Preset finds (system, user and project, plus the selected environment's files), then environment
// variables. It prints the config it ends up with, and the layers that made it.
//
//	go run .                              # defaults and ccx-basic.toml
//	CCX_BASIC_ENV=dev go run .            # adds ccx-basic.dev.toml
//	CCX_BASIC_SERVER__PORT=9090 go run .  # an environment variable beats every file
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"

	"github.com/kittomic/cascadingconfig"
	"github.com/kittomic/cascadingconfig/presets/mise"
)

// Config is the app's configuration.
type Config struct {
	Server   Server   `koanf:"server"`
	Log      Log      `koanf:"log"`
	Features []string `koanf:"features"`
}

// Server is where the app listens.
type Server struct {
	Host string `koanf:"host"`
	Port int    `koanf:"port"`
}

// Log configures logging.
type Log struct {
	Level  string `koanf:"level"`
	Format string `koanf:"format"`
}

// defaults is the config before any file or variable is read.
func defaults() Config {
	return Config{
		Server: Server{Host: "localhost", Port: 8000},
		Log:    Log{Level: "warn", Format: "text"},
	}
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	err := run(ctx, os.Args[1:], os.Stdout)
	stop()
	if err != nil {
		fmt.Fprintln(os.Stderr, "ccx-basic:", err)
		os.Exit(1)
	}
}

// run loads the config and prints it. opts are added to the loader's options.
func run(ctx context.Context, args []string, out io.Writer, opts ...cascadingconfig.Option) error {
	if len(args) > 0 {
		return errors.New("ccx-basic takes no arguments")
	}
	preset := mise.Preset.Extend(
		cascadingconfig.WithDefaults(defaults()),
		// CCX_BASIC_SERVER__PORT=9090 sets server.port: the prefix goes, and __ separates levels.
		cascadingconfig.WithScope("env", cascadingconfig.EnvVars("CCX_BASIC_")),
		// Defining a scope doesn't select it, so the selection lists the layout's scopes too.
		cascadingconfig.WithScopes(cascadingconfig.ScopeSystem, cascadingconfig.ScopeUser,
			cascadingconfig.ScopeProject, cascadingconfig.ScopeOverride, "env"),
	)
	loader, err := cascadingconfig.NewWithPreset("ccx-basic", preset, opts...)
	if err != nil {
		return err
	}
	res, err := loader.Load(ctx)
	if err != nil {
		return err
	}
	var cfg Config
	if err := res.Decode(&cfg); err != nil {
		return err
	}

	fmt.Fprintf(out, "server:   %s:%d\n", cfg.Server.Host, cfg.Server.Port)
	fmt.Fprintf(out, "log:      %s, %s\n", cfg.Log.Level, cfg.Log.Format)
	fmt.Fprintf(out, "features: %v\n", cfg.Features)

	report := res.Describe()
	envs := "none"
	if len(report.Envs.Selected) > 0 {
		envs = strings.Join(report.Envs.Selected, ", ")
	}
	fmt.Fprintf(out, "\nlayers, lowest first (environments: %s):\n", envs)
	for _, layer := range report.Layers {
		if layer.Status != cascadingconfig.LayerLoaded {
			continue
		}
		source := layer.Path
		switch {
		case layer.Scope == cascadingconfig.ScopeDefaults:
			source = "(built-in defaults)"
		case source == "":
			source = "(CCX_BASIC_* variables)"
		}
		fmt.Fprintf(out, "  %-9s %s\n", layer.Scope, source)
	}
	for _, w := range report.Warnings {
		fmt.Fprintf(out, "warning: %s\n", w.Message)
	}
	return nil
}
