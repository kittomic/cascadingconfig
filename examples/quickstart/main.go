// Command quickstart is the README's quick start: it loads myapp's config with mise.Preset, from /etc/myapp,
// ~/.config/myapp, myapp.toml in this directory or any above it, and MYAPP_* environment variables, and prints it.
package main

import (
	"context"
	"fmt"
	"log"

	"github.com/kittomic/cascadingconfig"
	"github.com/kittomic/cascadingconfig/presets/mise"
)

// Config is the app's configuration.
type Config struct {
	Server struct {
		Port int `koanf:"port"`
	} `koanf:"server"`
	Log struct {
		Level string `koanf:"level"`
	} `koanf:"log"`
}

func main() {
	cfg, err := load(context.Background())
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("port %d, log level %s\n", cfg.Server.Port, cfg.Log.Level)
}

// load reads the config. opts are added to the loader's options, such as a test's fake filesystem.
func load(ctx context.Context, opts ...cascadingconfig.Option) (Config, error) {
	loader, err := cascadingconfig.NewWithPreset("myapp", mise.Preset, append(opts,
		// MYAPP_SERVER__PORT=9090 sets server.port, above every file.
		cascadingconfig.WithScope("env", cascadingconfig.EnvVars("MYAPP_")),
	)...)
	if err != nil {
		return Config{}, err
	}
	return cascadingconfig.Load[Config](ctx, loader)
}
