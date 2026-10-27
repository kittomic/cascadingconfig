package cascadingconfig

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// The include cache (DL-206, DL-207) lives on the real OS filesystem, never the loader's FileSystem, which is
// read-only. Content is stored under the SHA-256 of the content, in content/, and each reference's entry under the
// SHA-256 of its normalised string, in index/, so no part of a reference becomes part of a file name.

// cacheEntry is one reference's index entry.
type cacheEntry struct {
	Identity  string    `json:"identity"`
	Sum       string    `json:"sha256"` // the content's
	MediaType string    `json:"media_type,omitempty"`
	Fetched   time.Time `json:"fetched"`
	Pinned    bool      `json:"pinned"`
	Validator string    `json:"validator,omitempty"` // the resolver's, for a conditional fetch (DL-229)
}

// cached is an index entry with its content.
type cached struct {
	entry   cacheEntry
	content []byte
}

// cacheRoot opens the cache dir, once per load; nil means the cache is off or can't be used, with a warning.
func (ld *loading) cacheRoot() *os.Root {
	if ld.cacheOpened {
		return ld.cache
	}
	ld.cacheOpened = true
	if ld.c.noCache {
		return nil
	}
	dir, err := ld.cacheDir()
	if err == nil {
		err = os.MkdirAll(dir, 0o700)
	}
	var r *os.Root
	if err == nil {
		r, err = os.OpenRoot(dir)
	}
	for _, sub := range []string{"index", "content"} {
		if err == nil {
			err = r.MkdirAll(sub, 0o700)
		}
	}
	if err != nil {
		if r != nil {
			_ = r.Close()
		}
		ld.cacheWarning(err)
		return nil
	}
	ld.cache = r
	return r
}

// cacheDir returns the cache dir: WithIncludeCache's, or {user cache dir}/{name}/includes from the host.
func (ld *loading) cacheDir() (string, error) {
	c := ld.c
	if c.cacheDir != "" {
		return c.cacheDir, nil
	}
	h := c.host
	var base string
	switch h.GOOS() {
	case "windows":
		base, _ = h.LookupEnv("LocalAppData")
	case "darwin", "ios":
		if home, err := h.Home(); err == nil {
			base = filepath.Join(home, "Library", "Caches")
		}
	case "plan9":
		if home, err := h.Home(); err == nil {
			base = filepath.Join(home, "lib", "cache")
		}
	default:
		if v, _ := h.LookupEnv("XDG_CACHE_HOME"); filepath.IsAbs(v) {
			base = v
		} else if home, err := h.Home(); err == nil {
			base = filepath.Join(home, ".cache")
		}
	}
	if base == "" || !filepath.IsAbs(base) {
		return "", errors.New("the host has no user cache directory")
	}
	return filepath.Join(base, c.name, "includes"), nil
}

func (ld *loading) cacheWarning(err error) {
	if ld.cacheWarned {
		return
	}
	ld.cacheWarned = true
	ld.warnings = append(ld.warnings, Warning{
		Code: WarnIncludeCache, Message: fmt.Sprintf("the include cache can't be used, so remote includes are fetched: %v", err),
	})
}

// cacheLookup returns ref's cached entry and content. Content that doesn't match its recorded SHA-256 counts as
// missing.
func (ld *loading) cacheLookup(ref string) (cached, bool) {
	r := ld.cacheRoot()
	if r == nil {
		return cached{}, false
	}
	data, err := r.ReadFile(filepath.Join("index", sha256Hex([]byte(ref))+".json"))
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			ld.cacheWarning(err)
		}
		return cached{}, false
	}
	var e cacheEntry
	if json.Unmarshal(data, &e) != nil || !hex64.MatchString(e.Sum) {
		return cached{}, false
	}
	content, err := r.ReadFile(filepath.Join("content", e.Sum))
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			ld.cacheWarning(err)
		}
		return cached{}, false
	}
	if sha256Hex(content) != e.Sum {
		return cached{}, false
	}
	return cached{entry: e, content: content}, true
}

// cacheStore records ref's content and entry, content first, each by atomic rename. A failure is a warning.
func (ld *loading) cacheStore(ref string, e cacheEntry, content []byte) {
	r := ld.cacheRoot()
	if r == nil {
		return
	}
	idx, err := json.Marshal(e)
	if err == nil {
		err = writeAtomic(r, filepath.Join("content", e.Sum), content)
	}
	if err == nil {
		err = writeAtomic(r, filepath.Join("index", sha256Hex([]byte(ref))+".json"), idx)
	}
	if err != nil {
		ld.cacheWarning(err)
	}
}

// writeAtomic writes data to name in r through a temporary file and a rename.
func writeAtomic(r *os.Root, name string, data []byte) error {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return err
	}
	tmp := filepath.Join(filepath.Dir(name), "tmp-"+hex.EncodeToString(b[:]))
	f, err := r.OpenFile(tmp, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	_, err = f.Write(data)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = r.Rename(tmp, name)
	}
	if err != nil {
		_ = r.Remove(tmp)
	}
	return err
}
