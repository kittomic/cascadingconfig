package cascadingconfig

// WithEnvs selects environments, later ones winning. Empty entries are dropped, and with none left, the option means
// not set, so {NAME}_ENV stays in charge. Each name must be safe in a filename.
func WithEnvs(names ...string) Option {
	_ = names // stub
	return optionFunc(func(*config) {})
}

// WithConfigFiles names explicit config files, applied in order. Empty entries are dropped, and with none left, the
// option means not set, so {NAME}_CONFIG_FILE stays in charge.
func WithConfigFiles(paths ...string) Option {
	_ = paths // stub
	return optionFunc(func(*config) {})
}

// Binding is the key of an env-var binding: the suffix of its default variable name, {NAME}_<suffix>.
type Binding string

// The built-in bindings.
const (
	BindConfigFile              Binding = "CONFIG_FILE"
	BindConfigDir               Binding = "CONFIG_DIR"
	BindGlobalConfigFile        Binding = "GLOBAL_CONFIG_FILE"
	BindSystemConfigDir         Binding = "SYSTEM_CONFIG_DIR"
	BindSystemConfigFile        Binding = "SYSTEM_CONFIG_FILE"
	BindEnv                     Binding = "ENV"
	BindCeilingPaths            Binding = "CEILING_PATHS"
	BindIgnoredConfigPaths      Binding = "IGNORED_CONFIG_PATHS"
	BindOverrideConfigFilenames Binding = "OVERRIDE_CONFIG_FILENAMES"
	BindConfigFormats           Binding = "CONFIG_FORMATS"
)

// WithEnvBinding declares a binding or renames it. An empty name means the default, {NAME}_<suffix>. The name must
// match [A-Za-z_][A-Za-z0-9_]*, and no two bindings may share one.
func WithEnvBinding(b Binding, name string) Option {
	_, _ = b, name // stub
	return optionFunc(func(*config) {})
}

// WithoutEnvBinding turns a binding off. A built-in binding that isn't declared is left alone; an unknown key is an
// *OptionError.
func WithoutEnvBinding(b Binding) Option {
	_ = b // stub
	return optionFunc(func(*config) {})
}
