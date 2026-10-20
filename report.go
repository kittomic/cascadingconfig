package cascadingconfig

import "errors"

// Report describes a load: its settings, every layer it found, loaded or skipped, and its warnings. It never contains
// config values, so it is safe to print and to log.
type Report struct {
	Preset    string        `json:"preset,omitempty"`    // the name of the preset given to NewWithPreset
	Scopes    []ScopeInfo   `json:"scopes"`              // selected and unselected, lowest precedence first
	Envs      EnvInfo       `json:"envs"`                // selected and platform environments
	Formats   []string      `json:"formats"`             // effective format order
	Bootstrap []Layer       `json:"bootstrap,omitempty"` // the bootstrap files read, lowest precedence first
	Layers    []Layer       `json:"layers"`              // loaded and skipped, lowest precedence first
	Probes    []Probe       `json:"probes,omitempty"`    // locations searched where nothing was found, with WithProbes
	Bindings  []BindingInfo `json:"bindings,omitempty"`  // every declared binding
	Warnings  []Warning     `json:"warnings,omitempty"`  // warnings that belong to no layer, and unknown keys
}

// ScopeInfo describes one defined scope.
type ScopeInfo struct {
	Name     ScopeName  `json:"name"`
	Kind     string     `json:"kind"` // "FixedDir", "Walk", "ExplicitFile", "EmbeddedFS", "Values", "ValuesFunc", "EnvVars", "Defaults" or "Scope"
	Trust    TrustLevel `json:"trust"`
	Selected bool       `json:"selected"`
	Skipped  string     `json:"skipped,omitempty"` // why a selected scope wasn't searched
}

// EnvInfo describes the environments of a load.
type EnvInfo struct {
	Selected []string `json:"selected,omitempty"` // lowest precedence first
	Source   string   `json:"source,omitempty"`   // "WithEnvs", the variable's name, or the bootstrap file's path
	Platform []string `json:"platform,omitempty"` // lowest precedence first
}

// Probe is a location searched where nothing was found.
type Probe struct {
	Scope   ScopeName `json:"scope"`
	Dir     string    `json:"dir"`
	Pattern string    `json:"pattern"` // the pattern searched, with {env} filled in
}

// BindingInfo describes one declared env-var binding. It says whether the variable is set, never its value.
type BindingInfo struct {
	Binding Binding `json:"binding"`
	Var     string  `json:"var"`
	Set     bool    `json:"set"`
}

// Describe returns the load's report. Unknown keys found by decodes through r so far are in its warnings.
func (r *Result) Describe() *Report {
	return &Report{} // stub
}

// String returns the report as text, like mise config ls: the settings, the scopes, then the bootstrap files and
// layers grouped by scope, lowest first, then the bindings and warnings.
func (r *Report) String() string {
	return "" // stub
}

// MarshalJSON encodes the report with snake_case field names and enums as strings.
func (r *Report) MarshalJSON() ([]byte, error) {
	return []byte("{}"), nil // stub
}

// WithProbes lists in the report every location searched where nothing was found.
func WithProbes() Option {
	return optionFunc(func(*config) {}) // stub
}

// KoanfProvider returns a koanf.Provider for l, by method set alone: each Read runs a load. It lets an app use this
// package's discovery and merging and keep its koanf decoding.
func (l *Loader) KoanfProvider() *KoanfProvider {
	return &KoanfProvider{loader: l}
}

// KoanfProvider is a koanf.Provider that loads from a Loader.
type KoanfProvider struct {
	loader *Loader
}

// Read runs a load with context.Background() and returns a copy of the merged tree, or the load's error.
func (p *KoanfProvider) Read() (map[string]any, error) {
	return nil, nil // stub
}

// ReadBytes returns an error: a merged tree has no single byte form.
func (p *KoanfProvider) ReadBytes() ([]byte, error) {
	return nil, errors.New("cascadingconfig: KoanfProvider has no byte form; use Read") // stub
}
