package cascadingconfig

import (
	"context"
	"io/fs"
)

// Pattern is one file a scope looks for. Later milestones add fields; their zero values keep today's meaning.
type Pattern struct {
	// Template is a relative file path that may use {name} and {ext}, and may include subdirectories. {ext} stands for
	// each extension of the searched formats, so one template can match several files: a slot.
	Template string
}

// FixedDir is a scope kind that searches one directory. dir is absolute, or starts with ~, {user} or {system}, and may
// use {name}. Patterns are listed lowest precedence first. Its default trust is TrustProject.
func FixedDir(dir string, patterns ...Pattern) ScopeKind {
	_, _ = dir, patterns // stub
	return stubKind{}
}

// EmbeddedFS is a scope kind that searches the root of fsys, such as an embed.FS compiled into the app. Its default
// trust is TrustProject.
func EmbeddedFS(fsys fs.FS, patterns ...Pattern) ScopeKind {
	_, _ = fsys, patterns // stub
	return stubKind{}
}

// ValuesFunc is a scope kind for a tree computed on every load, so a reload can re-read it. An error from f, or a
// value outside the canonical types, is a *BaseError with CodeInternal. Its default trust is TrustUser.
func ValuesFunc(f func(ctx context.Context) (Tree, error)) ScopeKind {
	_ = f // stub
	return stubKind{}
}

// EnvVarsOption configures EnvVars.
type EnvVarsOption interface {
	applyEnvVarsOption()
}

// EnvVars is a scope kind for single values from environment variables. The prefix is stripped, the name is
// lowercased, and __ separates nesting levels, so MYAPP_SERVER__PORT gives server.port. Every value is a string unless
// MapEnv maps it. Variables bound to discovery settings are never read. Its default trust is TrustUser.
func EnvVars(prefix string, opts ...EnvVarsOption) ScopeKind {
	_, _ = prefix, opts // stub
	return stubKind{}
}

// MapEnv replaces EnvVars' mapping. f gets each variable's full name and value, and returns the key path and value, or
// false to skip the variable.
func MapEnv(f func(name, value string) (Path, any, bool)) EnvVarsOption {
	_ = f // stub
	return nil
}

// stubKind stands in for the kinds until they are implemented.
type stubKind struct{}

func (stubKind) defaultTrust() TrustLevel { return TrustProject }
