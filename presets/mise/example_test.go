// The example uses Unix paths in cctest's world, so, like the tests that do, it doesn't run on Windows.

//go:build !windows

package mise_test

import (
	"context"
	"fmt"
	"log"

	"github.com/kittomic/cascadingconfig"
	"github.com/kittomic/cascadingconfig/cctest"
	"github.com/kittomic/cascadingconfig/presets/mise"
)

// Config is the example's app config.
type Config struct {
	Server struct {
		Host string `koanf:"host"`
		Port int    `koanf:"port"`
	} `koanf:"server"`
}

// mise.Preset reads system, user and project config, in TOML, YAML or JSON. Here the project's TOML file overrides the
// system's YAML file. The world comes from cctest, so the example reads nothing from the machine it runs on.
func Example() {
	fsys := cctest.NewFS(map[string]string{
		"/etc/myapp/config.yaml":   "server:\n  host: 0.0.0.0\n  port: 80\n",
		"/home/me/work/myapp.toml": "[server]\nport = 8080\n",
	})
	host := cctest.Host{GOOS: "linux", GOARCH: "amd64", Home: "/home/me", Cwd: "/home/me/work"}

	loader, err := cascadingconfig.NewWithPreset("myapp", mise.Preset,
		cascadingconfig.WithFileSystem(fsys), cctest.WithHost(host))
	if err != nil {
		log.Fatal(err)
	}
	cfg, err := cascadingconfig.Load[Config](context.Background(), loader)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(cfg.Server.Host, cfg.Server.Port)
	// Output: 0.0.0.0 8080
}
