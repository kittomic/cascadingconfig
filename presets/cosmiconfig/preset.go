package cosmiconfig

import "github.com/kittomic/cascadingconfig"

// Preset is the ready-to-use preset (DL-248). Never reassign it.
var Preset = cascadingconfig.NewPreset("cosmiconfig.Preset") // stub: no layout or capabilities yet
