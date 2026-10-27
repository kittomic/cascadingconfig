package cosmiconfig

import (
	"github.com/kittomic/cascadingconfig"
	"github.com/kittomic/cascadingconfig/format/yaml"
	"github.com/kittomic/cascadingconfig/watch/fsnotify"
)

// Preset is cascadingconfig.CosmiconfigLayout with YAML and fsnotify for watching. Like cosmiconfig, it has no TOML and
// no remote includes. Never reassign it.
var Preset = cascadingconfig.CosmiconfigLayout.ExtendAs("cosmiconfig.Preset",
	cascadingconfig.WithDecoder(yaml.Decoder()),
	cascadingconfig.WithNotifier(fsnotify.New),
)
