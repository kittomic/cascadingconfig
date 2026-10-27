package misehardened

import (
	"github.com/kittomic/cascadingconfig"
	"github.com/kittomic/cascadingconfig/format/toml"
	"github.com/kittomic/cascadingconfig/format/yaml"
	"github.com/kittomic/cascadingconfig/watch/fsnotify"
)

// Preset is cascadingconfig.MiseHardenedLayout with TOML, YAML and fsnotify for watching, and no remote resolvers.
// Never reassign it.
var Preset = cascadingconfig.MiseHardenedLayout.ExtendAs("misehardened.Preset",
	cascadingconfig.WithDecoder(toml.Decoder()),
	cascadingconfig.WithDecoder(yaml.Decoder()),
	cascadingconfig.WithNotifier(fsnotify.New),
)
