package result

import (
	"os"
	"path/filepath"
	"testing"
)

// write drops a result file into an artifact-shaped subdirectory, matching what
// actions/download-artifact produces: one directory per artifact.
func write(t *testing.T, root, artifact, name, body string) {
	t.Helper()
	dir := filepath.Join(root, artifact)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
}

func ok(variant string) string {
	return `{"variant":"` + variant + `","status":"success","pr_number":7,` +
		`"pr_url":"https://example.test/pull/7","files_changed":3,` +
		`"additions":10,"deletions":2,"duration_seconds":90,"test_status":"passed"}`
}

// A variant that uploaded nothing must still occupy a row. Dropping it makes a
// three-way fan-out that lost one attempt indistinguishable from a two-way one
// that was never asked for a third — and the second reading is the one that
// costs money silently.
func TestMissingVariantStillGetsARow(t *testing.T) {
	root := t.TempDir()
	write(t, root, "attempt-a", "result.json", ok("a"))

	got, err := Load(root, []string{"a", "b", "c"})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("want 3 rows, got %d: %+v", len(got), got)
	}
	for _, want := range []struct {
		idx    int
		name   string
		status string
	}{{0, "a", StatusSuccess}, {1, "b", StatusMissing}, {2, "c", StatusMissing}} {
		if got[want.idx].Variant != want.name || got[want.idx].Status != want.status {
			t.Errorf("row %d = %q/%q, want %q/%q",
				want.idx, got[want.idx].Variant, got[want.idx].Status, want.name, want.status)
		}
	}
}

// Order follows the requested variant list and nothing measured. Sorting by
// diff size would put the smallest change first in a table a human reads
// top-down, which reads as a recommendation the action is not entitled to make.
func TestOrderFollowsTheRequestedVariantsNotAnyMeasurement(t *testing.T) {
	root := t.TempDir()
	// "big" is written first on disk and has the largest diff; "small" the
	// smallest. Neither fact may influence the row order.
	write(t, root, "z", "r.json", `{"variant":"big","status":"success","files_changed":90,"additions":900}`)
	write(t, root, "a", "r.json", `{"variant":"small","status":"success","files_changed":1,"additions":1}`)

	got, err := Load(root, []string{"big", "small"})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got[0].Variant != "big" || got[1].Variant != "small" {
		t.Fatalf("order = %q,%q; want big,small (the requested order)", got[0].Variant, got[1].Variant)
	}
}

// A file that parses to a status nobody defined must not become a fourth
// category: the renderer switches on these strings, and an unrecognised one
// would fall through to a cell with no legend entry.
func TestUnknownStatusIsCoercedNotPassedThrough(t *testing.T) {
	root := t.TempDir()
	write(t, root, "a", "r.json", `{"variant":"a","status":"partially-ok"}`)

	got, err := Load(root, []string{"a"})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got[0].Status != StatusUnknown {
		t.Fatalf("status = %q, want %q", got[0].Status, StatusUnknown)
	}
}

// Status matching is case-insensitive on read, because the workflow writes it
// with jq from shell variables and "Success" is an easy thing to end up with.
func TestStatusIsMatchedCaseInsensitively(t *testing.T) {
	root := t.TempDir()
	write(t, root, "a", "r.json", `{"variant":"a","status":"  SUCCESS "}`)

	got, _ := Load(root, []string{"a"})
	if got[0].Status != StatusSuccess {
		t.Fatalf("status = %q, want %q", got[0].Status, StatusSuccess)
	}
}

// A malformed file is a finding, not a non-event: it is reported as a failed
// row carrying the parse error, keyed by its filename so it can be found.
func TestUnparseableFileBecomesAFailedRowWithItsError(t *testing.T) {
	root := t.TempDir()
	write(t, root, "attempt-broken", "conservative.json", `{not json`)

	got, err := Load(root, nil)
	if err != nil {
		t.Fatalf("Load returned an error for a malformed file; it should report a row: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("want 1 row, got %d", len(got))
	}
	if got[0].Status != StatusFailed {
		t.Errorf("status = %q, want %q", got[0].Status, StatusFailed)
	}
	if got[0].LoadError == "" {
		t.Error("LoadError is empty; the reason the file could not be read must survive to the report")
	}
	if got[0].Variant != "conservative" {
		t.Errorf("variant = %q, want the filename stem %q", got[0].Variant, "conservative")
	}
}

// An absent directory yields missing rows rather than an error. A failed step
// says nothing about WHICH attempts were expected; a table of missing rows does.
func TestAbsentResultsDirYieldsMissingRowsNotAnError(t *testing.T) {
	got, err := Load(filepath.Join(t.TempDir(), "never-created"), []string{"a", "b"})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(got) != 2 || got[0].Status != StatusMissing || got[1].Status != StatusMissing {
		t.Fatalf("want two missing rows, got %+v", got)
	}
}

// A result whose variant was never requested means the workflow and this action
// disagree about the variant list — worth a row, not a silent discard.
func TestUnrequestedVariantIsAppendedNotDropped(t *testing.T) {
	root := t.TempDir()
	write(t, root, "a", "r.json", ok("a"))
	write(t, root, "x", "r.json", ok("surprise"))

	got, err := Load(root, []string{"a"})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("want 2 rows, got %d: %+v", len(got), got)
	}
	if got[1].Variant != "surprise" {
		t.Fatalf("row 1 = %q, want the unrequested variant appended", got[1].Variant)
	}
}

// Two files claiming the same variant is a workflow bug. Keeping the last one
// read makes the outcome depend on filesystem walk order, so the first wins and
// the result is reproducible.
func TestDuplicateVariantKeepsTheFirstDeterministically(t *testing.T) {
	root := t.TempDir()
	write(t, root, "a-first", "r.json", `{"variant":"dup","status":"success","files_changed":1}`)
	write(t, root, "b-second", "r.json", `{"variant":"dup","status":"failed","files_changed":99}`)

	for i := 0; i < 5; i++ {
		got, err := Load(root, []string{"dup"})
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		if len(got) != 1 || got[0].Files != 1 {
			t.Fatalf("run %d: want the first-walked file (files=1), got %+v", i, got)
		}
	}
}

// Only *.json is read. The attempt jobs also upload logs and patches beside the
// result, and feeding those to the parser would manufacture failed rows.
func TestNonJSONFilesAreIgnored(t *testing.T) {
	root := t.TempDir()
	write(t, root, "a", "result.json", ok("a"))
	write(t, root, "a", "agent.log", "some log output")
	write(t, root, "a", "changes.patch", "diff --git a/x b/x")

	got, err := Load(root, nil)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("want 1 row, got %d: %+v", len(got), got)
	}
}

// Missing and failed are counted separately. They are fixed in different places
// — a missing attempt is a harness fault, a failed one is an agent fault — so a
// combined count sends the reader to the wrong one.
func TestSummariseCountsMissingSeparatelyFromFailed(t *testing.T) {
	got := Summarise([]Attempt{
		{Status: StatusSuccess},
		{Status: StatusFailed},
		{Status: StatusMissing},
		{Status: StatusNoChanges},
	})
	want := Summary{Total: 4, Succeeded: 1, Failed: 1, Missing: 1}
	if got != want {
		t.Fatalf("Summarise = %+v, want %+v", got, want)
	}
}

// no-changes is not a success: the agent ran and produced nothing to review.
func TestNoChangesIsNotCountedAsSuccess(t *testing.T) {
	if (Attempt{Status: StatusNoChanges}).Succeeded() {
		t.Fatal("no-changes reported itself as a success")
	}
}

// Every defined status round-trips, and only those. Adding a constant without
// teaching normaliseStatus about it silently files it as unknown.
func TestNormaliseStatusCoversEveryDefinedStatus(t *testing.T) {
	for _, s := range []string{StatusSuccess, StatusNoChanges, StatusFailed, StatusMissing} {
		if got := normaliseStatus(s); got != s {
			t.Errorf("normaliseStatus(%q) = %q, want it unchanged", s, got)
		}
	}
	for _, s := range []string{"", "done", "ok"} {
		if got := normaliseStatus(s); got != StatusUnknown {
			t.Errorf("normaliseStatus(%q) = %q, want %q", s, got, StatusUnknown)
		}
	}
}

// A results path that is a FILE is a caller mistake worth an error: walking it
// would find nothing and report every variant missing, which points the reader
// at the agent jobs instead of at their own `with:` block.
func TestAResultsPathThatIsAFileIsAnError(t *testing.T) {
	f := filepath.Join(t.TempDir(), "notadir.json")
	if err := os.WriteFile(f, []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(f, []string{"a"}); err == nil {
		t.Fatal("a file was accepted as the results directory")
	}
}

// With nothing requested and nothing found the caller gets an empty set rather
// than an error; main turns that into its own message naming the directory.
func TestEmptyDirWithNoExpectedVariantsYieldsNoRows(t *testing.T) {
	got, err := Load(t.TempDir(), nil)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("want no rows, got %+v", got)
	}
}

// Found-but-unrequested rows are sorted by name so a run is reproducible.
func TestUnrequestedRowsAreSortedByName(t *testing.T) {
	root := t.TempDir()
	write(t, root, "1", "r.json", ok("zeta"))
	write(t, root, "2", "r.json", ok("alpha"))

	got, err := Load(root, nil)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got[0].Variant != "alpha" || got[1].Variant != "zeta" {
		t.Fatalf("order = %q,%q; want alpha,zeta", got[0].Variant, got[1].Variant)
	}
}
