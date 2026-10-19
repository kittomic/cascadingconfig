package cascadingconfig

// Loader loads configuration. It is immutable once built, and safe for concurrent use.
type Loader struct {
	cfg *config
}

// New builds a loader from the engine defaults and opts. name is the app name, {name} in every pattern.
//
// Every option is checked. All the problems found are returned together, as joined *OptionErrors.
func New(name string, opts ...Option) (*Loader, error) {
	return NewWithPreset(name, Preset{}, opts...)
}

// NewWithPreset builds a loader from a preset, then opts, so NewWithPreset(n, p, a, b) behaves exactly like
// NewWithPreset(n, p.Extend(a, b)).
func NewWithPreset(name string, preset Preset, opts ...Option) (*Loader, error) {
	_, _, _, _ = name, preset.bases, preset.opts, opts // stub
	return &Loader{cfg: &config{}}, nil
}
