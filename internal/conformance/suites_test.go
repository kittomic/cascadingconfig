package conformance_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	cc "github.com/kittomic/cascadingconfig"
	"github.com/kittomic/cascadingconfig/cctest"
	ccjson "github.com/kittomic/cascadingconfig/format/json"
	"github.com/kittomic/cascadingconfig/format/toml"
	"github.com/kittomic/cascadingconfig/format/yaml"
	"github.com/kittomic/cascadingconfig/internal/conformance"
)

// result is one case's outcome: whether it passed, and if not, why; for a JSON i_ case, which way the decoder went.
type result struct {
	pass     bool
	why      string
	recorded string // conformance.Accepted or conformance.Rejected, for an i_ case
}

// tally counts a suite's cases by how they ended (DL-272).
type tally struct {
	cases, pass, deviation, failure, accepted, rejected int
}

// judge checks a suite's results against its outcomes file (DL-271), and returns its tally.
func judge(t *testing.T, suite string, results map[string]result) tally {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("outcomes", suite+".txt"))
	if err != nil {
		t.Fatal(err)
	}
	listed, err := conformance.ReadOutcomes(data)
	if err != nil {
		t.Fatalf("outcomes/%s.txt: %v", suite, err)
	}
	var n tally
	for _, id := range slices.Sorted(keys(results)) {
		r, o := results[id], listed[id]
		n.cases++
		switch {
		case r.recorded != "":
			if o.Category != r.recorded {
				t.Errorf("%s: the decoder %s it; list it as %q with a reason", id, r.recorded, r.recorded)
			}
		case r.pass && o.Category != "":
			t.Errorf("%s passes now; remove its %s line", id, o.Category)
		case !r.pass && o.Category != conformance.Deviation && o.Category != conformance.Failure:
			t.Errorf("%s: %s", id, r.why)
		}
		switch {
		case r.recorded == conformance.Accepted:
			n.accepted++
		case r.recorded == conformance.Rejected:
			n.rejected++
		case r.pass:
			n.pass++
		case o.Category == conformance.Deviation:
			n.deviation++
		case o.Category == conformance.Failure:
			n.failure++
		}
	}
	for id := range listed {
		if _, ok := results[id]; !ok {
			t.Errorf("outcomes/%s.txt lists %s, which the suite doesn't have", suite, id)
		}
	}
	return n
}

func keys[V any](m map[string]V) func(func(string) bool) {
	return func(yield func(string) bool) {
		for k := range m {
			if !yield(k) {
				return
			}
		}
	}
}

// decode runs a decoder as the loader does.
func decode(d cc.Decoder, data []byte) (cc.Document, error) {
	return cc.DecodeDocument(d, "case", data)
}

// DL-270: toml-test's TOML 1.1 cases. A valid case must decode to its typed JSON, and an invalid one must fail.
func TestTOML(t *testing.T) { report(t, "toml-test", judge(t, "toml-test", tomlResults(t))) }

// tomlResults runs every toml-test case.
func tomlResults(t *testing.T) map[string]result {
	t.Helper()
	dir := filepath.Join("testdata", "toml-test", "tests")
	list, err := os.ReadFile(filepath.Join(dir, "files-toml-1.1.0"))
	if err != nil {
		t.Fatal(err)
	}
	results := map[string]result{}
	for _, name := range strings.Fields(string(list)) {
		if !strings.HasSuffix(name, ".toml") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		doc, derr := decode(toml.Decoder(), data)
		if strings.HasPrefix(name, "invalid/") {
			results[name] = result{pass: derr != nil, why: "an invalid file decoded"}
			continue
		}
		if derr != nil {
			results[name] = result{why: "rejected: " + derr.Error()}
			continue
		}
		raw, err := os.ReadFile(filepath.Join(dir, strings.TrimSuffix(name, ".toml")+".json"))
		if err != nil {
			t.Fatal(err)
		}
		var want any
		if err := json.Unmarshal(raw, &want); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		got := any(doc.Tree)
		if doc.Tree == nil {
			got = map[string]any{}
		}
		why := conformance.MatchTOML(got, want)
		results[name] = result{pass: why == "", why: why}
	}
	return results
}

// DL-270: yaml-test-suite's cases. An error case must fail; any other must decode, to the tree its in.json holds.
func TestYAML(t *testing.T) {
	t.Parallel()
	report(t, "yaml-test-suite", judge(t, "yaml-test-suite", yamlResults(t)))
}

// yamlResults runs every yaml-test-suite case.
func yamlResults(t *testing.T) map[string]result {
	t.Helper()
	root := filepath.Join("testdata", "yaml-test-suite")
	results := map[string]result{}
	err := filepath.WalkDir(root, func(p string, de os.DirEntry, err error) error {
		if err != nil || de.Name() != "in.yaml" {
			return err
		}
		caseDir := filepath.Dir(p)
		id := filepath.ToSlash(strings.TrimPrefix(caseDir, root+string(filepath.Separator)))
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		doc, derr := decode(yaml.Decoder(), data)
		if _, err := os.Stat(filepath.Join(caseDir, "error")); err == nil {
			results[id] = result{pass: derr != nil, why: "an error case decoded"}
			return nil
		}
		if derr != nil {
			results[id] = result{why: "rejected: " + derr.Error()}
			return nil
		}
		raw, err := os.ReadFile(filepath.Join(caseDir, "in.json"))
		if errors.Is(err, os.ErrNotExist) {
			results[id] = result{pass: true} // no expected tree: acceptance is all that's checked
			return nil
		} else if err != nil {
			return err
		}
		docs, err := jsonValues(raw)
		if err != nil {
			return fmt.Errorf("%s: %w", id, err)
		}
		switch {
		case len(docs) == 0 || (len(docs) == 1 && docs[0] == nil):
			results[id] = result{pass: doc.Empty, why: "want an empty document"}
		case len(docs) > 1:
			results[id] = result{why: fmt.Sprintf("decoded, but the suite expects %d documents", len(docs))}
		default:
			why := conformance.MatchJSON(doc.Tree, docs[0])
			if doc.Empty {
				why = "decoded as empty"
			}
			results[id] = result{pass: why == "", why: why}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return results
}

// jsonValues decodes every JSON value in data, in order.
func jsonValues(data []byte) ([]any, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	var out []any
	for {
		var v any
		if err := dec.Decode(&v); errors.Is(err, io.EOF) {
			return out, nil
		} else if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
}

// DL-270: JSONTestSuite's parsing cases, each as a document and wrapped as {"v": …}: y_ cases must decode, n_ cases
// must fail, and i_ cases' outcomes are recorded. The core's built-in decoder must agree with format/json on each.
func TestJSON(t *testing.T) {
	skipUnixPaths(t)
	report(t, "JSONTestSuite", judge(t, "JSONTestSuite", jsonResults(t)))
}

// jsonResults runs every JSONTestSuite case, in both forms.
func jsonResults(t *testing.T) map[string]result {
	t.Helper()
	dir := filepath.Join("testdata", "JSONTestSuite", "test_parsing")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	results := map[string]result{}
	for _, e := range entries {
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		for _, form := range []struct {
			name string
			data []byte
		}{
			{"document", data},
			{"wrapped", slices.Concat([]byte(`{"v": `), data, []byte("}"))},
		} {
			id := form.name + "/" + e.Name()
			_, derr := decode(ccjson.Decoder(), form.data)
			if builtin := loadBuiltin(t, form.data); (builtin == nil) != (derr == nil) {
				t.Errorf("%s: format/json and the built-in decoder disagree: %v, %v", id, derr, builtin)
			}
			switch e.Name()[0] {
			case 'y':
				results[id] = result{pass: derr == nil, why: fmt.Sprintf("rejected: %v", derr)}
			case 'n':
				results[id] = result{pass: derr != nil, why: "an invalid case decoded"}
			default:
				r := result{recorded: conformance.Rejected}
				if derr == nil {
					r.recorded = conformance.Accepted
				}
				results[id] = r
			}
		}
	}
	return results
}

// loadBuiltin loads data as myapp.json with the core's built-in JSON decoder, through a loader, and returns the
// error.
func loadBuiltin(t *testing.T, data []byte) error {
	t.Helper()
	l, err := cc.NewWithPreset("myapp", cc.MiseLayout, // a layout brings the built-in JSON decoder
		cc.WithFileSystem(cctest.NewFS(map[string]string{"/c/myapp.json": string(data)})),
		cctest.WithHost(cctest.Host{GOOS: "linux", GOARCH: "amd64", Home: "/home/me", Cwd: "/c"}),
		cc.WithLogger(slogDiscard()),
		cc.WithScope("s", cc.FixedDir("/c", cc.Pattern{Template: "myapp.json"})),
		cc.WithScopes("s"),
	)
	if err != nil {
		t.Fatal(err)
	}
	_, err = l.Load(context.Background())
	return err
}

// report logs a suite's tally.
func report(t *testing.T, suite string, n tally) {
	t.Helper()
	t.Logf("%s: %d cases: %d pass, %d deviations, %d known failures, %d accepted and %d rejected by choice",
		suite, n.cases, n.pass, n.deviation, n.failure, n.accepted, n.rejected)
}
