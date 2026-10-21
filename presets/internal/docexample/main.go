// Command docexample is api.md's full example, unchanged but for its imports (DL-250). It's compiled, never run.
package main

import (
	"log"
	"time"

	"github.com/kittomic/cascadingconfig"
	"github.com/kittomic/cascadingconfig/presets/mise"
)

var appPreset = mise.Preset.Extend(
	cascadingconfig.WithScopes(cascadingconfig.ScopeUser, cascadingconfig.ScopeProject),
	cascadingconfig.WithMergeRule("plugins.*", cascadingconfig.Replace),
	cascadingconfig.WithScopePolicy(cascadingconfig.ScopeProject,
		cascadingconfig.InheritDefault(),
		cascadingconfig.AllowResolvers("file"),
	),
)

func main() {
	loader, err := cascadingconfig.NewWithPreset("myapp", appPreset,
		cascadingconfig.WithConfigFiles(flagConfigFiles...),
		cascadingconfig.WithEnvs(flagEnvs...),
	)
	if err != nil {
		log.Fatal(err)
	}
	cfg, err := cascadingconfig.Load[Config](ctx, loader)
	if err != nil {
		log.Fatal(err)
	}
	run(cfg)
	w, err := cascadingconfig.Watch[Config](ctx, loader, onChange,
		cascadingconfig.WithDebounce(200*time.Millisecond, 2*time.Second),
	)
	if err != nil {
		log.Fatal(err)
	}
	defer w.Close()
}
