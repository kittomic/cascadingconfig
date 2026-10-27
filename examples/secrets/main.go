// Command ccx-secrets reads its secrets from a mounted directory, as Kubernetes and Docker mount them: one file per
// setting, named by its key (db.password), holding the value. No layout searches such a place, so a custom scope
// reads it, above the config files. The scope is watchable, so with -watch a rotated secret is picked up.
//
//	go run .                                  # secrets from ./secrets
//	go run . -watch                           # then edit secrets/db.password
//	go run . -secrets /run/secrets/ccx-secrets
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"sync"

	"github.com/kittomic/cascadingconfig"
	"github.com/kittomic/cascadingconfig/presets/mise"
)

// Config is the app's configuration, secrets included.
type Config struct {
	DB struct {
		Host     string `koanf:"host"`
		User     string `koanf:"user"`
		Password string `koanf:"password"`
	} `koanf:"db"`
	API struct {
		URL   string `koanf:"url"`
		Token string `koanf:"token"`
	} `koanf:"api"`
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	err := run(ctx, os.Args[1:], os.Stdout)
	stop()
	if err != nil && !errors.Is(err, flag.ErrHelp) {
		fmt.Fprintln(os.Stderr, "ccx-secrets:", err)
		os.Exit(1)
	}
}

// run loads the config and prints it with the secrets redacted; with -watch it then reports changes until ctx is
// cancelled. opts are added to the loader's options.
func run(ctx context.Context, args []string, out io.Writer, opts ...cascadingconfig.Option) error {
	flags := flag.NewFlagSet("ccx-secrets", flag.ContinueOnError)
	flags.SetOutput(out)
	dir := flags.String("secrets", "secrets", "the mounted secrets directory")
	watch := flags.Bool("watch", false, "keep running and report changes")
	if err := flags.Parse(args); err != nil {
		return err
	}
	preset := mise.Preset.Extend(
		cascadingconfig.WithScope("secrets", cascadingconfig.Custom(secretsDir{dir: *dir})),
	)
	loader, err := cascadingconfig.NewWithPreset("ccx-secrets", preset, opts...)
	if err != nil {
		return err
	}
	if !*watch {
		cfg, err := cascadingconfig.Load[Config](ctx, loader)
		if err != nil {
			return err
		}
		show(out, cfg, newSecretVersions())
		return nil
	}

	// The handler runs on the watcher's goroutine, so printing is serialised.
	var mu sync.Mutex
	versions := newSecretVersions()
	w, err := cascadingconfig.Watch(ctx, loader, func(_ context.Context, c cascadingconfig.Change[Config]) {
		mu.Lock()
		defer mu.Unlock()
		if c.Err != nil {
			fmt.Fprintf(out, "reload failed, keeping the last good config: %v\n", c.Err)
			return
		}
		for _, d := range c.Diff {
			fmt.Fprintf(out, "%s\n", d) // a KeyDiff prints its key and origins, never the values
		}
		show(out, c.New, versions)
	})
	if err != nil {
		return err
	}
	mu.Lock()
	show(out, w.Current(), versions)
	mu.Unlock()
	<-ctx.Done()
	err = w.Close()
	<-w.Done() // Close doesn't wait for a handler call that's still running
	return err
}

// show prints cfg with each secret replaced by its version, so a rotation is visible, but nothing derived from a
// secret's value is printed: even a hash of it would let anyone with the output test guesses at a weak password.
func show(out io.Writer, cfg Config, v *secretVersions) {
	fmt.Fprintf(out, "db:  %s@%s, password %s\n", cfg.DB.User, cfg.DB.Host, v.label("db.password", cfg.DB.Password))
	fmt.Fprintf(out, "api: %s, token %s\n", cfg.API.URL, v.label("api.token", cfg.API.Token))
}

// secretVersions numbers each secret's values in the order they're seen. The values it compares against stay in
// memory.
type secretVersions struct {
	last    map[string]string
	version map[string]int
}

func newSecretVersions() *secretVersions {
	return &secretVersions{last: map[string]string{}, version: map[string]int{}}
}

// label describes the secret called name: "not set", or "set, version n", where n goes up each time the value changes.
func (v *secretVersions) label(name, secret string) string {
	if secret == "" {
		return "not set"
	}
	if v.last[name] != secret {
		v.last[name] = secret
		v.version[name]++
	}
	return fmt.Sprintf("set, version %d", v.version[name])
}
