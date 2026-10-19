package cascadingconfig

// The core layouts. Each is built only from exported options, so an app's own layout can do everything they do
// (DL-71). They gain their scopes, bootstrap files, includes and policies in the milestones that implement them
// (DL-94). Never reassign them.
var (
	// MiseLayout is mise's layout: TOML first, then YAML, then JSON, with mise's env-var bindings. JSON is registered.
	MiseLayout = Preset{}

	// MiseHardenedLayout is MiseLayout with hardened settings.
	MiseHardenedLayout = Preset{}

	// CosmiconfigLayout is cosmiconfig's layout: JSON, then YAML; the first file in a slot; arrays appended.
	CosmiconfigLayout = Preset{}
)
