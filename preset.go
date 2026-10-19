package cascadingconfig

import "slices"

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
	return Preset{name: name, opts: slices.Clone(opts)}
}

// Extend returns a new preset with opts added after p's own options. It keeps p's name, and never changes p.
func (p Preset) Extend(opts ...Option) Preset {
	return Preset{name: p.name, bases: slices.Clone(p.bases), opts: slices.Concat(p.opts, opts)}
}

// ExtendAs returns a new preset named name, with opts added after p's own options, and records p as its base. It never
// changes p.
func (p Preset) ExtendAs(name string, opts ...Option) Preset {
	bases := slices.Clone(p.bases)
	if p.name != "" {
		bases = slices.Insert(bases, 0, p.name)
	}
	return Preset{name: name, bases: bases, opts: slices.Concat(p.opts, opts)}
}

// Name returns the preset's name, as Describe shows it.
func (p Preset) Name() string {
	return p.name
}

// lineage returns the preset's name and its bases, nearest first, or nothing for an unnamed preset with no bases.
func (p Preset) lineage() []string {
	if p.name == "" {
		return slices.Clone(p.bases)
	}
	return slices.Insert(slices.Clone(p.bases), 0, p.name)
}
