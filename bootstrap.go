package cascadingconfig

import "context"

// BootstrapFiles makes a scope's kind also search for bootstrap files, in the same places as its config files. Only
// FixedDir and Walk support it. Bootstrap files are read before the main search and change how it runs; they never
// become part of the config.
func BootstrapFiles(patterns ...Pattern) ScopeOption {
	_ = patterns // stub
	return scopeOptionFunc(func(*scopeSpec) {})
}

// BootstrapKey is a key a bootstrap file may set.
type BootstrapKey string

// The bootstrap keys.
const (
	BootstrapEnv                     BootstrapKey = "env"
	BootstrapFormats                 BootstrapKey = "formats"
	BootstrapCeilingPaths            BootstrapKey = "ceiling_paths"
	BootstrapIgnoredConfigPaths      BootstrapKey = "ignored_config_paths"
	BootstrapOverrideConfigFilenames BootstrapKey = "override_config_filenames"
)

// WithBootstrapKeys enables bootstrap keys. A bootstrap file's other keys are warned about and ignored.
func WithBootstrapKeys(keys ...BootstrapKey) Option {
	_ = keys // stub
	return optionFunc(func(*config) {})
}

// WithoutBootstrapKey turns a bootstrap key off, so bootstrap files can't change that setting.
func WithoutBootstrapKey(key BootstrapKey) Option {
	_ = key // stub
	return optionFunc(func(*config) {})
}

// WithBootstrapLimit restricts the bootstrap files of one scope to the keys listed. Other keys are dropped with a
// warning.
func WithBootstrapLimit(scope ScopeName, keys ...BootstrapKey) Option {
	_, _ = scope, keys // stub
	return optionFunc(func(*config) {})
}

// WithBootstrapTransform adds a function that runs on each bootstrap file's tree, a copy, before its keys are read. It
// can rewrite or drop keys. An error it returns is a *TransformError naming the file.
func WithBootstrapTransform(f func(ctx context.Context, l Layer, t Tree) (Tree, error)) Option {
	_ = f // stub
	return optionFunc(func(*config) {})
}
