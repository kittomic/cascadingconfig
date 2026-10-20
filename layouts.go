package cascadingconfig

// The core layouts. Each is built only from exported options, so an app's own layout can do everything they do
// (DL-71). They gain their bootstrap files, includes and policies in the milestones that implement them (DL-94).
// Never reassign them.
var (
	// MiseLayout is mise's layout: the system, user, project and override scopes with mise's file names (DL-124), TOML
	// first, then YAML, then JSON, and mise's env-var bindings. JSON is registered.
	MiseLayout = NewPreset("MiseLayout",
		WithDecoder(builtinJSONDecoder),
		WithDefaultFormats("toml", "yaml", "json"),
		WithPlatformDirs(XDGDirs),
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
		WithBootstrapKeys(BootstrapEnv, BootstrapFormats, BootstrapCeilingPaths, BootstrapIgnoredConfigPaths,
			BootstrapOverrideConfigFilenames),

		// Bootstrap files are mise's .miserc.toml locations (config-locations.md 3.7), lowest precedence first.
		WithScope(ScopeSystem, FixedDir("{system}", MiseGlobalPatterns()...),
			Trust(TrustSystem), ReplacedByFile(BindSystemConfigFile),
			BootstrapFiles(Pattern{Template: "{name}rc.{ext}"})),
		WithScope(ScopeUser, FixedDir("{user}", MiseGlobalPatterns()...),
			Trust(TrustUser), ConfigRoot("~"), ReplacedByFile(BindGlobalConfigFile),
			BootstrapFiles(Pattern{Template: "{name}rc.{ext}"}, Pattern{Template: "{name}rc.local.{ext}"})),

		// mise's layers A to D within one project directory (config-locations.md 3.2), without .tool-versions.
		WithScope(ScopeProject, Walk(
			Pattern{Template: ".config/{name}/conf.d/*.{ext}"},
			Pattern{Template: ".config/{name}/conf.d/*/{name}.{ext}", Root: RootFileDir},
			Pattern{Template: ".config/{name}/config.{ext}"},
			Pattern{Template: ".config/{name}/{name}.{ext}", Root: RootFileDir},
			Pattern{Template: ".config/{name}.{ext}"},
			Pattern{Template: ".{name}/conf.d/*.{ext}"},
			Pattern{Template: ".{name}/conf.d/*/{name}.{ext}", Root: RootFileDir},
			Pattern{Template: ".{name}/config.{ext}"},
			Pattern{Template: "{name}/conf.d/*.{ext}"},
			Pattern{Template: "{name}/conf.d/*/{name}.{ext}", Root: RootFileDir},
			Pattern{Template: "{name}/config.{ext}"},
			Pattern{Template: "{name}.{ext}"},
			Pattern{Template: ".{name}.{ext}"},
			Pattern{Template: ".config/{name}/conf.d/*.{env}.{ext}", Variant: VariantEnv},
			Pattern{Template: ".config/{name}/conf.d/*/{name}.{env}.{ext}", Variant: VariantEnv, Root: RootFileDir},
			Pattern{Template: ".config/{name}/config.{env}.{ext}", Variant: VariantEnv},
			Pattern{Template: ".config/{name}.{env}.{ext}", Variant: VariantEnv},
			Pattern{Template: "{name}/conf.d/*.{env}.{ext}", Variant: VariantEnv},
			Pattern{Template: "{name}/conf.d/*/{name}.{env}.{ext}", Variant: VariantEnv, Root: RootFileDir},
			Pattern{Template: "{name}/config.{env}.{ext}", Variant: VariantEnv},
			Pattern{Template: "{name}.{env}.{ext}", Variant: VariantEnv},
			Pattern{Template: ".{name}/conf.d/*.{env}.{ext}", Variant: VariantEnv},
			Pattern{Template: ".{name}/conf.d/*/{name}.{env}.{ext}", Variant: VariantEnv, Root: RootFileDir},
			Pattern{Template: ".{name}/config.{env}.{ext}", Variant: VariantEnv},
			Pattern{Template: ".{name}.{env}.{ext}", Variant: VariantEnv},
			Pattern{Template: ".config/{name}/conf.d/*/{name}.local.{ext}", Variant: VariantLocal, Root: RootFileDir},
			Pattern{Template: ".config/{name}/config.local.{ext}", Variant: VariantLocal},
			Pattern{Template: ".config/{name}/{name}.local.{ext}", Variant: VariantLocal, Root: RootFileDir},
			Pattern{Template: ".config/{name}.local.{ext}", Variant: VariantLocal},
			Pattern{Template: ".{name}/conf.d/*/{name}.local.{ext}", Variant: VariantLocal, Root: RootFileDir},
			Pattern{Template: ".{name}/config.local.{ext}", Variant: VariantLocal},
			Pattern{Template: "{name}/conf.d/*/{name}.local.{ext}", Variant: VariantLocal, Root: RootFileDir},
			Pattern{Template: "{name}/config.local.{ext}", Variant: VariantLocal},
			Pattern{Template: "{name}.local.{ext}", Variant: VariantLocal},
			Pattern{Template: ".{name}.local.{ext}", Variant: VariantLocal},
			Pattern{Template: ".config/{name}/conf.d/*.{env}.local.{ext}", Variant: VariantEnvLocal},
			Pattern{Template: ".config/{name}/conf.d/*/{name}.{env}.local.{ext}", Variant: VariantEnvLocal, Root: RootFileDir},
			Pattern{Template: ".config/{name}/config.{env}.local.{ext}", Variant: VariantEnvLocal},
			Pattern{Template: ".config/{name}.{env}.local.{ext}", Variant: VariantEnvLocal},
			Pattern{Template: "{name}/conf.d/*.{env}.local.{ext}", Variant: VariantEnvLocal},
			Pattern{Template: "{name}/conf.d/*/{name}.{env}.local.{ext}", Variant: VariantEnvLocal, Root: RootFileDir},
			Pattern{Template: "{name}/config.{env}.local.{ext}", Variant: VariantEnvLocal},
			Pattern{Template: "{name}.{env}.local.{ext}", Variant: VariantEnvLocal},
			Pattern{Template: ".{name}/conf.d/*.{env}.local.{ext}", Variant: VariantEnvLocal},
			Pattern{Template: ".{name}/conf.d/*/{name}.{env}.local.{ext}", Variant: VariantEnvLocal, Root: RootFileDir},
			Pattern{Template: ".{name}/config.{env}.local.{ext}", Variant: VariantEnvLocal},
			Pattern{Template: ".{name}.{env}.local.{ext}", Variant: VariantEnvLocal},
		), ReplacedByConfigFiles(), BootstrapFiles(
			Pattern{Template: ".config/{name}rc.{ext}", SkipHomeAndRoot: true},
			Pattern{Template: ".{name}rc.{ext}"},
			Pattern{Template: ".{name}rc.local.{ext}"},
		)),

		WithScope(ScopeOverride, FixedDir("{user}",
			Pattern{Template: "config.override.{ext}"},
			Pattern{Template: "config.{env}.override.{ext}", Variant: VariantEnv},
		), Trust(TrustUser), ConfigRoot("~"), DisabledBy(BindGlobalConfigFile)),

		WithScopes(ScopeSystem, ScopeUser, ScopeProject, ScopeOverride),
	)

	// MiseHardenedLayout is MiseLayout with hardened settings: project bootstrap files may only set env and formats.
	MiseHardenedLayout = MiseLayout.ExtendAs("MiseHardenedLayout",
		WithBootstrapLimit(ScopeProject, BootstrapEnv, BootstrapFormats),
	)

	// CosmiconfigLayout is cosmiconfig's layout: JSON, then YAML; the first file in a slot; arrays appended. Its scopes
	// arrive in M4d.
	CosmiconfigLayout = NewPreset("CosmiconfigLayout",
		WithDecoder(builtinJSONDecoder),
		WithDefaultFormats("json", "yaml"),
		WithFormatConflict(ConflictFirst),
		WithArrayMerge(Append),
		WithEnvBinding(BindConfigFile, ""),
		WithEnvBinding(BindConfigFormats, ""),
	)
)

// MiseGlobalPatterns returns mise's global-directory patterns (config-locations.md 3.4), lowest precedence first, as
// MiseLayout's system and user scopes use them. Apps can use them for their own scopes, such as a mounted config
// directory. Each call returns a fresh copy.
func MiseGlobalPatterns() []Pattern {
	return []Pattern{
		{Template: "conf.d/*.{ext}"},
		{Template: "conf.d/*/{name}.{ext}", Root: RootFileDir},
		{Template: "config.{ext}"},
		{Template: "{name}.{ext}"},
		{Template: "conf.d/*.{env}.{ext}", Variant: VariantEnv},
		{Template: "conf.d/*/{name}.{env}.{ext}", Variant: VariantEnv, Root: RootFileDir},
		{Template: "config.{env}.{ext}", Variant: VariantEnv},
		{Template: "{name}.{env}.{ext}", Variant: VariantEnv},
		{Template: "conf.d/*/{name}.local.{ext}", Variant: VariantLocal, Root: RootFileDir},
		{Template: "config.local.{ext}", Variant: VariantLocal},
		{Template: "{name}.local.{ext}", Variant: VariantLocal},
		{Template: "conf.d/*.{env}.local.{ext}", Variant: VariantEnvLocal},
		{Template: "conf.d/*/{name}.{env}.local.{ext}", Variant: VariantEnvLocal, Root: RootFileDir},
		{Template: "config.{env}.local.{ext}", Variant: VariantEnvLocal},
		{Template: "{name}.{env}.local.{ext}", Variant: VariantEnvLocal},
	}
}
