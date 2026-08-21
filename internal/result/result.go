// Package result loads the per-attempt JSON files a fan-out run produced.
//
// One file per attempt, written by the matrix job that ran the agent and
// uploaded as its own artifact. The loader's whole job is to turn "N artifacts
// on disk, some possibly absent" into "exactly len(ExpectedVariants) rows",
// because the alternative — dropping what is not there — makes a run that paid
// for three attempts and got two look exactly like a run that asked for two.
package result

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Status values an attempt can report. Anything else read off disk is coerced
// to StatusUnknown rather than passed through: these strings are rendered into
// a table and compared against, so an unrecognised one must not silently become
// a fourth category nobody wrote a column for.
const (
	StatusSuccess   = "success"    // the agent changed files and a PR was opened
	StatusNoChanges = "no-changes" // the agent ran and decided nothing needed changing
	StatusFailed    = "failed"     // the agent or a downstream step errored
	StatusMissing   = "missing"    // no artifact was found for an expected variant
	StatusUnknown   = "unknown"    // a file was read but its status was not one of the above
)

// Attempt is one agent run. The JSON tags are the on-disk contract; the fan-out
// workflow writes this file with jq, so renaming a field is a breaking change to
// the workflow as much as to this struct.
type Attempt struct {
	Variant   string `json:"variant"`
	Status    string `json:"status"`
	Branch    string `json:"branch"`
	PRNumber  int    `json:"pr_number"`
	PRURL     string `json:"pr_url"`
	Files     int    `json:"files_changed"`
	Additions int    `json:"additions"`
	Deletions int    `json:"deletions"`
	Duration  int    `json:"duration_seconds"`
	CommitSHA string `json:"commit_sha"`
	Tests     string `json:"test_status"`
	Notes     string `json:"notes"`

	// LoadError records why a file that WAS found could not be used. It is
	// deliberately not folded into Notes: a parse failure is a fault in the
	// harness and a note is something the agent said, and showing them in one
	// column would let the first hide behind the second.
	LoadError string `json:"-"`
}

// Succeeded reports whether the attempt produced a reviewable change.
func (a Attempt) Succeeded() bool { return a.Status == StatusSuccess }

// Load walks dir for *.json files and returns one Attempt per expected variant,
// in the order the variants were given.
//
// Ordering is by the caller's variant list and never by any measured field.
// Sorting by diff size would put the smallest change at the top of a table a
// human reads top-down, which is a recommendation — and "smallest" is not
// "best". The table states facts; the choice stays with the reader.
//
// When expected is empty, whatever was found is returned sorted by variant name
// so the output is at least stable across runs.
func Load(dir string, expected []string) ([]Attempt, error) {
	found, err := scan(dir)
	if err != nil {
		return nil, err
	}

	if len(expected) == 0 {
		out := make([]Attempt, 0, len(found))
		for _, a := range found {
			out = append(out, a)
		}
		sort.Slice(out, func(i, j int) bool { return out[i].Variant < out[j].Variant })
		return out, nil
	}

	out := make([]Attempt, 0, len(expected))
	for _, v := range expected {
		if a, ok := found[v]; ok {
			out = append(out, a)
			delete(found, v)
			continue
		}
		out = append(out, Attempt{Variant: v, Status: StatusMissing})
	}

	// A result whose variant was never requested still gets a row. It means the
	// workflow and this action disagree about the variant list, which is worth
	// seeing rather than discarding.
	extra := make([]Attempt, 0, len(found))
	for _, a := range found {
		extra = append(extra, a)
	}
	sort.Slice(extra, func(i, j int) bool { return extra[i].Variant < extra[j].Variant })
	return append(out, extra...), nil
}

// scan reads every *.json under dir into a map keyed by variant.
func scan(dir string) (map[string]Attempt, error) {
	found := map[string]Attempt{}

	info, err := os.Stat(dir)
	if err != nil {
		if os.IsNotExist(err) {
			// An absent directory is not an error here: every expected variant
			// becomes a missing row, which is a more useful report than a
			// failed step that says nothing about what was requested.
			return found, nil
		}
		return nil, fmt.Errorf("stat results dir: %w", err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("results path %q is not a directory", dir)
	}

	err = filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.EqualFold(filepath.Ext(path), ".json") {
			return nil
		}

		a, loadErr := readOne(path)
		if a.Variant == "" {
			// Without a variant the row cannot be matched to anything the
			// caller asked for, so key it by its filename rather than dropping
			// it — a malformed file is a finding, not a non-event.
			a.Variant = strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
		}
		if loadErr != nil {
			a.Status = StatusFailed
			a.LoadError = loadErr.Error()
		}
		a.Status = normaliseStatus(a.Status)

		// Later files do not overwrite earlier ones; a duplicate variant is a
		// workflow bug and silently keeping the last one read (which depends on
		// walk order) would make it non-reproducible.
		if _, dup := found[a.Variant]; !dup {
			found[a.Variant] = a
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walk results dir: %w", err)
	}
	return found, nil
}

func readOne(path string) (Attempt, error) {
	var a Attempt
	b, err := os.ReadFile(path)
	if err != nil {
		return a, fmt.Errorf("read %s: %w", filepath.Base(path), err)
	}
	if err := json.Unmarshal(b, &a); err != nil {
		return a, fmt.Errorf("parse %s: %w", filepath.Base(path), err)
	}
	return a, nil
}

func normaliseStatus(s string) string {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case StatusSuccess:
		return StatusSuccess
	case StatusNoChanges:
		return StatusNoChanges
	case StatusFailed:
		return StatusFailed
	case StatusMissing:
		return StatusMissing
	default:
		return StatusUnknown
	}
}

// Summary counts attempts by outcome.
type Summary struct {
	Total     int
	Succeeded int
	Missing   int
	Failed    int
}

// Summarise tallies a loaded attempt set.
func Summarise(as []Attempt) Summary {
	s := Summary{Total: len(as)}
	for _, a := range as {
		switch a.Status {
		case StatusSuccess:
			s.Succeeded++
		case StatusMissing:
			s.Missing++
		case StatusFailed:
			s.Failed++
		}
	}
	return s
}
