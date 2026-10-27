// The example uses Unix paths in cctest's world, so, like the tests that do, it doesn't run on Windows.

//go:build !windows

package toml_test

import (
	"context"
	"fmt"
	"log"

	"github.com/kittomic/cascadingconfig"
	"github.com/kittomic/cascadingconfig/cctest"
	"github.com/kittomic/cascadingconfig/format/toml"
)

// A layout from the core module reads JSON only; registering the decoder lets it read TOML files too.
func ExampleDecoder() {
	fsys := cctest.NewFS(map[string]string{"/home/me/work/myapp.toml": "[server]\nport = 8080\n"})
	host := cctest.Host{GOOS: "linux", GOARCH: "amd64", Home: "/home/me", Cwd: "/home/me/work"}

	loader, err := cascadingconfig.NewWithPreset("myapp", cascadingconfig.MiseLayout,
		cascadingconfig.WithDecoder(toml.Decoder()), cascadingconfig.WithFileSystem(fsys), cctest.WithHost(host))
	if err != nil {
		log.Fatal(err)
	}
	var cfg struct {
		Server struct {
			Port int `koanf:"port"`
		} `koanf:"server"`
	}
	res, err := loader.Load(context.Background())
	if err != nil {
		log.Fatal(err)
	}
	if err := res.Decode(&cfg); err != nil {
		log.Fatal(err)
	}
	fmt.Println(cfg.Server.Port)
	// Output: 8080
}
