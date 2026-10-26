// Command ccx-cli is a command-line tool whose flags beat its config files, in cosmiconfig's style: the first config
// file found wins, -config names one explicitly, and -explain shows where any setting came from.
//
//	go run .                       # Hello, world! from .ccx-clirc
//	go run . -name Ada             # a flag beats the file
//	go run . -config team.yaml     # an explicit file replaces the ones found
//	go run . -name Ada -explain name
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"

	"github.com/kittomic/cascadingconfig"
	"github.com/kittomic/cascadingconfig/presets/cosmiconfig"
)

// Config is the tool's configuration.
type Config struct {
	Greeting string `koanf:"greeting"`
	Name     string `koanf:"name"`
	Shout    bool   `koanf:"shout"`
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	err := run(ctx, os.Args[1:], os.Stdout)
	stop()
	if errors.Is(err, flag.ErrHelp) {
		return
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "ccx-cli:", err)
		os.Exit(2)
	}
}

// run parses args, loads the config and greets, or explains a setting. opts are added to the loader's options.
func run(ctx context.Context, args []string, out io.Writer, opts ...cascadingconfig.Option) error {
	flags := flag.NewFlagSet("ccx-cli", flag.ContinueOnError)
	flags.SetOutput(out)
	flags.String("greeting", "", "the greeting")
	flags.String("name", "", "who to greet")
	flags.Bool("shout", false, "greet in capitals")
	configFile := flags.String("config", "", "use this config file instead of searching")
	explain := flags.String("explain", "", "show where a setting comes from, such as name")
	if err := flags.Parse(args); err != nil {
		return err
	}

	// Only the flags given on the command line become a layer, so a flag left out doesn't hide the file's value.
	given := cascadingconfig.Tree{}
	flags.Visit(func(f *flag.Flag) {
		switch f.Name {
		case "greeting", "name":
			given[f.Name] = f.Value.String()
		case "shout":
			given[f.Name] = f.Value.(flag.Getter).Get()
		}
	})
	preset := cosmiconfig.Preset.Extend(
		cascadingconfig.WithScope("flags", cascadingconfig.Values(given)),
	)
	if *configFile != "" {
		opts = append(opts, cascadingconfig.WithConfigFiles(*configFile))
	}
	loader, err := cascadingconfig.NewWithPreset("ccx-cli", preset, opts...)
	if err != nil {
		return err
	}
	res, err := loader.Load(ctx)
	if err != nil {
		return err
	}
	if *explain != "" {
		return explainKey(out, res, *explain)
	}

	var cfg Config
	if err := res.Decode(&cfg); err != nil {
		return err
	}
	greeting := fmt.Sprintf("%s, %s!", cfg.Greeting, cfg.Name)
	if cfg.Shout {
		greeting = strings.ToUpper(greeting)
	}
	fmt.Fprintln(out, greeting)
	return nil
}

// explainKey prints key's value, the layer that set it, and what earlier layers did to it.
func explainKey(out io.Writer, res *cascadingconfig.Result, key string) error {
	origin, ok := cascadingconfig.OriginOf(res, key)
	if !ok {
		return fmt.Errorf("%s isn't set by any layer", key)
	}
	var value any
	if err := cascadingconfig.DecodeAt(res, key, &value); err != nil {
		return err
	}
	fmt.Fprintf(out, "%s = %v\n", key, value)
	fmt.Fprintf(out, "  %s by %s\n", origin.Kind, describe(origin.Layer))
	for i := len(origin.History) - 1; i >= 0; i-- {
		step := origin.History[i]
		fmt.Fprintf(out, "  earlier: %s by %s\n", step.Kind, describe(step.Layer))
	}
	return nil
}

// describe names a layer: its scope, and its file if it has one.
func describe(layer cascadingconfig.Layer) string {
	if layer.Path == "" {
		return fmt.Sprintf("the %s scope", layer.Scope)
	}
	return fmt.Sprintf("%s (%s)", layer.Path, layer.Scope)
}
