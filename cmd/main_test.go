package main

import (
	"strings"
	"testing"

	"github.com/somaz94/agent-fanout/internal/report"
	"github.com/somaz94/agent-fanout/internal/result"
)

// The marker exists so a re-run can find its own COMMENT; the job summary is
// never read back, so it must not carry one. This test also pins the coupling:
// the prefix was once re-typed as a literal here, so changing report.Marker
// silently stopped stripping it — with no error and no failing test, only a
// stray HTML comment at the top of the summary.
func TestStepSummaryDropsTheCommentMarker(t *testing.T) {
	body := report.Render("T", 7, []result.Attempt{{Variant: "a", Status: result.StatusSuccess}})
	if !strings.Contains(body, report.MarkerPrefix) {
		t.Fatal("precondition failed: the rendered body carries no marker")
	}
	if got := stripMarker(body); strings.Contains(got, report.MarkerPrefix) {
		t.Fatalf("marker survived into the job summary:\n%s", got)
	}
}

// Stripping the marker must not eat anything else.
func TestStripMarkerLeavesTheRestOfTheBodyIntact(t *testing.T) {
	body := report.Render("Fan-out for #7", 7, []result.Attempt{
		{Variant: "a", Status: result.StatusSuccess, PRNumber: 1, PRURL: "https://x/1"},
	})
	got := stripMarker(body)
	for _, want := range []string{"Fan-out for #7", "| `a` |", "not** ranked"} {
		if !strings.Contains(got, want) {
			t.Errorf("stripMarker removed %q from the body", want)
		}
	}
}
