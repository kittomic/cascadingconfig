package gitresolver_test

import (
	"log"

	"github.com/kittomic/cascadingconfig"
	"github.com/kittomic/cascadingconfig/gitresolver"
)

// A layout from the core module fetches no remote includes until a resolver is registered. Here git may only use
// https, and a policy allows one organisation's repositories, so a file can include
// git::https://github.com/acme/config.git//myapp/base.toml?ref=v1.4.0.
func ExampleNew() {
	loader, err := cascadingconfig.NewWithPreset("myapp", cascadingconfig.MiseLayout,
		cascadingconfig.WithIncludeResolver(gitresolver.New(gitresolver.WithProtocols("https"))),
		cascadingconfig.WithDefaultPolicy(
			cascadingconfig.AllowFor("git", cascadingconfig.Prefix("git::https://github.com/acme/")),
		),
	)
	if err != nil {
		log.Fatal(err)
	}
	_ = loader // loader.Load fetches the includes its files name
	// Output:
}
