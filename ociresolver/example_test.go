package ociresolver_test

import (
	"context"
	"log"
	"os"

	"github.com/kittomic/cascadingconfig"
	"github.com/kittomic/cascadingconfig/ociresolver"
)

// A layout from the core module fetches no remote includes until a resolver is registered. Here the resolver sends a
// token to one registry, and a policy requires every reference to name its digest, as in
// oci::ghcr.io/acme/myapp-config@sha256:<hex>.
func ExampleNew() {
	creds := func(_ context.Context, registry string) (ociresolver.Credential, error) {
		if registry == "ghcr.io" {
			return ociresolver.Credential{Username: "acme-ci", Password: os.Getenv("GHCR_TOKEN")}, nil
		}
		return ociresolver.Credential{}, nil // anonymous
	}
	loader, err := cascadingconfig.NewWithPreset("myapp", cascadingconfig.MiseLayout,
		cascadingconfig.WithIncludeResolver(ociresolver.New(ociresolver.WithCredentials(creds))),
		cascadingconfig.WithDefaultPolicy(cascadingconfig.RequirePinned()),
	)
	if err != nil {
		log.Fatal(err)
	}
	_ = loader // loader.Load fetches the includes its files name
	// Output:
}
