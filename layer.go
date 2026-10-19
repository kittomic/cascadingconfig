package cascadingconfig

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

// TrustLevel orders scopes by how far their files are trusted: TrustProject < TrustUser < TrustSystem.
type TrustLevel int

// The trust levels, least trusted first.
const (
	TrustProject TrustLevel = iota
	TrustUser
	TrustSystem
)

// LayerStatus says what happened to a layer.
type LayerStatus int

// Layer statuses. Later milestones add more.
const (
	// LayerLoaded is a layer that was merged.
	LayerLoaded LayerStatus = iota
	// LayerIgnored is a layer that was found but not loaded, such as a lower-ranked file in a slot under
	// ConflictFirst. Reason says why.
	LayerIgnored
)

// Layer is the metadata for one source. It never contains the layer's tree, so it is safe to log and to keep.
// Layers are always listed lowest precedence first, matching the order they merge in.
type Layer struct {
	Path       string // file path, or the reference as written for a remote include
	Scope      ScopeName
	Variant    Variant
	Env        string // for VariantEnv and VariantEnvLocal
	Format     string // "toml", "yaml", "json", …
	ConfigRoot string
	Pattern    string // the pattern that matched, such as ".config/{name}/config.{ext}"
	Trust      TrustLevel
	IncludedBy string // Path of the including layer, if any
	Status     LayerStatus
	Reason     string // why the layer was skipped, if it was
	Warnings   []Warning
}
