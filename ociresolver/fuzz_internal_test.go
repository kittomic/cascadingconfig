package ociresolver

import (
	"archive/tar"
	"bytes"
	"errors"
	"io"
	"path"
	"strings"
	"testing"
)

func tarOf(t testing.TB, entries ...[3]string) []byte {
	t.Helper()
	var b bytes.Buffer
	tw := tar.NewWriter(&b)
	for _, e := range entries {
		typ := byte(tar.TypeReg)
		if e[2] != "" {
			typ = e[2][0]
		}
		hdr := &tar.Header{Name: e[0], Typeflag: typ, Mode: 0o644}
		if typ == tar.TypeReg {
			hdr.Size = int64(len(e[1]))
		}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatal(err)
		}
		if hdr.Size > 0 {
			if _, err := tw.Write([]byte(e[1])); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

// DL-215: applyTar finds the requested file, lets a later layer replace it, and removes it on a whiteout.
func TestApplyTar(t *testing.T) {
	t.Parallel()
	s := &layerState{want: "dir/a.json"}
	if err := s.applyTar(bytes.NewReader(tarOf(t, [3]string{"./dir/a.json", "one", ""})), 1<<20, 1<<10); err != nil || !s.found || string(s.content) != "one" {
		t.Fatalf("after the first layer: found %v, content %q, err %v; want one", s.found, s.content, err)
	}
	if err := s.applyTar(bytes.NewReader(tarOf(t, [3]string{"dir/a.json", "two", ""})), 1<<20, 1<<10); err != nil || string(s.content) != "two" {
		t.Fatalf("after the second layer: content %q, err %v; want two", s.content, err)
	}
	if err := s.applyTar(bytes.NewReader(tarOf(t, [3]string{"dir/.wh.a.json", "", ""})), 1<<20, 1<<10); err != nil || s.found {
		t.Fatalf("after the whiteout: found %v, err %v; want removed", s.found, err)
	}
}

// malformed reports whether a tar header's name breaks the entry rules: absolute, a .. segment, or a backslash.
func malformed(name string) bool {
	if strings.HasPrefix(name, "/") || strings.Contains(name, `\`) {
		return true
	}
	for seg := range strings.SplitSeq(name, "/") {
		if seg == ".." {
			return true
		}
	}
	return false
}

// FuzzTarEntries checks the layer reader on any bytes (DL-215): it never panics; a layer with a malformed entry name
// never succeeds; a file it finds came from a regular entry and fits the file limit. Its positive control is a
// well-formed layer holding the file.
func FuzzTarEntries(f *testing.F) {
	good := tarOf(f, [3]string{"a.json", `{"v": 1}`, ""})
	f.Add(good)
	f.Add(tarOf(f, [3]string{"../a.json", "x", ""}, [3]string{"a.json", "y", ""}))
	f.Add(tarOf(f, [3]string{"a.json", "", string(tar.TypeSymlink)}))
	f.Add(tarOf(f, [3]string{".wh.a.json", "", ""}))
	f.Add([]byte("not a tar"))
	f.Fuzz(func(t *testing.T, data []byte) {
		s := &layerState{want: "a.json"}
		err := s.applyTar(bytes.NewReader(data), 1<<20, 1<<10)
		if bytes.Equal(data, good) && (err != nil || !s.found || string(s.content) != `{"v": 1}`) {
			t.Fatalf("the well-formed layer gave found %v, content %q, err %v", s.found, s.content, err)
		}
		if err != nil {
			return
		}
		tr := tar.NewReader(bytes.NewReader(data))
		var last *tar.Header
		for {
			hdr, err := tr.Next()
			if errors.Is(err, io.EOF) || err != nil {
				break
			}
			if malformed(hdr.Name) {
				t.Fatalf("the layer has the malformed entry %q, but applyTar succeeded", hdr.Name)
			}
			if path.Clean(strings.TrimPrefix(hdr.Name, "./")) == "a.json" {
				last = hdr
			}
		}
		if s.found {
			if last == nil || last.Typeflag != tar.TypeReg {
				t.Errorf("found a.json, but its last entry is %+v", last)
			}
			if len(s.content) > 1<<10 {
				t.Errorf("found %d bytes, over the file limit", len(s.content))
			}
		}
	})
}
