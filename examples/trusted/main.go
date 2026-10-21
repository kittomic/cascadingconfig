// Command ccx-trusted applies a project's config only once you've approved it, as mise trust does. Config in a cloned
// repository is input from whoever wrote it, so misehardened.Preset requires the project scope's files to be trusted:
// until then they're skipped with a warning. "ccx-trusted trust" approves the current files, and an approval covers
// a file's exact content, so an edit needs approving again.
//
//	go run .          # the project file is skipped: not trusted yet
//	go run . trust    # approve it
//	go run .          # now it applies
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"

	"github.com/kittomic/cascadingconfig"
	"github.com/kittomic/cascadingconfig/presets/misehardened"
)

// Config is the app's configuration.
type Config struct {
	Deploy struct {
		Target   string `koanf:"target"`
		Replicas int    `koanf:"replicas"`
	} `koanf:"deploy"`
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	err := run(ctx, os.Args[1:], os.Stdout)
	stop()
	if err != nil && !errors.Is(err, flag.ErrHelp) {
		fmt.Fprintln(os.Stderr, "ccx-trusted:", err)
		os.Exit(1)
	}
}

// run shows the config, or with "trust" approves the project's files. opts are added to the loader's options.
func run(ctx context.Context, args []string, out io.Writer, opts ...cascadingconfig.Option) error {
	flags := flag.NewFlagSet("ccx-trusted", flag.ContinueOnError)
	flags.SetOutput(out)
	trustFile := flags.String("trust-file", "", "where approvals are kept (default: in the user config directory)")
	if err := flags.Parse(args); err != nil {
		return err
	}
	command := flags.Arg(0)
	if flags.NArg() > 1 || (command != "" && command != "trust") {
		return errors.New("usage: ccx-trusted [-trust-file file] [trust]")
	}
	if *trustFile == "" {
		dir, err := os.UserConfigDir()
		if err != nil {
			return fmt.Errorf("no user config directory, so set -trust-file: %w", err)
		}
		*trustFile = filepath.Join(dir, "ccx-trusted", "trusted.json")
	}
	store, err := openStore(*trustFile)
	if err != nil {
		return err
	}

	var defaults Config
	defaults.Deploy.Target, defaults.Deploy.Replicas = "local", 1
	preset := misehardened.Preset.Extend(
		cascadingconfig.WithDefaults(defaults),
		cascadingconfig.WithScopePolicy(cascadingconfig.ScopeProject,
			cascadingconfig.InheritDefault(), // keep the hardened rules, and add trust
			cascadingconfig.RequireTrust(store),
		),
	)
	loader, err := cascadingconfig.NewWithPreset("ccx-trusted", preset, opts...)
	if err != nil {
		return err
	}
	res, err := loader.Load(ctx)
	if err != nil {
		return err
	}

	if command == "trust" {
		added, err := store.approveAsked()
		if err != nil {
			return err
		}
		if len(added) == 0 {
			fmt.Fprintln(out, "nothing new to trust")
		}
		for _, path := range added {
			fmt.Fprintf(out, "trusted %s\n", path)
		}
		return nil
	}

	var cfg Config
	if err := res.Decode(&cfg); err != nil {
		return err
	}
	fmt.Fprintf(out, "deploying to %s with %d replicas\n", cfg.Deploy.Target, cfg.Deploy.Replicas)
	for _, layer := range res.Describe().Layers {
		if layer.Status == cascadingconfig.LayerUntrusted {
			fmt.Fprintf(out, "skipped %s: not trusted; run \"ccx-trusted trust\" to approve it\n", layer.Path)
		}
	}
	return nil
}
