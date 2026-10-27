// Command ccx-watch stands in for a long-running service that picks up config edits without a restart. It watches
// every file mise.Preset would load, and on each change prints the keys that changed and the config it now runs
// with. An edit that breaks the config is reported, and the service keeps its last good config.
//
//	go run .     # then edit ccx-watch.toml and save; Ctrl-C stops it
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"sync"

	"github.com/kittomic/cascadingconfig"
	"github.com/kittomic/cascadingconfig/presets/mise"
)

// Config is the service's configuration.
type Config struct {
	Server struct {
		Port int `koanf:"port"`
	} `koanf:"server"`
	Limits struct {
		RequestsPerSecond int `koanf:"requests_per_second"`
		Burst             int `koanf:"burst"`
	} `koanf:"limits"`
}

func (c Config) String() string {
	return fmt.Sprintf("port %d, %d requests/s, burst %d", c.Server.Port, c.Limits.RequestsPerSecond, c.Limits.Burst)
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	err := run(ctx, os.Args[1:], os.Stdout)
	stop()
	if err != nil {
		fmt.Fprintln(os.Stderr, "ccx-watch:", err)
		os.Exit(1)
	}
}

// run serves until ctx is cancelled, reporting each config change. opts are added to the loader's options.
func run(ctx context.Context, args []string, out io.Writer, opts ...cascadingconfig.Option) error {
	if len(args) > 0 {
		return errors.New("ccx-watch takes no arguments")
	}
	loader, err := cascadingconfig.NewWithPreset("ccx-watch", mise.Preset, opts...)
	if err != nil {
		return err
	}

	// The handler runs on the watcher's goroutine, so printing is serialised.
	var mu sync.Mutex
	printf := func(format string, args ...any) {
		mu.Lock()
		defer mu.Unlock()
		fmt.Fprintf(out, format, args...)
	}

	w, err := cascadingconfig.Watch(ctx, loader, func(_ context.Context, c cascadingconfig.Change[Config]) {
		if c.Err != nil {
			printf("reload failed, keeping the last good config: %v\n", c.Err)
			return
		}
		for _, d := range c.Diff {
			switch d.Kind {
			case cascadingconfig.DiffAdded:
				printf("  + %s = %v\n", d.Path, d.New)
			case cascadingconfig.DiffRemoved:
				printf("  - %s (was %v)\n", d.Path, d.Old)
			default:
				printf("  ~ %s: %v → %v\n", d.Path, d.Old, d.New)
			}
		}
		printf("now serving with %s\n", c.New)
	})
	if err != nil {
		return err // the first load failed, so there's no config to serve with
	}

	printf("serving with %s\n", w.Current())
	printf("watching for changes; press Ctrl-C to stop\n")
	<-ctx.Done()
	if err := w.Close(); err != nil {
		return err
	}
	printf("stopped\n")
	return nil
}
