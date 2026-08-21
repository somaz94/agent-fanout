package report

import (
	"strings"
	"testing"

	"github.com/somaz94/agent-fanout/internal/result"
)

func success(v string) result.Attempt {
	return result.Attempt{
		Variant: v, Status: result.StatusSuccess, PRNumber: 12,
		PRURL: "https://example.test/pull/12", Files: 4,
		Additions: 100, Deletions: 20, Duration: 125, Tests: "passed",
	}
}

// The marker is what lets a re-run UPDATE its own comment. Without it three
// re-runs leave three tables on the issue with nothing saying which is current.
func TestRenderCarriesTheUpdateMarker(t *testing.T) {
	got := Render("T", 42, []result.Attempt{success("a")})
	if !strings.Contains(got, MarkerFor(42)) {
		t.Fatal("rendered body carries no marker; a re-run cannot find its own comment")
	}
}

// The marker is scoped per issue so two fan-outs tracked on one thread do not
// overwrite each other's comment.
func TestMarkerIsScopedPerIssue(t *testing.T) {
	if MarkerFor(1) == MarkerFor(2) {
		t.Fatal("two issues share a marker; the second fan-out would overwrite the first")
	}
	if MarkerFor(0) != Marker {
		t.Fatalf("MarkerFor(0) = %q, want the unscoped marker", MarkerFor(0))
	}
}

// A non-success row must not print 0 in the numeric columns. "0 files changed"
// and "we have no figure" are opposite claims, and the 0 is the more convincing
// wrong answer in a column a reader scans for the smallest diff.
func TestNonSuccessRowsRenderAnEmDashNotZero(t *testing.T) {
	body := Render("T", 1, []result.Attempt{
		{Variant: "gone", Status: result.StatusMissing},
		{Variant: "broke", Status: result.StatusFailed},
	})
	for _, line := range strings.Split(body, "\n") {
		if !strings.HasPrefix(line, "| `gone`") && !strings.HasPrefix(line, "| `broke`") {
			continue
		}
		if strings.Contains(line, "| 0 ") {
			t.Fatalf("non-success row prints a zero figure: %s", line)
		}
		if !strings.Contains(line, "—") {
			t.Fatalf("non-success row has no em dash: %s", line)
		}
	}
}

// The caveat is load-bearing, not boilerplate: rows are in request order, and a
// reader who assumes a table is ranked treats the top row as a recommendation.
func TestRenderStatesThatTheTableIsNotRanked(t *testing.T) {
	got := Render("T", 1, []result.Attempt{success("a"), success("b")})
	if !strings.Contains(got, "not** ranked") {
		t.Fatal("rendered body does not say the table is unranked")
	}
}

// A missing attempt is reported in its own clause, never summed into the failed
// count — the two are fixed in different places.
func TestHeadlineReportsMissingSeparatelyFromFailed(t *testing.T) {
	got := Render("T", 1, []result.Attempt{
		success("a"),
		{Variant: "b", Status: result.StatusFailed},
		{Variant: "c", Status: result.StatusMissing},
	})
	if !strings.Contains(got, "1 failed.") {
		t.Error("headline does not report the failed count on its own")
	}
	if !strings.Contains(got, "1 reported no result at all.") {
		t.Error("headline does not report the missing count on its own")
	}
	if !strings.Contains(got, "1/3 attempts produced a change.") {
		t.Errorf("headline miscounts successes:\n%s", got)
	}
}

// Agent-authored text reaches a table cell. An unescaped pipe splits the row and
// shifts every column after it.
func TestPipesInAgentTextAreEscaped(t *testing.T) {
	got := Render("T", 1, []result.Attempt{
		{Variant: "a|b", Status: result.StatusSuccess, Tests: "2 passed | 1 flaky"},
	})
	for _, line := range strings.Split(got, "\n") {
		if strings.HasPrefix(line, "| `a") {
			if strings.Contains(line, "a|b") {
				t.Fatalf("variant pipe not escaped: %s", line)
			}
			if strings.Count(line, "|")-strings.Count(line, "\\|") != 8 {
				t.Fatalf("row has the wrong number of unescaped cell separators: %s", line)
			}
		}
	}
}

// A note is prose of unbounded length; inside a cell it destroys the alignment
// of the row that most needs reading, so it renders beneath the table.
func TestNotesRenderBelowTheTableNotInsideIt(t *testing.T) {
	long := "the agent could not run the test suite because the fixture directory was absent"
	got := Render("T", 1, []result.Attempt{
		{Variant: "a", Status: result.StatusSuccess, Notes: long},
	})
	for _, line := range strings.Split(got, "\n") {
		if strings.HasPrefix(line, "| `a`") && strings.Contains(line, long) {
			t.Fatal("note was rendered inside the table row")
		}
	}
	if !strings.Contains(got, long) {
		t.Fatal("note did not survive to the report at all")
	}
}

// A parse failure is a harness fault and a note is something the agent said.
// Rendering them in one place lets the first hide behind the second.
func TestLoadErrorIsReportedDistinctlyFromANote(t *testing.T) {
	got := Render("T", 1, []result.Attempt{
		{Variant: "a", Status: result.StatusFailed, LoadError: "parse a.json: unexpected EOF", Notes: "ignore me"},
	})
	if !strings.Contains(got, "result file could not be read") {
		t.Fatal("load error is not called out as a harness fault")
	}
	if strings.Contains(got, "ignore me") {
		t.Fatal("the note displaced the load error")
	}
}

// A missing attempt is explained, not left as a bare row: the reader has to
// know the job died rather than that the agent declined to change anything.
func TestMissingRowExplainsItself(t *testing.T) {
	got := Render("T", 1, []result.Attempt{{Variant: "a", Status: result.StatusMissing}})
	if !strings.Contains(got, "no result artifact was uploaded") {
		t.Fatal("missing row carries no explanation")
	}
}

func TestDurationRendersMinutesAndSeconds(t *testing.T) {
	cases := map[int]string{0: "—", -5: "—", 45: "45s", 125: "2m 05s", 3600: "60m 00s"}
	for secs, want := range cases {
		got := durationCell(result.Attempt{Duration: secs})
		if got != want {
			t.Errorf("durationCell(%d) = %q, want %q", secs, got, want)
		}
	}
}

func TestPRCellDegradesWithoutAURL(t *testing.T) {
	if got := prCell(result.Attempt{PRNumber: 9}); got != "#9" {
		t.Errorf("prCell without url = %q, want #9", got)
	}
	if got := prCell(result.Attempt{}); got != "—" {
		t.Errorf("prCell with no PR = %q, want an em dash", got)
	}
}

// A test status the workflow did not normalise still renders as itself rather
// than being silently mapped onto a tick.
func TestUnrecognisedTestStatusRendersVerbatim(t *testing.T) {
	if got := testsCell(result.Attempt{Tests: "2 of 9 flaky"}); got != "2 of 9 flaky" {
		t.Errorf("testsCell = %q, want the value verbatim", got)
	}
	if got := testsCell(result.Attempt{Tests: "PASS"}); got != "✅" {
		t.Errorf("testsCell(PASS) = %q, want a tick", got)
	}
	if got := testsCell(result.Attempt{}); got != "—" {
		t.Errorf("testsCell(empty) = %q, want an em dash", got)
	}
}

// Every attempt gets exactly one row. A header, a separator and N rows.
func TestEveryAttemptGetsExactlyOneRow(t *testing.T) {
	attempts := []result.Attempt{success("a"), success("b"), {Variant: "c", Status: result.StatusMissing}}
	rows := 0
	for _, line := range strings.Split(table(attempts), "\n") {
		if strings.HasPrefix(line, "| `") {
			rows++
		}
	}
	if rows != len(attempts) {
		t.Fatalf("rendered %d rows for %d attempts", rows, len(attempts))
	}
}

// Every status the loader can produce has a label. A status with no case falls
// through to "unknown", which would render a real outcome as an unreadable one.
func TestEveryStatusHasItsOwnLabel(t *testing.T) {
	seen := map[string]bool{}
	for _, s := range []string{
		result.StatusSuccess, result.StatusNoChanges, result.StatusFailed,
		result.StatusMissing, result.StatusUnknown,
	} {
		label := statusLabel(s)
		if seen[label] {
			t.Errorf("status %q reuses the label %q", s, label)
		}
		seen[label] = true
		if s != result.StatusUnknown && strings.Contains(label, "unknown") {
			t.Errorf("status %q fell through to the unknown label", s)
		}
	}
}

func TestFailedTestsRenderAsACross(t *testing.T) {
	if got := testsCell(result.Attempt{Tests: "failed"}); got != "❌" {
		t.Fatalf("testsCell(failed) = %q, want a cross", got)
	}
}

// no-changes rows also suppress their figures: the agent ran and decided
// nothing needed doing, which is not the same as changing zero files by fault.
func TestNoChangesRowSuppressesItsFigures(t *testing.T) {
	body := Render("T", 1, []result.Attempt{{Variant: "a", Status: result.StatusNoChanges, Files: 0}})
	for _, line := range strings.Split(body, "\n") {
		if strings.HasPrefix(line, "| `a`") && strings.Contains(line, "| 0 ") {
			t.Fatalf("no-changes row printed a zero: %s", line)
		}
	}
}
