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

		// mise's global-directory layout (config-locations.md 3.4), for both the system and the user directory.
		WithScope(ScopeSystem, FixedDir("{system}",
			Pattern{Template: "conf.d/*.{ext}"},
			Pattern{Template: "conf.d/*/{name}.{ext}", Root: RootFileDir},
			Pattern{Template: "config.{ext}"},
			Pattern{Template: "{name}.{ext}"},
			Pattern{Template: "conf.d/*.{env}.{ext}", Variant: VariantEnv},
			Pattern{Template: "conf.d/*/{name}.{env}.{ext}", Variant: VariantEnv, Root: RootFileDir},
			Pattern{Template: "config.{env}.{ext}", Variant: VariantEnv},
			Pattern{Template: "{name}.{env}.{ext}", Variant: VariantEnv},
			Pattern{Template: "conf.d/*/{name}.local.{ext}", Variant: VariantLocal, Root: RootFileDir},
			Pattern{Template: "config.local.{ext}", Variant: VariantLocal},
			Pattern{Template: "{name}.local.{ext}", Variant: VariantLocal},
			Pattern{Template: "conf.d/*.{env}.local.{ext}", Variant: VariantEnvLocal},
			Pattern{Template: "conf.d/*/{name}.{env}.local.{ext}", Variant: VariantEnvLocal, Root: RootFileDir},
			Pattern{Template: "config.{env}.local.{ext}", Variant: VariantEnvLocal},
			Pattern{Template: "{name}.{env}.local.{ext}", Variant: VariantEnvLocal},
		), Trust(TrustSystem), ReplacedByFile(BindSystemConfigFile)),
		WithScope(ScopeUser, FixedDir("{user}",
			Pattern{Template: "conf.d/*.{ext}"},
			Pattern{Template: "conf.d/*/{name}.{ext}", Root: RootFileDir},
			Pattern{Template: "config.{ext}"},
			Pattern{Template: "{name}.{ext}"},
			Pattern{Template: "conf.d/*.{env}.{ext}", Variant: VariantEnv},
			Pattern{Template: "conf.d/*/{name}.{env}.{ext}", Variant: VariantEnv, Root: RootFileDir},
			Pattern{Template: "config.{env}.{ext}", Variant: VariantEnv},
			Pattern{Template: "{name}.{env}.{ext}", Variant: VariantEnv},
			Pattern{Template: "conf.d/*/{name}.local.{ext}", Variant: VariantLocal, Root: RootFileDir},
			Pattern{Template: "config.local.{ext}", Variant: VariantLocal},
			Pattern{Template: "{name}.local.{ext}", Variant: VariantLocal},
			Pattern{Template: "conf.d/*.{env}.local.{ext}", Variant: VariantEnvLocal},
			Pattern{Template: "conf.d/*/{name}.{env}.local.{ext}", Variant: VariantEnvLocal, Root: RootFileDir},
			Pattern{Template: "config.{env}.local.{ext}", Variant: VariantEnvLocal},
			Pattern{Template: "{name}.{env}.local.{ext}", Variant: VariantEnvLocal},
		), Trust(TrustUser), ConfigRoot("~"), ReplacedByFile(BindGlobalConfigFile)),

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
		), ReplacedByConfigFiles()),

		WithScope(ScopeOverride, FixedDir("{user}",
			Pattern{Template: "config.override.{ext}"},
			Pattern{Template: "config.{env}.override.{ext}", Variant: VariantEnv},
		), Trust(TrustUser), ConfigRoot("~"), DisabledBy(BindGlobalConfigFile)),

		WithScopes(ScopeSystem, ScopeUser, ScopeProject, ScopeOverride),
	)

	// MiseHardenedLayout is MiseLayout with hardened settings.
	MiseHardenedLayout = MiseLayout.ExtendAs("MiseHardenedLayout")

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
	return nil // stub
}
