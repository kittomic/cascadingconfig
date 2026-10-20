package cascadingconfig

import "strconv"

// ScopeName names a scope. Custom scopes use their own names, and untyped string constants convert implicitly.
type ScopeName string

// The built-in scopes.
const (
	ScopeSystem   ScopeName = "system"
	ScopeUser     ScopeName = "user"
	ScopeProject  ScopeName = "project"
	ScopeOverride ScopeName = "override"
)

// Variant says which variant of a config file a layer is.
type Variant int

// The variants, from shared to env-and-local.
const (
	VariantShared   Variant = iota // myapp.toml
	VariantEnv                     // myapp.{env}.toml
	VariantLocal                   // myapp.local.toml
	VariantEnvLocal                // myapp.{env}.local.toml
)

// String returns the variant's name: "shared", "env", "local" or "env_local".
func (v Variant) String() string {
	return enumName(int(v), "shared", "env", "local", "env_local")
}

// MarshalText returns the variant's name.
func (v Variant) MarshalText() ([]byte, error) {
	return []byte(v.String()), nil
}

// TrustLevel orders scopes by how far their files are trusted: TrustProject < TrustUser < TrustSystem.
type TrustLevel int

// The trust levels, least trusted first.
const (
	TrustProject TrustLevel = iota
	TrustUser
	TrustSystem
)

// String returns the level's name: "project", "user" or "system".
func (t TrustLevel) String() string {
	return enumName(int(t), "project", "user", "system")
}

// MarshalText returns the level's name.
func (t TrustLevel) MarshalText() ([]byte, error) {
	return []byte(t.String()), nil
}

// LayerStatus says what happened to a layer.
type LayerStatus int

// Layer statuses. Later milestones add more.
const (
	// LayerLoaded is a layer that was merged.
	LayerLoaded LayerStatus = iota
	// LayerIgnored is a layer that was found but not loaded, such as a lower-ranked file in a slot under
	// ConflictFirst, a FIFO or a broken symlink. Reason says why.
	LayerIgnored
	// LayerEmpty is a file that holds nothing: whitespace, comments only, or a null document. It merges nothing.
	LayerEmpty
	// LayerUnreadable is a searched file that couldn't be read, for example because of its permissions. Reason says
	// why.
	LayerUnreadable
	// LayerFailed is a file that broke the load, for example because it doesn't parse. Reason is built from the
	// error's fields, never from its message, so it can't quote the file's contents.
	LayerFailed
	// LayerDuplicate is another route to a file that loaded from elsewhere. Reason names the route that was kept.
	LayerDuplicate
)

// String returns the status's name: "loaded", "ignored", "empty", "unreadable", "failed" or "duplicate".
func (s LayerStatus) String() string {
	return enumName(int(s), "loaded", "ignored", "empty", "unreadable", "failed", "duplicate")
}

// MarshalText returns the status's name.
func (s LayerStatus) MarshalText() ([]byte, error) {
	return []byte(s.String()), nil
}

// enumName returns names[v], or the number for a value with no name.
func enumName(v int, names ...string) string {
	if v >= 0 && v < len(names) {
		return names[v]
	}
	return strconv.Itoa(v)
}

// Layer is the metadata for one source. It never contains the layer's tree, so it is safe to log and to keep.
// Layers are always listed lowest precedence first, matching the order they merge in.
type Layer struct {
	Path       string      `json:"path,omitempty"` // file path, or the reference as written for a remote include
	Scope      ScopeName   `json:"scope,omitempty"`
	Variant    Variant     `json:"variant"`
	Env        string      `json:"env,omitempty"`    // for VariantEnv and VariantEnvLocal
	Format     string      `json:"format,omitempty"` // "toml", "yaml", "json", …
	ConfigRoot string      `json:"config_root,omitempty"`
	Pattern    string      `json:"pattern,omitempty"` // the pattern that matched, such as ".config/{name}/config.{ext}"
	Trust      TrustLevel  `json:"trust"`
	IncludedBy string      `json:"included_by,omitempty"` // Path of the including layer, if any
	Status     LayerStatus `json:"status"`
	Reason     string      `json:"reason,omitempty"` // why the layer was skipped, if it was
	Warnings   []Warning   `json:"warnings,omitempty"`
}
