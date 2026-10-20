package cascadingconfig

import (
	"context"
	"io/fs"
)

// WithDefaultPolicy sets the policy for every scope without one of its own (DL-188). The last call wins.
func WithDefaultPolicy(opts ...PolicyOption) Option {
	_ = opts // stub
	return optionFunc(func(*config) {})
}

// WithScopePolicy sets scope's policy, which replaces the default as a whole; InheritDefault starts from the default
// instead (DL-188). The last call for a scope wins. A scope that is never declared is an *OptionError (DL-189).
func WithScopePolicy(scope ScopeName, opts ...PolicyOption) Option {
	_, _ = scope, opts // stub
	return optionFunc(func(*config) {})
}

type policyFunc func()

func (policyFunc) policyOption() {}

// InheritDefault starts the policy from the default policy, wherever it appears in the list.
func InheritDefault() PolicyOption {
	return policyFunc(func() {}) // stub
}

// AllowResolvers sets the schemes whose resolvers the policy can use. With no schemes, every include is forbidden. A
// scheme with no registered resolver is an *OptionError (DL-189).
func AllowResolvers(schemes ...string) PolicyOption {
	_ = schemes // stub
	return policyFunc(func() {})
}

// Allow sets the allow-list for every scheme: a reference must match at least one of the matchers.
func Allow(m ...Matcher) PolicyOption {
	_ = m // stub
	return policyFunc(func() {})
}

// AllowFor sets scheme's allow-list. A reference must match it as well as the list Allow sets.
func AllowFor(scheme string, m ...Matcher) PolicyOption {
	_, _ = scheme, m // stub
	return policyFunc(func() {})
}

// Confine requires a local include to resolve, after symlinks, to one of roots or below it (DL-180). Roots are
// absolute paths, or start with ~, {user}, {system}, {cwd} or {config_root}, the including file's config root.
func Confine(roots ...string) PolicyOption {
	_ = roots // stub
	return policyFunc(func() {})
}

// RequireTrust skips files the store hasn't approved, with a warning (DL-192).
func RequireTrust(store TrustStore) PolicyOption {
	_ = store // stub
	return policyFunc(func() {})
}

// FilePermissions sets the check that every file read, and its containing directory, must pass (DL-191).
func FilePermissions(f func(FileCheck) (Permission, string)) PolicyOption {
	_ = f // stub
	return policyFunc(func() {})
}

// FileCheck is what a FilePermissions check gets.
type FileCheck struct {
	Path  string
	Info  fs.FileInfo // implements OwnerInfo where the platform has owners
	Dir   bool        // the file's containing directory, not the file
	Scope ScopeName
	UID   int    // the current user
	GOOS  string // the host's, so a check can tell Windows apart
}

// Permission is a FilePermissions check's verdict.
type Permission int

const (
	// PermissionAllow reads the file.
	PermissionAllow Permission = iota
	// PermissionIgnore skips a searched file with a warning; a file named directly fails with a *PolicyError.
	PermissionIgnore
	// PermissionError fails the load with a *PolicyError.
	PermissionError
)

// StrictModes is SSH-style checking: it ignores a file or directory writable by group or other, except a directory with
// the sticky bit, or owned by neither the current user nor root. An unknown owner, and everything on Windows, is
// allowed.
func StrictModes(c FileCheck) (Permission, string) {
	_ = c // stub
	return PermissionAllow, ""
}

// TrustStore says whether a file has been approved.
type TrustStore interface {
	// Trusted reports whether path (symlinks resolved) has been approved. digest is the lowercase hex SHA-256 of the
	// file's raw bytes, so a store can require re-approval after the file changes.
	Trusted(ctx context.Context, path, digest string) (bool, error)
}

// TrustPaths trusts the files that globs match, as Glob matches, after resolving symlinks and expanding ~. An entry
// without wildcards trusts that path and everything beneath it, as mise's trusted_config_paths does.
func TrustPaths(globs ...string) TrustStore {
	_ = globs // stub
	return trustPaths{}
}

type trustPaths struct{}

func (trustPaths) Trusted(context.Context, string, string) (bool, error) {
	return false, nil // stub
}
