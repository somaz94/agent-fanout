package config

import (
	"reflect"
	"testing"
)

// The variant list is written as a single-line comma list for two variants and
// as a multi-line YAML scalar for more, so both have to parse.
func TestVariantListAcceptsCommasAndNewlines(t *testing.T) {
	cases := map[string][]string{
		"a,b,c":       {"a", "b", "c"},
		"a, b , c":    {"a", "b", "c"},
		"a\nb\nc":     {"a", "b", "c"},
		"a\r\nb":      {"a", "b"},
		"a,\n b,\n":   {"a", "b"},
		"":            nil,
		"   ":         nil,
		"\n,\n , ,\n": nil,
		"only-one":    {"only-one"},
	}
	for in, want := range cases {
		if got := splitList(in); !reflect.DeepEqual(got, want) {
			t.Errorf("splitList(%q) = %#v, want %#v", in, got, want)
		}
	}
}

// A non-numeric issue input must not become issue 0 by accident in a way the
// caller cannot tell from "no issue supplied" — both mean "do not comment", and
// that equivalence is the behaviour being pinned.
func TestNonNumericIssueBecomesZero(t *testing.T) {
	for _, in := range []string{"", "abc", "#12", " "} {
		if got := atoi(in); got != 0 {
			t.Errorf("atoi(%q) = %d, want 0", in, got)
		}
	}
	if got := atoi(" 42 "); got != 42 {
		t.Errorf("atoi(\" 42 \") = %d, want 42", got)
	}
}

func TestLoadAppliesDefaults(t *testing.T) {
	t.Setenv("INPUT_RESULTS_DIR", "")
	t.Setenv("INPUT_TITLE", "")
	t.Setenv("INPUT_DRY_RUN", "")
	t.Setenv("GITHUB_REPOSITORY", "somaz94/agent-fanout")
	t.Setenv("INPUT_REPOSITORY", "")

	cfg := Load()
	if cfg.ResultsDir != "results" {
		t.Errorf("ResultsDir = %q, want the default", cfg.ResultsDir)
	}
	if cfg.Title == "" {
		t.Error("Title has no default")
	}
	if cfg.DryRun {
		t.Error("DryRun defaulted to true")
	}
	if cfg.Repository != "somaz94/agent-fanout" {
		t.Errorf("Repository = %q, want the GITHUB_REPOSITORY fallback", cfg.Repository)
	}
}

// dry_run arrives as a string from YAML and 'True' is an ordinary thing to
// write; matching only the lowercase literal would silently post a comment on a
// run the caller believed was a preview.
func TestDryRunIsCaseInsensitive(t *testing.T) {
	for _, v := range []string{"true", "TRUE", "True"} {
		t.Setenv("INPUT_DRY_RUN", v)
		if !Load().DryRun {
			t.Errorf("INPUT_DRY_RUN=%q did not enable dry run", v)
		}
	}
	for _, v := range []string{"false", "no", ""} {
		t.Setenv("INPUT_DRY_RUN", v)
		if Load().DryRun {
			t.Errorf("INPUT_DRY_RUN=%q wrongly enabled dry run", v)
		}
	}
}

// An explicit repository input overrides the environment, which is what lets
// the action be pointed at another repo from a dispatch workflow.
func TestExplicitRepositoryWinsOverEnvironment(t *testing.T) {
	t.Setenv("GITHUB_REPOSITORY", "env/one")
	t.Setenv("INPUT_REPOSITORY", "input/two")
	if got := Load().Repository; got != "input/two" {
		t.Fatalf("Repository = %q, want the explicit input", got)
	}
}

// A repeated variant must collapse. The loader consumes each expected name
// once, so a duplicate finds nothing left and is reported MISSING — which
// invents a harness failure that never happened, in the one signal the whole
// comparison has to be trusted about.
func TestDuplicateVariantsCollapseInsteadOfBecomingPhantomMissingRows(t *testing.T) {
	cases := map[string][]string{
		"a,b,a":     {"a", "b"},
		"a,a,a":     {"a"},
		"a, b , a ": {"a", "b"},
		"a\nb\na":   {"a", "b"},
	}
	for in, want := range cases {
		if got := splitList(in); !reflect.DeepEqual(got, want) {
			t.Errorf("splitList(%q) = %#v, want %#v", in, got, want)
		}
	}
}

// The FIRST occurrence wins, so the order a caller wrote is the order rendered.
func TestDedupeKeepsTheFirstOccurrenceOrder(t *testing.T) {
	got := splitList("refactor,conservative,refactor,minimal")
	want := []string{"refactor", "conservative", "minimal"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("splitList = %#v, want %#v", got, want)
	}
}
