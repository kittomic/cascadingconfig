//go:build ignore

// Command restore-mtimes sets each tracked file's modification time to the time of the last commit that changed it,
// and each directory's to the newest time below it. CI runs it after checkout (DL-288).
//
// go test's cache compares the files a test opens by size and modification time, and a fresh checkout gives every file
// a new one, so every test that reads a file would rerun. With commit times, an unchanged file keeps its time from run
// to run, and a changed file gets a new one, because the commit that changed it is new. The history must be complete:
// in a shallow clone, every file gets the one commit's time.
//
// Usage, from the repository root: go run scripts/restore-mtimes.go
package main

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path"
	"strconv"
	"strings"
	"time"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "restore-mtimes:", err)
		os.Exit(1)
	}
}

func run() error {
	tracked, err := git("ls-files", "-z")
	if err != nil {
		return err
	}
	pending := map[string]bool{}
	for _, f := range strings.Split(strings.TrimSuffix(tracked, "\x00"), "\x00") {
		if f != "" {
			pending[f] = true
		}
	}

	// Newest first: the first commit that names a file is the last one that changed it. Merge commits name no files.
	out, err := git("log", "--no-renames", "--format=format:commit %ct", "--name-only", "-z")
	if err != nil {
		return err
	}
	times := map[string]time.Time{}
	var when time.Time
	for _, tok := range strings.Split(out, "\x00") {
		if rest, ok := strings.CutPrefix(tok, "commit "); ok {
			ts, name, _ := strings.Cut(rest, "\n")
			secs, err := strconv.ParseInt(ts, 10, 64)
			if err != nil {
				return fmt.Errorf("unexpected git log header %q", tok)
			}
			when, tok = time.Unix(secs, 0), name
		}
		tok = strings.TrimPrefix(tok, "\n")
		if tok != "" && pending[tok] {
			times[tok] = when
			delete(pending, tok)
		}
	}
	if len(pending) > 0 {
		return fmt.Errorf("%d tracked files have no commit, such as a staged new file", len(pending))
	}

	dirs := map[string]time.Time{}
	for f, t := range times {
		if err := os.Chtimes(f, t, t); err != nil {
			return err
		}
		for d := path.Dir(f); ; d = path.Dir(d) {
			if t.After(dirs[d]) {
				dirs[d] = t
			}
			if d == "." {
				break
			}
		}
	}
	for d, t := range dirs {
		if err := os.Chtimes(d, t, t); err != nil {
			return err
		}
	}
	fmt.Printf("restored the modification times of %d files and %d directories\n", len(times), len(dirs))
	return nil
}

func git(args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, stderr.String())
	}
	return string(out), nil
}
