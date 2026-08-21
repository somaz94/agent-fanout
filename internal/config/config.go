package config

import (
	"os"
	"strconv"
	"strings"
)

// Config holds the action configuration loaded from environment variables.
//
// Every field maps to an INPUT_* variable that GitHub Actions sets from the
// `with:` block in action.yml.
type Config struct {
	// ResultsDir is the directory the attempt artifacts were downloaded into.
	// actions/download-artifact places each artifact in its own subdirectory,
	// so the loader walks this tree rather than reading one flat level.
	ResultsDir string

	// ExpectedVariants is the full list of variants the fan-out was asked to
	// run. It is what makes a MISSING result visible: an attempt whose job died
	// before uploading anything has no file to be read, and without this list
	// the comparison would silently be an N-1-way one.
	ExpectedVariants []string

	// Issue is the issue number the comparison comment is posted to. Zero means
	// "do not comment" — the step summary is still written.
	Issue int

	// Repository is "owner/name", supplied by GITHUB_REPOSITORY.
	Repository string

	// Token authenticates the comment write.
	Token string

	// Title is the heading of the rendered comparison.
	Title string

	// DryRun renders the comparison and writes the step summary but performs no
	// API write.
	DryRun bool
}

// Load reads configuration from INPUT_* environment variables.
func Load() *Config {
	return &Config{
		ResultsDir:       getEnv("INPUT_RESULTS_DIR", "results"),
		ExpectedVariants: splitList(getEnv("INPUT_VARIANTS", "")),
		Issue:            atoi(getEnv("INPUT_ISSUE", "")),
		Repository:       getEnv("INPUT_REPOSITORY", os.Getenv("GITHUB_REPOSITORY")),
		Token:            getEnv("INPUT_GITHUB_TOKEN", ""),
		Title:            getEnv("INPUT_TITLE", "Agent fan-out results"),
		DryRun:           strings.EqualFold(getEnv("INPUT_DRY_RUN", "false"), "true"),
	}
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// splitList accepts either a comma- or newline-separated list, because a
// multi-line YAML scalar is the natural way to write more than two variants and
// a single-line one is the natural way to write two.
func splitList(s string) []string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	f := strings.FieldsFunc(s, func(r rune) bool {
		return r == ',' || r == '\n' || r == '\r'
	})
	out := make([]string, 0, len(f))
	seen := make(map[string]bool, len(f))
	for _, v := range f {
		t := strings.TrimSpace(v)
		if t == "" {
			continue
		}
		// A repeated variant must collapse, not repeat. The loader consumes
		// each name once, so a second "a" finds nothing left and is reported
		// MISSING — inventing a harness failure that never happened, in the
		// one signal this tool has to be trusted about.
		if seen[t] {
			continue
		}
		seen[t] = true
		out = append(out, t)
	}
	// A separator-only string means "no variants", the same as an empty one.
	// Returning an empty-but-non-nil slice here would give the field two
	// spellings for one meaning, and the loader branches on that field.
	if len(out) == 0 {
		return nil
	}
	return out
}

func atoi(s string) int {
	n, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil {
		return 0
	}
	return n
}
