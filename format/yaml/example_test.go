// The example uses Unix paths in cctest's world, so, like the tests that do, it doesn't run on Windows.

//go:build !windows

package yaml_test

import (
	"context"
	"fmt"
	"log"

	"github.com/kittomic/cascadingconfig"
	"github.com/kittomic/cascadingconfig/cctest"
	"github.com/kittomic/cascadingconfig/format/yaml"
)

// A layout from the core module reads JSON only; registering the decoder lets it read YAML files too. Unquoted values
// get YAML 1.2's types, so "no" is a string.
func ExampleDecoder() {
	fsys := cctest.NewFS(map[string]string{"/home/me/work/myapp.yaml": "server:\n  port: 8080\ncountry: no\n"})
	host := cctest.Host{GOOS: "linux", GOARCH: "amd64", Home: "/home/me", Cwd: "/home/me/work"}

	loader, err := cascadingconfig.NewWithPreset("myapp", cascadingconfig.MiseLayout,
		cascadingconfig.WithDecoder(yaml.Decoder()), cascadingconfig.WithFileSystem(fsys), cctest.WithHost(host))
	if err != nil {
		log.Fatal(err)
	}
	res, err := loader.Load(context.Background())
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("%q\n", res.Tree()["country"])
	// Output: "no"
}
