package cascadingconfig_test

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	cc "github.com/kittomic/cascadingconfig"
	"github.com/kittomic/cascadingconfig/cctest"
)

// policyFS has a system file and a project file, each including ref, and files the references can reach.
func policyFS(sysRef, projRef string) *cctest.FS {
	files := map[string]string{
		"/etc/myapp/inc.json":       `{"sys_inc": 1}`,
		"/home/me/up.json":          `{"up": 1}`,
		work + "/inc.json":          `{"proj_inc": 1}`,
		work + "/shared/a.json":     `{"shared": 1}`,
		work + "/conf/a.json":       `{"conf": 1}`,
		work + "/conf/ab.json":      `{"conf": 2}`,
		"/srv/outside.json":         `{"outside": 1}`,
		"/home/me/work2/other.json": `{"other": 1}`,
	}
	ref := func(r string) string {
		if r == "" {
			return `{}`
		}
		b, _ := json.Marshal(r)
		return `{"include": ` + string(b) + `}`
	}
	files[sysFile] = ref(sysRef)
	files[rootCfg] = ref(projRef)
	fsys := cctest.NewFS(files)
	fsys.Symlink(work+"/shared/a.json", work+"/link.json")
	return fsys
}

// policyCase loads policyFS(sysRef, projRef) with opts; with code 0 it must succeed with tree want, and otherwise fail
// with code, naming the file that wrote the rejected reference.
type policyCase struct {
	name             string
	sysRef, projRef  string
	opts             []cc.Option
	want             string
	code             cc.Code
	rejectedIncluder string
}

func runPolicyCases(t *testing.T, tests []policyCase) {
	t.Helper()
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			res, err := incLoad(t, policyFS(tc.sysRef, tc.projRef), tc.opts...)
			if tc.code == 0 {
				if err != nil {
					t.Fatalf("Load: %v", err)
				}
				wantTreeJSON(t, res.Tree(), tc.want)
				return
			}
			if got := cc.CodeOf(err); got != tc.code {
				t.Fatalf("error = %v (code %d), want code %d", err, got, tc.code)
			}
			if tc.code == cc.CodeForbidden {
				wantErr[*cc.PolicyError](t, err, cc.CodeForbidden, tc.rejectedIncluder)
			}
		})
	}
}

// DL-24, DL-188, design.md Per-scope policies: a scope policy replaces the default as a whole; InheritDefault starts
// from the default, wherever it appears; a later option replaces only its own setting.
func TestScopePolicies(t *testing.T) {
	skipUnixPaths(t)
	t.Parallel()
	strict := cc.WithDefaultPolicy(cc.AllowResolvers("file"), cc.Confine("{config_root}"))
	runPolicyCases(t, []policyCase{
		{
			name: "the default applies to every scope", projRef: "../up.json", opts: []cc.Option{strict},
			code: cc.CodeForbidden, rejectedIncluder: rootCfg,
		},
		{
			name: "a scope policy replaces it", projRef: "../up.json",
			opts: []cc.Option{strict, cc.WithScopePolicy("proj", cc.AllowResolvers("file"))},
			want: `{"up": 1}`,
		},
		{
			name: "other scopes keep the default", sysRef: "/srv/outside.json",
			opts: []cc.Option{strict, cc.WithScopePolicy("proj", cc.AllowResolvers("file"))},
			code: cc.CodeForbidden, rejectedIncluder: sysFile,
		},
		{
			name: "InheritDefault, then a new confinement", projRef: "../up.json",
			opts: []cc.Option{strict, cc.WithScopePolicy("proj", cc.InheritDefault(), cc.Confine("/home/me"))},
			want: `{"up": 1}`,
		},
		{
			name: "the new confinement replaces the old one", projRef: "/srv/outside.json",
			opts: []cc.Option{strict, cc.WithScopePolicy("proj", cc.InheritDefault(), cc.Confine("/home/me"))},
			code: cc.CodeForbidden, rejectedIncluder: rootCfg,
		},
		{
			name: "InheritDefault last", projRef: "../up.json",
			opts: []cc.Option{strict, cc.WithScopePolicy("proj", cc.Confine("/home/me"), cc.InheritDefault())},
			want: `{"up": 1}`,
		},
		{
			name: "the last of one option wins", projRef: "../up.json",
			opts: []cc.Option{cc.WithDefaultPolicy(cc.AllowResolvers("file"), cc.Confine("/srv"), cc.Confine("/home/me"))},
			want: `{"up": 1}`,
		},
		{
			name: "the last WithDefaultPolicy wins", projRef: "../up.json",
			opts: []cc.Option{strict, cc.WithDefaultPolicy(cc.AllowResolvers("file"))},
			want: `{"up": 1}`,
		},
		{
			name: "the last WithScopePolicy wins", projRef: "../up.json",
			opts: []cc.Option{
				cc.WithScopePolicy("proj", cc.AllowResolvers("file")),
				cc.WithScopePolicy("proj", cc.AllowResolvers("file"), cc.Confine("{config_root}")),
			},
			code: cc.CodeForbidden, rejectedIncluder: rootCfg,
		},
	})
}

// DL-188, DL-189: a fresh policy allows no resolver unless it lists one; the engine default allows every registered
// resolver.
func TestFreshPolicy(t *testing.T) {
	skipUnixPaths(t)
	t.Parallel()
	runPolicyCases(t, []policyCase{
		{name: "engine default", projRef: "/srv/outside.json", want: `{"outside": 1}`},
		{
			name: "a fresh policy", projRef: "inc.json",
			opts: []cc.Option{cc.WithScopePolicy("proj", cc.Confine("/"))},
			code: cc.CodeForbidden, rejectedIncluder: rootCfg,
		},
		{
			name: "no resolvers", projRef: "inc.json",
			opts: []cc.Option{cc.WithScopePolicy("proj", cc.AllowResolvers())},
			code: cc.CodeForbidden, rejectedIncluder: rootCfg,
		},
		{
			name: "a fresh policy doesn't confine", projRef: "/srv/outside.json",
			opts: []cc.Option{cc.WithScopePolicy("proj", cc.AllowResolvers("file"))},
			want: `{"outside": 1}`,
		},
	})
}

// DL-177: a policy without "file" forbids local includes in its scope, as a 403; removing the file resolver while an
// include key is set is an *OptionError at New.
func TestForbidLocalIncludes(t *testing.T) {
	skipUnixPaths(t)
	t.Parallel()
	runPolicyCases(t, []policyCase{{
		name: "a policy without file", projRef: "inc.json",
		opts: []cc.Option{cc.WithScopePolicy("proj", cc.InheritDefault(), cc.AllowResolvers())},
		code: cc.CodeForbidden, rejectedIncluder: rootCfg,
	}})
	runOptionErrorCases(t, []optionErrorCase{{
		name: "no file resolver",
		opts: []cc.Option{scope, cc.WithIncludeKey("include", cc.IncludeFlat), cc.WithoutIncludeResolver("file")},
		ops:  []string{"New"},
	}})
	if _, err := cc.New("myapp", scope, cc.WithoutIncludeResolver("file")); err != nil {
		t.Errorf("New without an include key or a file resolver: %v", err)
	}
}

// DL-24, design.md Per-scope policies, "Which scope judges an include": the root file's scope, through nested includes.
func TestPolicyOfNestedIncludes(t *testing.T) {
	skipUnixPaths(t)
	t.Parallel()
	fsys := policyFS("", "inc.json")
	fsys.WriteFile(work+"/inc.json", []byte(`{"include": "/srv/outside.json"}`), 0o644)
	_, err := incLoad(t, fsys, nested,
		cc.WithScopePolicy("proj", cc.AllowResolvers("file"), cc.Confine("{config_root}")))
	wantErr[*cc.PolicyError](t, err, cc.CodeForbidden, work+"/inc.json", "/srv/outside.json")
}

// DL-190: explicit files are judged by the policy of the scope whose name they take, or with no marked scope, by the
// default policy.
func TestPolicyOfExplicitFiles(t *testing.T) {
	t.Parallel()
	files := map[string]string{"/srv/app.json": `{"include": "base.json"}`, "/srv/base.json": `{"b": 1}`}
	forbid := cc.WithScopePolicy("proj", cc.AllowResolvers())
	t.Run("the replaced scope's policy", func(t *testing.T) {
		_, err := newLoader(t, files, nil,
			cc.WithScope("proj", cc.FixedDir(work, cc.Pattern{Template: "myapp.json"}), cc.ReplacedByConfigFiles()),
			cc.WithScopes("proj"), cc.WithIncludeKey("include", cc.IncludeFlat),
			cc.WithConfigFiles("/srv/app.json"), forbid,
		).Load(t.Context())
		wantErr[*cc.PolicyError](t, err, cc.CodeForbidden, "/srv/app.json", "base.json")
	})
	t.Run("no marked scope: the default policy", func(t *testing.T) {
		_, err := newLoader(t, files, nil, incScopes,
			cc.WithConfigFiles("/srv/app.json"), cc.WithDefaultPolicy(cc.AllowResolvers()),
			cc.WithScopePolicy("proj", cc.AllowResolvers("file")),
		).Load(t.Context())
		wantErr[*cc.PolicyError](t, err, cc.CodeForbidden, "/srv/app.json", "base.json")
	})
}

// DL-176, design.md Allow-lists: a reference must match the global list and its scheme's list; local references are
// matched as absolute slash paths after symlinks.
func TestAllowLists(t *testing.T) {
	skipUnixPaths(t)
	t.Parallel()
	fileOnly := func(opts ...cc.PolicyOption) []cc.Option {
		return []cc.Option{cc.WithDefaultPolicy(append([]cc.PolicyOption{cc.AllowResolvers("file")}, opts...)...)}
	}
	runPolicyCases(t, []policyCase{
		{
			name: "Prefix, allowed", projRef: "shared/a.json",
			opts: fileOnly(cc.Allow(cc.Prefix(work + "/shared/"))), want: `{"shared": 1}`,
		},
		{
			name: "Prefix, rejected", projRef: "inc.json",
			opts: fileOnly(cc.Allow(cc.Prefix(work + "/shared/"))), code: cc.CodeForbidden, rejectedIncluder: rootCfg,
		},
		{
			name: "Prefix is a plain string prefix", projRef: "/home/me/work2/other.json",
			opts: fileOnly(cc.Allow(cc.Prefix(work))), want: `{"other": 1}`,
		},
		{
			name: "matched after symlinks", projRef: "link.json",
			opts: fileOnly(cc.Allow(cc.Prefix(work + "/shared/"))), want: `{"shared": 1}`,
		},
		{
			name: "a symlink's own path doesn't count", projRef: "link.json",
			opts: fileOnly(cc.Allow(cc.Glob(work + "/link.json"))), code: cc.CodeForbidden, rejectedIncluder: rootCfg,
		},
		{
			name: "Glob ? matches one character", projRef: "conf/a.json",
			opts: fileOnly(cc.AllowFor("file", cc.Glob(work+"/conf/?.json"))), want: `{"conf": 1}`,
		},
		{
			name: "Glob ? matches only one", projRef: "conf/ab.json",
			opts: fileOnly(cc.AllowFor("file", cc.Glob(work+"/conf/?.json"))), code: cc.CodeForbidden, rejectedIncluder: rootCfg,
		},
		{
			name: "any entry of a list", projRef: "conf/ab.json",
			opts: fileOnly(cc.Allow(cc.Prefix("/srv/"), cc.Regexp(`.*/conf/[a-z]+\.json`))), want: `{"conf": 2}`,
		},
		{
			name: "both lists must match", projRef: "/srv/outside.json",
			opts: fileOnly(cc.Allow(cc.Prefix("/srv/")), cc.AllowFor("file", cc.Extension("toml"))),
			code: cc.CodeForbidden, rejectedIncluder: rootCfg,
		},
		{
			name: "both lists match", projRef: "/srv/outside.json",
			opts: fileOnly(cc.Allow(cc.Prefix("/srv/")), cc.AllowFor("file", cc.FileName("*.json"))), want: `{"outside": 1}`,
		},
		{
			name: "MatchFunc gets the normalised string", projRef: "link.json",
			opts: fileOnly(cc.Allow(cc.MatchFunc(func(s string) bool { return s == work+"/shared/a.json" }))),
			want: `{"shared": 1}`,
		},
	})
}

// DL-176: MatchRef gets the whole IncludeRef, so it can judge by the including file's scope.
func TestMatchRef(t *testing.T) {
	skipUnixPaths(t)
	t.Parallel()
	var seen calls[cc.IncludeRef]
	onlySys := cc.MatchRef(func(r cc.IncludeRef) bool {
		seen.add(r)
		return r.From.Scope == "sys"
	})
	opts := []cc.Option{cc.WithDefaultPolicy(cc.AllowResolvers("file"), cc.Allow(onlySys))}
	runPolicyCases(t, []policyCase{
		{name: "from sys", sysRef: "inc.json", opts: opts, want: `{"sys_inc": 1}`},
		{name: "from proj", projRef: "link.json", opts: opts, code: cc.CodeForbidden, rejectedIncluder: rootCfg},
	})
	var proj cc.IncludeRef
	for _, r := range seen.all() {
		if r.From.Path == rootCfg {
			proj = r
		}
	}
	if proj.Raw != "link.json" || proj.Scheme != "file" || proj.Location != work+"/shared/a.json" || proj.From.Scope != "proj" {
		t.Errorf("IncludeRef = %+v, want Raw link.json, Scheme file, Location the target, From the proj file", proj)
	}
}

// DL-176, A2: Glob's ? matches one character other than /, in WithFileFormat too; Prefix works there as well.
func TestGlobAndPrefixInFileFormat(t *testing.T) {
	t.Parallel()
	files := map[string]string{work + "/myapp.cfg": `{"a": 1}`}
	scope := cc.Options(cc.WithScope("proj", cc.FixedDir(work, cc.Pattern{Template: "{name}.cfg"})), cc.WithScopes("proj"))
	for _, tc := range []struct {
		name string
		m    cc.Matcher
		want string
	}{
		{"? matches", cc.Glob(work + "/mya?p.cfg"), `{"a": 1}`},
		{"? is one character", cc.Glob(work + "/my?.cfg"), `{}`},
		{"? isn't /", cc.Glob("/home/me?work/myapp.cfg"), `{}`},
		{"Prefix", cc.Prefix(work + "/"), `{"a": 1}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			res, err := newLoader(t, files, nil, scope, cc.WithFileFormat(tc.m, "json")).Load(t.Context())
			if err != nil {
				t.Fatalf("Load: %v", err)
			}
			wantTreeJSON(t, res.Tree(), tc.want)
		})
	}
}

// DL-176 to DL-193: invalid include and policy options are *OptionErrors named after the option.
func TestIncludePolicyOptionErrors(t *testing.T) {
	t.Parallel()
	inc := cc.WithIncludeKey("include", cc.IncludeFlat)
	runOptionErrorCases(t, []optionErrorCase{
		{name: "empty include key", opts: []cc.Option{scope, cc.WithIncludeKey("", cc.IncludeFlat)}, ops: []string{"WithIncludeKey"}},
		{name: "include key is the unset key", opts: []cc.Option{scope, cc.WithIncludeKey("_unset", cc.IncludeFlat)}, ops: []string{"WithIncludeKey"}},
		{
			name: "include key is a renamed unset key",
			opts: []cc.Option{scope, cc.WithUnsetKey("drop"), cc.WithIncludeKey("drop", cc.IncludeFlat)},
			ops:  []string{"WithIncludeKey"},
		},
		{name: "unknown nesting", opts: []cc.Option{scope, cc.WithIncludeKey("include", cc.IncludeNesting(7))}, ops: []string{"WithIncludeKey"}},
		{name: "empty scheme to remove", opts: []cc.Option{scope, cc.WithoutIncludeResolver("")}, ops: []string{"WithoutIncludeResolver"}},
		{name: "undeclared scope", opts: []cc.Option{scope, cc.WithScopePolicy("nope", cc.AllowResolvers("file"))}, ops: []string{"WithScopePolicy"}, contains: []string{"nope"}},
		{name: "nil policy option", opts: []cc.Option{scope, cc.WithScopePolicy("s", nil)}, ops: []string{"WithScopePolicy"}},
		{name: "nil default policy option", opts: []cc.Option{scope, cc.WithDefaultPolicy(nil)}, ops: []string{"WithDefaultPolicy"}},
		{name: "unregistered scheme", opts: []cc.Option{scope, inc, cc.WithDefaultPolicy(cc.AllowResolvers("file", "https"))}, ops: []string{"AllowResolvers"}, contains: []string{"https"}},
		{name: "empty scheme", opts: []cc.Option{scope, cc.WithDefaultPolicy(cc.AllowResolvers(""))}, ops: []string{"AllowResolvers"}},
		{name: "AllowFor an unregistered scheme", opts: []cc.Option{scope, cc.WithDefaultPolicy(cc.AllowFor("git", cc.Prefix("git::")))}, ops: []string{"AllowFor"}, contains: []string{"git"}},
		{name: "AllowFor an empty scheme", opts: []cc.Option{scope, cc.WithDefaultPolicy(cc.AllowFor("", cc.Prefix("/")))}, ops: []string{"AllowFor"}},
		{name: "nil matcher", opts: []cc.Option{scope, cc.WithDefaultPolicy(cc.Allow(nil))}, ops: []string{"Allow"}},
		{name: "bad matcher", opts: []cc.Option{scope, cc.WithDefaultPolicy(cc.Allow(cc.Regexp("(")))}, ops: []string{"Regexp"}},
		{name: "empty prefix", opts: []cc.Option{scope, cc.WithDefaultPolicy(cc.Allow(cc.Prefix("")))}, ops: []string{"Prefix"}},
		{name: "nil MatchRef", opts: []cc.Option{scope, cc.WithDefaultPolicy(cc.Allow(cc.MatchRef(nil)))}, ops: []string{"MatchRef"}},
		{name: "MatchRef in WithFileFormat", opts: []cc.Option{scope, cc.WithFileFormat(cc.MatchRef(func(cc.IncludeRef) bool { return true }), "json")}, ops: []string{"WithFileFormat"}, contains: []string{"MatchRef"}},
		{name: "empty root", opts: []cc.Option{scope, cc.WithDefaultPolicy(cc.Confine(""))}, ops: []string{"Confine"}},
		{name: "relative root", opts: []cc.Option{scope, cc.WithDefaultPolicy(cc.Confine("conf"))}, ops: []string{"Confine"}, contains: []string{"conf"}},
		{name: "unknown placeholder", opts: []cc.Option{scope, cc.WithDefaultPolicy(cc.Confine("{home}"))}, ops: []string{"Confine"}, contains: []string{"{home}"}},
		{name: "nil trust store", opts: []cc.Option{scope, cc.WithDefaultPolicy(cc.RequireTrust(nil))}, ops: []string{"RequireTrust"}},
		{name: "nil permission check", opts: []cc.Option{scope, cc.WithDefaultPolicy(cc.FilePermissions(nil))}, ops: []string{"FilePermissions"}},
		{name: "nil layer transform", opts: []cc.Option{scope, cc.WithLayerTransform(nil)}, ops: []string{"WithLayerTransform"}},
		{name: "nil result transform", opts: []cc.Option{scope, cc.WithResultTransform(nil)}, ops: []string{"WithResultTransform"}},
	})
}

// DL-176 to DL-193: every include and policy option with valid values, together, is accepted.
func TestPolicyOptionsValid(t *testing.T) {
	skipUnixPaths(t)
	t.Parallel()
	_, err := cc.New("myapp", scope, cc.WithIncludeKey("include", cc.IncludeNested),
		cc.WithDefaultPolicy(cc.AllowResolvers("file"), cc.Allow(cc.Prefix("/"), cc.Glob("/**")),
			cc.AllowFor("file", cc.MatchRef(func(cc.IncludeRef) bool { return true })),
			cc.Confine("{config_root}", "{user}", "{system}", "{cwd}", "~", "~/x", "/abs"),
			cc.RequireTrust(cc.TrustPaths("/")), cc.FilePermissions(cc.StrictModes)),
		cc.WithPlatformDirs(cc.XDGDirs),
		cc.WithScopePolicy("s", cc.AllowResolvers()),
	)
	if err != nil {
		t.Errorf("New: %v", err)
	}
}

// policyErrorMentions checks a rejection's message says which rule broke, without quoting the file.
func policyErrorMentions(t *testing.T, err error, rule string) {
	t.Helper()
	var pe *cc.PolicyError
	if !errors.As(err, &pe) {
		t.Fatalf("error = %v, want a *PolicyError", err)
	}
	if !strings.Contains(pe.Error(), rule) {
		t.Errorf("error %q doesn't say %q", pe.Error(), rule)
	}
}

// DL-185: a rejection says which rule the reference broke.
func TestPolicyErrorMessages(t *testing.T) {
	skipUnixPaths(t)
	t.Parallel()
	for _, tc := range []struct {
		name string
		opt  cc.Option
		rule string
	}{
		{"confinement", cc.WithDefaultPolicy(cc.AllowResolvers("file"), cc.Confine("/nowhere")), "confinement"},
		{"allow-list", cc.WithDefaultPolicy(cc.AllowResolvers("file"), cc.Allow(cc.Prefix("/nowhere/"))), "allow-list"},
		{"resolver", cc.WithDefaultPolicy(cc.AllowResolvers()), `"file"`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := incLoad(t, policyFS("", "inc.json"), tc.opt)
			policyErrorMentions(t, err, tc.rule)
		})
	}
}
