package mise

import (
	"github.com/kittomic/cascadingconfig"
	"github.com/kittomic/cascadingconfig/format/toml"
	"github.com/kittomic/cascadingconfig/format/yaml"
	"github.com/kittomic/cascadingconfig/gitresolver"
	"github.com/kittomic/cascadingconfig/httpresolver"
	"github.com/kittomic/cascadingconfig/ociresolver"
	"github.com/kittomic/cascadingconfig/watch/fsnotify"
)

// Preset is cascadingconfig.MiseLayout with TOML, YAML, the http(s), git and OCI resolvers with their default options,
// and fsnotify for watching. Register a resolver again with WithIncludeResolver to change its options. Never reassign
// it.
var Preset = cascadingconfig.MiseLayout.ExtendAs("mise.Preset",
	cascadingconfig.WithDecoder(toml.Decoder()),
	cascadingconfig.WithDecoder(yaml.Decoder()),
	cascadingconfig.WithIncludeResolver(httpresolver.New()),
	cascadingconfig.WithIncludeResolver(gitresolver.New()),
	cascadingconfig.WithIncludeResolver(ociresolver.New()),
	cascadingconfig.WithNotifier(fsnotify.New),
)
