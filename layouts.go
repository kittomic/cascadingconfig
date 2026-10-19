package cascadingconfig

// The core layouts. Each is built only from exported options, so an app's own layout can do everything they do
// (DL-71). They gain their scopes, bootstrap files, includes and policies in the milestones that implement them
// (DL-94). Never reassign them.
var (
	// MiseLayout is mise's layout: TOML first, then YAML, then JSON, with mise's env-var bindings. JSON is registered.
	MiseLayout = NewPreset("MiseLayout",
		WithDecoder(builtinJSONDecoder),
		WithDefaultFormats("toml", "yaml", "json"),
		WithEnvBinding(BindConfigFile, ""),
		WithEnvBinding(BindConfigDir, ""),
		WithEnvBinding(BindGlobalConfigFile, ""),
		WithEnvBinding(BindSystemConfigDir, ""),
		WithEnvBinding(BindSystemConfigFile, ""),
		WithEnvBinding(BindEnv, ""),
		WithEnvBinding(BindCeilingPaths, ""),
		WithEnvBinding(BindIgnoredConfigPaths, ""),
		WithEnvBinding(BindOverrideConfigFilenames, ""),
		WithEnvBinding(BindConfigFormats, ""),
	)

	// MiseHardenedLayout is MiseLayout with hardened settings.
	MiseHardenedLayout = MiseLayout.ExtendAs("MiseHardenedLayout")

	// CosmiconfigLayout is cosmiconfig's layout: JSON, then YAML; the first file in a slot; arrays appended.
	CosmiconfigLayout = NewPreset("CosmiconfigLayout",
		WithDecoder(builtinJSONDecoder),
		WithDefaultFormats("json", "yaml"),
		WithFormatConflict(ConflictFirst),
		WithArrayMerge(Append),
		WithEnvBinding(BindConfigFile, ""),
		WithEnvBinding(BindConfigFormats, ""),
	)
)
