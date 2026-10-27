package httpresolver_test

import (
	"log"

	"github.com/kittomic/cascadingconfig"
	"github.com/kittomic/cascadingconfig/httpresolver"
)

// A layout from the core module fetches no remote includes until a resolver is registered. Here a policy also limits
// them to pinned references on one site, so a file can include
// https://config.acme.example/myapp/base.toml#sha256=<hex> and nothing else remote.
func ExampleNew() {
	loader, err := cascadingconfig.NewWithPreset("myapp", cascadingconfig.MiseLayout,
		cascadingconfig.WithIncludeResolver(httpresolver.New()),
		cascadingconfig.WithDefaultPolicy(
			cascadingconfig.Allow(cascadingconfig.Prefix("https://config.acme.example/")),
			cascadingconfig.RequirePinned(),
		),
	)
	if err != nil {
		log.Fatal(err)
	}
	_ = loader // loader.Load fetches the includes its files name
	// Output:
}
