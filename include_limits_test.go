package cascadingconfig_test

import (
	"context"
	"fmt"
	"strings"
	"testing"

	cc "github.com/kittomic/cascadingconfig"
	"github.com/kittomic/cascadingconfig/cctest"
)

// chainFiles is rootCfg including d1.json, which includes d2.json, and so on down to dn.json, each setting dK = K.
func chainFiles(n int) map[string]string {
	files := map[string]string{rootCfg: includeOf("", "d1.json")}
	for k := 1; k <= n; k++ {
		f := fmt.Sprintf(`{"d%d": %d}`, k, k)
		if k < n {
			f = includeOf(fmt.Sprintf(`"d%d": %d`, k, k), fmt.Sprintf("d%d.json", k+1))
		}
		files[fmt.Sprintf("%s/d%d.json", work, k)] = f
	}
	return files
}

// DL-210: an include more than WithMaxIncludeDepth levels below its root file, 16 by default, is a *CycleError naming
// the including file.
func TestIncludeDepth(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name  string
		depth int
		opts  []cc.Option
		fail  bool
	}{
		{"default, at the limit", 16, nil, false},
		{"default, over the limit", 17, nil, true},
		{"option, at the limit", 2, []cc.Option{cc.WithMaxIncludeDepth(2)}, false},
		{"option, over the limit", 3, []cc.Option{cc.WithMaxIncludeDepth(2)}, true},
		{"option raises the limit", 20, []cc.Option{cc.WithMaxIncludeDepth(20)}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			res, err := incLoad(t, cctest.NewFS(chainFiles(tc.depth)), append([]cc.Option{nested}, tc.opts...)...)
			if !tc.fail {
				if err != nil {
					t.Fatalf("Load: %v", err)
				}
				if _, ok := res.Tree()[fmt.Sprintf("d%d", tc.depth)]; !ok {
					t.Errorf("the deepest file didn't load: %v", res.Tree())
				}
				return
			}
			wantErr[*cc.CycleError](t, err, cc.CodeLoopDetected, fmt.Sprintf("%s/d%d.json", work, tc.depth-1))
		})
	}
}

// DL-210: the depth limit stops a remote server that answers every fetch with another relative include, after as many
// fetches as the limit allows.
func TestRemoteIncludeDepth(t *testing.T) {
	t.Parallel()
	f := newFake("https")
	f.fetch = anyFile(includeOf("", "sub/a.json"), "application/json")
	_, err := remFails(t, f, []string{"https://h.example/a.json"}, nested)
	wantErr[*cc.CycleError](t, err, cc.CodeLoopDetected, "https://h.example/"+strings.Repeat("sub/", 15)+"a.json")
	if n := len(f.refs()); n != 16 {
		t.Errorf("%d fetches, want 16", n)
	}
}

// DL-210: more than WithMaxIncludes include references in one load, 1000 by default and a reference listed twice
// counting twice, is a *TooLargeError naming the including file.
func TestMaxIncludes(t *testing.T) {
	t.Parallel()
	refs := func(n int) []string {
		out := make([]string, n)
		for i := range out {
			out[i] = "a.json"
		}
		return out
	}
	for _, tc := range []struct {
		name string
		refs int
		opts []cc.Option
		fail bool
	}{
		{"default, at the limit", 1000, nil, false},
		{"default, over the limit", 1001, nil, true},
		{"option, at the limit", 3, []cc.Option{cc.WithMaxIncludes(3)}, false},
		{"option, over the limit", 4, []cc.Option{cc.WithMaxIncludes(3)}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			files := map[string]string{rootCfg: includeOf("", refs(tc.refs)...), work + "/a.json": `{"a": 1}`}
			res, err := incLoad(t, cctest.NewFS(files), tc.opts...)
			if !tc.fail {
				if err != nil {
					t.Fatalf("Load: %v", err)
				}
				wantTreeJSON(t, res.Tree(), `{"a": 1}`)
				return
			}
			wantErr[*cc.TooLargeError](t, err, cc.CodeTooLarge, rootCfg)
		})
	}
	t.Run("counted across nested files", func(t *testing.T) {
		files := map[string]string{
			rootCfg:          includeOf("", "a.json", "b.json"),
			work + "/a.json": includeOf("", "c.json"),
			work + "/b.json": `{"b": 1}`,
			work + "/c.json": `{"c": 1}`,
		}
		_, err := incLoad(t, cctest.NewFS(files), nested, cc.WithMaxIncludes(2))
		wantErr[*cc.TooLargeError](t, err, cc.CodeTooLarge, work+"/a.json")
	})
	t.Run("remote references count", func(t *testing.T) {
		f := newFake("https")
		f.fetch = func(context.Context, cc.IncludeRef) (*cc.Fetched, error) {
			return &cc.Fetched{Content: []byte(`{}`), MediaType: "application/json"}, nil
		}
		_, err := remFails(t, f, []string{"https://h.example/a.json", "https://h.example/b.json"}, cc.WithMaxIncludes(1))
		wantErr[*cc.TooLargeError](t, err, cc.CodeTooLarge, rootCfg)
	})
}

// DL-210: a limit that isn't positive is an *OptionError.
func TestIncludeLimitErrors(t *testing.T) {
	t.Parallel()
	runOptionErrorCases(t, []optionErrorCase{
		{name: "zero depth", opts: []cc.Option{scope, cc.WithMaxIncludeDepth(0)}, ops: []string{"WithMaxIncludeDepth"}},
		{name: "negative depth", opts: []cc.Option{scope, cc.WithMaxIncludeDepth(-1)}, ops: []string{"WithMaxIncludeDepth"}},
		{name: "zero includes", opts: []cc.Option{scope, cc.WithMaxIncludes(0)}, ops: []string{"WithMaxIncludes"}},
		{name: "negative includes", opts: []cc.Option{scope, cc.WithMaxIncludes(-1)}, ops: []string{"WithMaxIncludes"}},
	})
}
