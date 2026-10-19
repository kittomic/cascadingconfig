package cascadingconfig

// Preset is a name, the names of the presets it extends, and an ordered list of options. Nothing is applied until New
// or NewWithPreset, which run the preset's options, then New's own, in order, onto a fresh configuration.
//
// Layouts and ready-to-use presets are both Presets. A zero Preset has no name and no options, so it is the same as no
// preset.
type Preset struct {
	name  string
	bases []string // the presets this one extends, nearest first
	opts  []Option
}

// NewPreset creates a preset from nothing.
func NewPreset(name string, opts ...Option) Preset {
	_, _ = name, opts // stub
	return Preset{}
}

// Extend returns a new preset with opts added after p's own options. It keeps p's name, and never changes p.
func (p Preset) Extend(opts ...Option) Preset {
	_ = opts // stub
	return p
}

// ExtendAs returns a new preset named name, with opts added after p's own options, and records p as its base. It never
// changes p.
func (p Preset) ExtendAs(name string, opts ...Option) Preset {
	_, _ = name, opts // stub
	return p
}

// Name returns the preset's name, as Describe shows it.
func (p Preset) Name() string {
	return p.name
}
