package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"sync"
)

// store is a cascadingconfig.TrustStore kept in a JSON file: each approved file's path, with the SHA-256 of the
// content that was approved. It also remembers every file the loader asked about, which is what "trust" approves.
type store struct {
	file string

	mu       sync.Mutex
	approved map[string]string // path → digest
	asked    map[string]string // path → digest, from this run's loads
}

func openStore(file string) (*store, error) {
	s := &store{file: file, approved: map[string]string{}, asked: map[string]string{}}
	data, err := os.ReadFile(file)
	if errors.Is(err, fs.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(data, &s.approved); err != nil {
		return nil, fmt.Errorf("%s: %w", file, err)
	}
	return s, nil
}

// Trusted reports whether path was approved with this content. The loader calls it before decoding the file.
func (s *store) Trusted(_ context.Context, path, digest string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.asked[path] = digest
	return s.approved[path] == digest, nil
}

// approveAsked approves every file the loader asked about that isn't approved as it is now, saves the store, and
// returns those files.
func (s *store) approveAsked() ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var added []string
	for path, digest := range s.asked {
		if s.approved[path] != digest {
			s.approved[path] = digest
			added = append(added, path)
		}
	}
	slices.Sort(added)
	if len(added) == 0 {
		return nil, nil
	}
	data, err := json.MarshalIndent(s.approved, "", "  ")
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(s.file), 0o700); err != nil {
		return nil, err
	}
	return added, os.WriteFile(s.file, append(data, '\n'), 0o600)
}
