package cascadingconfig_test

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	cc "github.com/kittomic/cascadingconfig"
	"github.com/kittomic/cascadingconfig/cctest"
)

// reportPolicies loads a project with its own policy, under a default one, plus a Values scope.
func reportPolicies(t *testing.T) *cc.Report {
	t.Helper()
	files := map[string]string{rootCfg: `{"include": "base.json"}`, work + "/base.json": `{}`}
	res, err := newLoader(t, files, nil, incScopes,
		cc.WithScope("v", cc.Values(cc.Tree{})), cc.WithScopes("sys", "proj", "v"),
		cc.WithDefaultPolicy(cc.AllowResolvers("file"), cc.Confine("{config_root}", "{system}"),
			cc.FilePermissions(cc.StrictModes)),
		cc.WithPlatformDirs(cc.XDGDirs),
		cc.WithScopePolicy("proj", cc.InheritDefault(),
			cc.Allow(cc.Prefix("/home/me/"), cc.Extension("json", "toml")),
			cc.AllowFor("file", cc.MatchRef(func(cc.IncludeRef) bool { return true }), cc.Glob("/home/me/**"),
				cc.MatchFunc(func(string) bool { return true }), cc.Regexp(`.*\.json`), cc.FileName("*.json")),
			cc.Confine("{config_root}"),
			cc.RequireTrust(cc.TrustPaths("/home/me")),
		),
	).Load(t.Context())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	return res.Describe()
}

func scopeInfo(t *testing.T, rep *cc.Report, name cc.ScopeName) cc.ScopeInfo {
	t.Helper()
	for _, s := range rep.Scopes {
		if s.Name == name {
			return s
		}
	}
	t.Fatalf("no scope %s in the report", name)
	return cc.ScopeInfo{}
}

// DL-159, DL-194, api.md Result, Report and Origin: each file scope's effective policy, with matchers printed as their
// constructors; nil for scopes that read no files.
func TestReportPolicyInfo(t *testing.T) {
	rep := reportPolicies(t)
	wantSys := &cc.PolicyInfo{
		Resolvers:       []string{"file"},
		Confine:         []string{"{config_root}", "{system}"},
		FilePermissions: true,
	}
	if got := scopeInfo(t, rep, "sys").Policy; !reflect.DeepEqual(got, wantSys) {
		t.Errorf("sys policy = %+v, want %+v", got, wantSys)
	}
	wantProj := &cc.PolicyInfo{
		Inherited: true,
		Resolvers: []string{"file"},
		Allow:     []string{`Prefix("/home/me/")`, `Extension("json", "toml")`},
		AllowFor: map[string][]string{"file": {
			"MatchRef", `Glob("/home/me/**")`, "MatchFunc", `Regexp(".*\\.json")`, `FileName("*.json")`,
		}},
		Confine:         []string{"{config_root}"},
		RequireTrust:    true,
		FilePermissions: true,
	}
	if got := scopeInfo(t, rep, "proj").Policy; !reflect.DeepEqual(got, wantProj) {
		t.Errorf("proj policy = %+v, want %+v", got, wantProj)
	}
	if got := scopeInfo(t, rep, "v").Policy; got != nil {
		t.Errorf("v policy = %+v, want nil", got)
	}
}

// DL-194: the policy's JSON uses snake_case keys.
func TestReportPolicyJSON(t *testing.T) {
	js, err := json.Marshal(scopeInfo(t, reportPolicies(t), "proj"))
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{`"policy":`, `"inherited":true`, `"resolvers":["file"]`, `"allow_for":{"file":`,
		`"confine":["{config_root}"]`, `"require_trust":true`, `"file_permissions":true`} {
		if !strings.Contains(string(js), key) {
			t.Errorf("JSON %s doesn't contain %s", js, key)
		}
	}
}

// DL-194, api.md Settled in M7: the text report has a policies section, with a default row and a row for each scope
// with its own policy, and marks included layers "included by".
func TestReportPolicyText(t *testing.T) {
	text := reportPolicies(t).String()
	wantSection := `policies:
  default  resolvers file; confine {config_root}, {system}; permissions
  proj     inherits default; resolvers file; allow Prefix("/home/me/"), Extension("json", "toml"); ` +
		`allow for file: MatchRef, Glob("/home/me/**"), MatchFunc, Regexp(".*\\.json"), FileName("*.json"); ` +
		`confine {config_root}; trust; permissions
`
	if !strings.Contains(text, wantSection) {
		t.Errorf("report:\n%s\nwant the section:\n%s", text, wantSection)
	}
	if !strings.Contains(text, "included by "+rootCfg) {
		t.Errorf("report:\n%s\nwant base.json marked included by %s", text, rootCfg)
	}
	if strings.Index(text, "scopes:") > strings.Index(text, "policies:") {
		t.Errorf("the policies section comes before the scopes:\n%s", text)
	}
}

// DL-194, design.md Per-scope policies, "Describe()": a rejected reference fails the load, and the failed result's
// report lists it as LayerFailed, with its scope, its includer, and which scope's policy rejected it and why.
func TestReportRejectedReference(t *testing.T) {
	fsys := cctest.NewFS(map[string]string{rootCfg: `{"include": "../outside.json"}`, "/home/me/outside.json": `{}`})
	res, err := incLoad(t, fsys, cc.WithScopePolicy("proj", cc.AllowResolvers("file"), cc.Confine("{config_root}")))
	if err == nil {
		t.Fatalf("Load succeeded, want a *PolicyError")
	}
	l := layerAt(t, res.Describe(), "/home/me/outside.json")
	if l.Status != cc.LayerFailed || l.Scope != "proj" || l.IncludedBy != rootCfg ||
		l.Reason != "rejected by the proj policy: outside the confinement roots" {
		t.Errorf("layer = %+v, want LayerFailed in proj, included by %s, rejected by the proj policy for confinement",
			l, rootCfg)
	}
}
