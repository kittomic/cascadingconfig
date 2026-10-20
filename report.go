package cascadingconfig

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"

	"github.com/kittomic/cascadingconfig/internal/tree"
)

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
	if r == nil || r.report == nil {
		return &Report{}
	}
	rep := r.report.clone()
	r.mu.Lock()
	rep.Warnings = append(rep.Warnings, cloneWarnings(r.unknownKeys)...)
	r.mu.Unlock()
	return rep
}

// report builds the load's report from what it found.
func (ld *loading) report() *Report {
	c := ld.c
	rep := &Report{Scopes: []ScopeInfo{}, Formats: []string{}, Layers: []Layer{}, Probes: ld.probes}
	if len(c.presets) > 0 {
		rep.Preset = c.presets[0]
	}
	for _, s := range c.scopes {
		rep.Scopes = append(rep.Scopes, ScopeInfo{
			Name: s.name, Kind: scopeKindName(s.kind), Trust: s.trust, Selected: c.selected[s.name], Skipped: ld.skipped[s.name],
		})
	}
	rep.Envs = EnvInfo{Selected: ld.selectedEnvs, Platform: ld.platformEnvs}
	if len(ld.selectedEnvs) > 0 {
		rep.Envs.Source = ld.envSource
	}
	for _, d := range ld.baseOrder {
		rep.Formats = append(rep.Formats, d.Format())
	}
	for _, e := range ld.boot {
		rep.Bootstrap = append(rep.Bootstrap, e.layer)
	}
	for _, e := range ld.entries {
		if e.dropped || e.err != nil && e.layer.Status != LayerFailed {
			continue
		}
		rep.Layers = append(rep.Layers, e.layer)
	}
	for _, b := range c.bindings {
		v := c.vars[b.key]
		val, ok := c.host.LookupEnv(v)
		rep.Bindings = append(rep.Bindings, BindingInfo{Binding: b.key, Var: v, Set: ok && val != ""})
	}
	slices.SortFunc(rep.Bindings, func(a, b BindingInfo) int { return cmp.Compare(a.Var, b.Var) })
	rep.Warnings = cloneWarnings(ld.warnings)
	return rep.clone()
}

// scopeKindName names a scope's kind for the report.
func scopeKindName(k ScopeKind) string {
	switch k.(type) {
	case fixedDirKind:
		return "FixedDir"
	case walkKind:
		return "Walk"
	case explicitKind:
		return "ExplicitFile"
	case embeddedKind:
		return "EmbeddedFS"
	case valuesKind:
		return "Values"
	case valuesFuncKind:
		return "ValuesFunc"
	case envVarsKind:
		return "EnvVars"
	case defaultsKind:
		return "Defaults"
	default:
		return "Scope"
	}
}

// clone copies the report, so a caller can change what it gets.
func (r *Report) clone() *Report {
	out := *r
	out.Scopes = slices.Clone(r.Scopes)
	out.Envs.Selected = slices.Clone(r.Envs.Selected)
	out.Envs.Platform = slices.Clone(r.Envs.Platform)
	out.Formats = slices.Clone(r.Formats)
	out.Bootstrap = cloneLayers(r.Bootstrap)
	out.Layers = cloneLayers(r.Layers)
	out.Probes = slices.Clone(r.Probes)
	out.Bindings = slices.Clone(r.Bindings)
	out.Warnings = cloneWarnings(r.Warnings)
	return &out
}

func cloneLayers(ls []Layer) []Layer {
	if ls == nil {
		return nil
	}
	out := make([]Layer, len(ls))
	for i, l := range ls {
		l.Warnings = cloneWarnings(l.Warnings)
		out[i] = l
	}
	return out
}

func cloneWarnings(ws []Warning) []Warning {
	if ws == nil {
		return nil
	}
	out := make([]Warning, len(ws))
	for i, w := range ws {
		w.Key = slices.Clone(w.Key)
		out[i] = w
	}
	return out
}

// String returns the report as text, like mise config ls: the settings, the scopes, then the bootstrap files and
// layers grouped by scope, lowest first, then the bindings and warnings.
func (r *Report) String() string {
	var b strings.Builder
	var header [][]string
	if r.Preset != "" {
		header = append(header, []string{"preset", r.Preset})
	}
	if len(r.Formats) > 0 {
		header = append(header, []string{"formats", strings.Join(r.Formats, ", ")})
	}
	if len(r.Envs.Selected) > 0 {
		envs := strings.Join(r.Envs.Selected, ", ")
		if r.Envs.Source != "" {
			envs += " (from " + r.Envs.Source + ")"
		}
		header = append(header, []string{"envs", envs})
	}
	if len(r.Envs.Platform) > 0 {
		header = append(header, []string{"platform", strings.Join(r.Envs.Platform, ", ")})
	}
	writeTable(&b, "", header)

	section := func(title string, rows [][]string) {
		if len(rows) == 0 {
			return
		}
		if b.Len() > 0 {
			b.WriteString("\n")
		}
		b.WriteString(title + ":\n")
		writeTable(&b, "  ", rows)
	}
	var scopes [][]string
	for _, s := range r.Scopes {
		state := "not selected"
		switch {
		case s.Skipped != "":
			state = "skipped: " + s.Skipped
		case s.Selected:
			state = "selected"
		}
		scopes = append(scopes, []string{string(s.Name), s.Kind, s.Trust.String(), state})
	}
	section("scopes", scopes)
	section("bootstrap", layerRows(r.Bootstrap))
	section("layers", layerRows(r.Layers))
	var probes [][]string
	for _, p := range r.Probes {
		probes = append(probes, []string{string(p.Scope), p.Dir, p.Pattern})
	}
	section("probes", probes)
	var bindings [][]string
	for _, bi := range r.Bindings {
		set := "not set"
		if bi.Set {
			set = "set"
		}
		bindings = append(bindings, []string{bi.Var, string(bi.Binding), set})
	}
	section("bindings", bindings)
	var warnings [][]string
	for _, ls := range [][]Layer{r.Bootstrap, r.Layers} {
		for _, l := range ls {
			for _, w := range l.Warnings {
				warnings = append(warnings, []string{string(w.Code), w.Message})
			}
		}
	}
	for _, w := range r.Warnings {
		warnings = append(warnings, []string{string(w.Code), w.Message})
	}
	section("warnings", warnings)
	return b.String()
}

// layerRows gives one row per layer: scope, status, path, format and variant, and reason.
func layerRows(ls []Layer) [][]string {
	var rows [][]string
	for _, l := range ls {
		p := l.Path
		if p == "" {
			p = "(" + string(l.Scope) + ")"
		}
		var details []string
		if l.Format != "" {
			details = append(details, l.Format)
		}
		if l.Variant != VariantShared {
			v := l.Variant.String()
			if l.Env != "" {
				v += " " + l.Env
			}
			details = append(details, v)
		}
		rows = append(rows, []string{string(l.Scope), l.Status.String(), p, strings.Join(details, ", "), l.Reason})
	}
	return rows
}

// writeTable writes rows with every column but the last padded to its widest cell plus two spaces, and trailing
// spaces trimmed.
func writeTable(b *strings.Builder, indent string, rows [][]string) {
	n := 0
	for _, r := range rows {
		n = max(n, len(r))
	}
	widths := make([]int, n)
	for _, r := range rows {
		for i, cell := range r {
			widths[i] = max(widths[i], len(cell))
		}
	}
	for _, r := range rows {
		var line strings.Builder
		line.WriteString(indent)
		for i := range n {
			cell := ""
			if i < len(r) {
				cell = r[i]
			}
			line.WriteString(cell)
			if i < n-1 {
				line.WriteString(strings.Repeat(" ", widths[i]-len(cell)+2))
			}
		}
		b.WriteString(strings.TrimRight(line.String(), " ") + "\n")
	}
}

// MarshalJSON encodes the report with snake_case field names and enums as strings.
func (r *Report) MarshalJSON() ([]byte, error) {
	type plain Report
	out := plain(*r.clone())
	for _, s := range []*[]ScopeInfo{&out.Scopes} {
		if *s == nil {
			*s = []ScopeInfo{}
		}
	}
	if out.Formats == nil {
		out.Formats = []string{}
	}
	if out.Layers == nil {
		out.Layers = []Layer{}
	}
	return json.Marshal(out)
}

// WithProbes lists in the report every location searched where nothing was found.
func WithProbes() Option {
	return optionFunc(func(c *config) { c.probes = true })
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
	res, err := p.loader.Load(context.Background())
	if err != nil {
		return nil, err
	}
	return tree.CopyMap(res.tree), nil
}

// ReadBytes returns an error: a merged tree has no single byte form.
func (p *KoanfProvider) ReadBytes() ([]byte, error) {
	return nil, errors.New("cascadingconfig: KoanfProvider has no byte form; use Read")
}
