package cascadingconfig_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"

	cc "github.com/kittomic/cascadingconfig"
	"github.com/kittomic/cascadingconfig/cctest"
)

// testStore trusts the paths in trusted, records every question, and fails with err when it's set.
type testStore struct {
	trusted []string
	err     error
	asked   calls[[2]string] // path, digest
}

func (s *testStore) Trusted(_ context.Context, path, digest string) (bool, error) {
	s.asked.add([2]string{path, digest})
	if s.err != nil {
		return false, s.err
	}
	return slices.Contains(s.trusted, path), nil
}

func (s *testStore) askedAbout() []string {
	var out []string
	for _, a := range s.asked.all() {
		out = append(out, a[0])
	}
	return out
}

// trustProj requires proj's files to be approved by store.
func trustProj(store cc.TrustStore) cc.Option {
	return cc.WithScopePolicy("proj", cc.InheritDefault(), cc.RequireTrust(store))
}

// DL-28, DL-192, design.md Trust store: an untrusted file is skipped with a warning as LayerUntrusted, and never
// decoded, so one that doesn't parse doesn't fail the load. Scopes without RequireTrust never ask the store.
func TestUntrustedFiles(t *testing.T) {
	t.Parallel()
	store := &testStore{}
	res := incOK(t, map[string]string{sysFile: `{"a": 1}`, rootCfg: `{"b": SECRET`}, trustProj(store))
	wantTreeJSON(t, res.Tree(), `{"a": 1}`)
	l := layerAt(t, res.Describe(), rootCfg)
	if l.Status != cc.LayerUntrusted || !hasWarningCode(l.Warnings, cc.WarnUntrusted) {
		t.Errorf("layer = %+v, want LayerUntrusted with an untrusted warning", l)
	}
	if got := store.askedAbout(); !slices.Equal(got, []string{rootCfg}) {
		t.Errorf("the store was asked about %v, want only %s", got, rootCfg)
	}

	store = &testStore{trusted: []string{rootCfg}}
	res = incOK(t, map[string]string{sysFile: `{"a": 1}`, rootCfg: `{"b": 2}`}, trustProj(store))
	wantTreeJSON(t, res.Tree(), `{"a": 1, "b": 2}`)
}

// DL-192, design.md Trust store, "The digest": the lowercase hex SHA-256 of the raw bytes, byte-order mark included;
// the path has symlinks resolved.
func TestTrustStoreArguments(t *testing.T) {
	t.Parallel()
	raw := "\uFEFF{\"b\": 2}"
	sum := sha256.Sum256([]byte(raw))
	fsys := cctest.NewFS(map[string]string{"/srv/real.json": raw})
	fsys.Symlink("/srv/real.json", rootCfg)
	store := &testStore{trusted: []string{"/srv/real.json"}}
	res, err := incLoad(t, fsys, trustProj(store))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	wantTreeJSON(t, res.Tree(), `{"b": 2}`)
	if got := store.asked.all(); len(got) != 1 || got[0] != [2]string{"/srv/real.json", hex.EncodeToString(sum[:])} {
		t.Errorf("the store was asked %v, want the target path and the raw bytes' digest", got)
	}
}

// DL-192: a store error fails the load as a *BaseError (500) with Op "trust store", wrapping the store's error.
func TestTrustStoreError(t *testing.T) {
	t.Parallel()
	boom := errors.New("store unavailable")
	_, err := incFails(t, map[string]string{rootCfg: `{}`}, trustProj(&testStore{err: boom}))
	if !errors.Is(err, boom) || cc.CodeOf(err) != cc.CodeInternal {
		t.Fatalf("error = %v, want a 500 wrapping the store's error", err)
	}
	var be *cc.BaseError
	if !errors.As(err, &be) || be.Op != "trust store" {
		t.Errorf("error = %#v, want a *BaseError with Op \"trust store\"", err)
	}
}

// DL-192, design.md Includes, "Trust": an included file inherits its includer's verdict, and the store isn't asked.
func TestTrustOfIncludedFiles(t *testing.T) {
	t.Parallel()
	store := &testStore{trusted: []string{rootCfg}}
	res := incOK(t, map[string]string{rootCfg: `{"include": "base.json"}`, work + "/base.json": `{"b": 1}`},
		trustProj(store))
	wantTreeJSON(t, res.Tree(), `{"b": 1}`)
	if got := store.askedAbout(); !slices.Equal(got, []string{rootCfg}) {
		t.Errorf("the store was asked about %v, want only %s", got, rootCfg)
	}
}

// DL-181, DL-192: an untrusted explicit file is a *PolicyError, because a file named directly is never skipped.
func TestUntrustedExplicitFile(t *testing.T) {
	t.Parallel()
	_, err := newLoader(t, map[string]string{"/srv/app.json": `{}`}, nil,
		cc.WithScope("proj", cc.FixedDir(work, cc.Pattern{Template: "myapp.json"}), cc.ReplacedByConfigFiles()),
		cc.WithScopes("proj"), cc.WithConfigFiles("/srv/app.json"), trustProj(&testStore{}),
	).Load(t.Context())
	wantErr[*cc.PolicyError](t, err, cc.CodeForbidden, "/srv/app.json")
}

// DL-190, DL-192: an untrusted bootstrap file is skipped with a warning, so it can't choose the env.
func TestUntrustedBootstrapFile(t *testing.T) {
	t.Parallel()
	rc := work + "/.myapprc.json"
	files := map[string]string{rc: `{"env": ["dev"]}`, work + "/myapp.dev.json": `{"dev": true}`}
	store := &testStore{trusted: []string{work + "/myapp.dev.json"}}
	l, err := cc.NewWithPreset("myapp", cc.MiseLayout, cc.WithFileSystem(cctest.NewFS(files)), cctest.WithHost(testHost(nil)),
		cc.WithScopePolicy(cc.ScopeProject, cc.InheritDefault(), cc.RequireTrust(store)))
	if err != nil {
		t.Fatalf("NewWithPreset: %v", err)
	}
	res, err := l.Load(t.Context())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	wantTreeJSON(t, res.Tree(), `{}`)
	rep := res.Describe()
	if len(rep.Envs.Selected) != 0 {
		t.Errorf("envs = %v, want none", rep.Envs.Selected)
	}
	found := false
	for _, b := range rep.Bootstrap {
		if b.Path == rc {
			found = true
			if b.Status != cc.LayerUntrusted || !hasWarningCode(b.Warnings, cc.WarnUntrusted) {
				t.Errorf("bootstrap layer = %+v, want LayerUntrusted with an untrusted warning", b)
			}
		}
	}
	if !found {
		t.Errorf("the bootstrap file isn't in the report: %+v", rep.Bootstrap)
	}
}

// DL-192, design.md Trust store: TrustPaths entries follow Glob; one without wildcards trusts that path and everything
// beneath it, by whole segments, as mise's trusted_config_paths does.
func TestTrustPaths(t *testing.T) {
	t.Parallel()
	store := cc.TrustPaths("/home/me/work", "/srv/*/app.json", "/opt/?.json", "/data/**/cfg.json")
	for _, tc := range []struct {
		path string
		want bool
	}{
		{"/home/me/work", true},
		{"/home/me/work/myapp.json", true},
		{"/home/me/work/a/b/c.json", true},
		{"/home/me/workx/myapp.json", false},
		{"/home/me/myapp.json", false},
		{"/srv/x/app.json", true},
		{"/srv/x/y/app.json", false},
		{"/opt/a.json", true},
		{"/opt/ab.json", false},
		{"/data/cfg.json", true},
		{"/data/a/b/cfg.json", true},
	} {
		got, err := store.Trusted(t.Context(), tc.path, "")
		if err != nil || got != tc.want {
			t.Errorf("Trusted(%s) = %v, %v; want %v", tc.path, got, err, tc.want)
		}
	}
}

// DL-192: TrustPaths expands a leading ~. It has no host to read, so it uses the process's home directory.
func TestTrustPathsHome(t *testing.T) {
	t.Parallel()
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		t.Skip("no home directory")
	}
	store := cc.TrustPaths("~/trusted")
	if ok, _ := store.Trusted(t.Context(), filepath.ToSlash(filepath.Join(home, "trusted", "a.json")), ""); !ok {
		t.Errorf("~/trusted doesn't cover a file beneath the home directory")
	}
}

// DL-192: a TrustPaths store in a policy works end to end.
func TestTrustPathsInPolicy(t *testing.T) {
	t.Parallel()
	res := incOK(t, map[string]string{rootCfg: `{"a": 1}`}, trustProj(cc.TrustPaths(work)))
	wantTreeJSON(t, res.Tree(), `{"a": 1}`)
	res = incOK(t, map[string]string{rootCfg: `{"a": 1}`}, trustProj(cc.TrustPaths("/elsewhere")))
	wantTreeJSON(t, res.Tree(), `{}`)
}
