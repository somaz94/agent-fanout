// Package report renders a loaded attempt set as the Markdown that is posted
// back to the issue and written to the job summary.
package report

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/somaz94/agent-fanout/internal/result"
)

// Marker is embedded in every comment this action posts so a re-run can find
// and UPDATE its previous comment instead of appending a second one. Three
// re-runs of a fan-out otherwise leave three tables on the issue with nothing
// saying which is current, and the newest is at the bottom.
const MarkerPrefix = "<!-- somaz94/agent-fanout"

// Marker is the unscoped form, used when no issue number is known.
const Marker = MarkerPrefix + " -->"

// MarkerFor scopes the marker to one issue so two fan-outs tracked on the same
// thread do not overwrite each other.
func MarkerFor(issue int) string {
	if issue <= 0 {
		return Marker
	}
	return fmt.Sprintf("<!-- somaz94/agent-fanout:issue-%d -->", issue)
}

// Render builds the full comment body for a set of attempts.
func Render(title string, issue int, attempts []result.Attempt) string {
	sum := result.Summarise(attempts)

	var b strings.Builder
	b.WriteString(MarkerFor(issue))
	b.WriteString("\n\n## ")
	b.WriteString(escapeText(title))
	b.WriteString("\n\n")

	b.WriteString(headline(sum))
	b.WriteString("\n\n")
	b.WriteString(table(attempts))
	b.WriteString("\n")

	if notes := noteList(attempts); notes != "" {
		b.WriteString("\n")
		b.WriteString(notes)
	}

	b.WriteString("\n")
	b.WriteString(caveat())
	b.WriteString("\n")
	return b.String()
}

func headline(s result.Summary) string {
	parts := []string{fmt.Sprintf("**%d/%d attempts produced a change.**", s.Succeeded, s.Total)}
	if s.Failed > 0 {
		parts = append(parts, fmt.Sprintf("%d failed.", s.Failed))
	}
	if s.Missing > 0 {
		// Called out separately from Failed and never summed with it: an
		// attempt that never reported is a harness problem, and one that
		// reported a failure is an agent problem. They are fixed in different
		// places, so a combined count sends you to the wrong one.
		parts = append(parts, fmt.Sprintf("%d reported no result at all.", s.Missing))
	}
	return strings.Join(parts, " ")
}

func table(attempts []result.Attempt) string {
	var b strings.Builder
	b.WriteString("| Variant | Status | PR | Files | +/- | Tests | Duration |\n")
	b.WriteString("|---|---|---|---:|---:|---|---:|\n")
	for _, a := range attempts {
		fmt.Fprintf(&b, "| `%s` | %s | %s | %s | %s | %s | %s |\n",
			escapeText(a.Variant),
			statusLabel(a.Status),
			prCell(a),
			numCell(a, a.Files),
			diffCell(a),
			testsCell(a),
			durationCell(a),
		)
	}
	return b.String()
}

// statusLabel keeps the words the JSON uses rather than inventing prettier
// ones, so a reader grepping the workflow for a status finds the same string
// they saw in the table.
func statusLabel(s string) string {
	switch s {
	case result.StatusSuccess:
		return "✅ success"
	case result.StatusNoChanges:
		return "➖ no-changes"
	case result.StatusFailed:
		return "❌ failed"
	case result.StatusMissing:
		return "⚠️ missing"
	default:
		return "❓ unknown"
	}
}

func prCell(a result.Attempt) string {
	if a.PRNumber > 0 && a.PRURL != "" {
		return fmt.Sprintf("[#%d](%s)", a.PRNumber, escapeText(a.PRURL))
	}
	if a.PRNumber > 0 {
		return fmt.Sprintf("#%d", a.PRNumber)
	}
	return "—"
}

// numCell renders an em dash rather than 0 for any attempt that did not
// produce a change. "0 files" and "we have no figure" are opposite claims, and
// a 0 in a column a reader scans for the smallest diff is the more convincing
// wrong answer of the two.
func numCell(a result.Attempt, n int) string {
	if !a.Succeeded() {
		return "—"
	}
	return strconv.Itoa(n)
}

func diffCell(a result.Attempt) string {
	if !a.Succeeded() {
		return "—"
	}
	return fmt.Sprintf("+%d / −%d", a.Additions, a.Deletions)
}

func testsCell(a result.Attempt) string {
	switch strings.ToLower(strings.TrimSpace(a.Tests)) {
	case "passed", "pass", "success":
		return "✅"
	case "failed", "fail", "failure":
		return "❌"
	case "", "skipped", "skip":
		return "—"
	default:
		return escapeText(a.Tests)
	}
}

func durationCell(a result.Attempt) string {
	if a.Duration <= 0 {
		return "—"
	}
	m := a.Duration / 60
	s := a.Duration % 60
	if m == 0 {
		return fmt.Sprintf("%ds", s)
	}
	return fmt.Sprintf("%dm %02ds", m, s)
}

// noteList renders whatever an attempt had to say beneath the table rather than
// inside it. A note is prose of unbounded length and a table cell is not, so
// inlining one pushes every column out of alignment on the row that most needs
// reading.
func noteList(attempts []result.Attempt) string {
	var b strings.Builder
	for _, a := range attempts {
		switch {
		case a.LoadError != "":
			fmt.Fprintf(&b, "- `%s` — result file could not be read: %s\n",
				escapeText(a.Variant), escapeText(a.LoadError))
		case a.Status == result.StatusMissing:
			fmt.Fprintf(&b, "- `%s` — no result artifact was uploaded; the job likely failed before finishing.\n",
				escapeText(a.Variant))
		case strings.TrimSpace(a.Notes) != "":
			fmt.Fprintf(&b, "- `%s` — %s\n", escapeText(a.Variant), escapeText(strings.TrimSpace(a.Notes)))
		}
	}
	if b.Len() == 0 {
		return ""
	}
	return "<details><summary>Notes</summary>\n\n" + b.String() + "\n</details>\n"
}

// caveat is not boilerplate. Rows are ordered by the variant list and by
// nothing else, and a reader who assumes a table is ranked will treat the top
// row as a recommendation. Saying so is cheaper than being misread.
func caveat() string {
	return "> Rows are in the order the variants were requested — this table is **not** ranked. " +
		"A smaller diff is not the same as a better change; open the PRs and decide.\n"
}

// escapeText neutralises every way a string that came from OUTSIDE this package
// can break the document it is rendered into. It must be applied to ALL of them,
// not to the ones that were noticed first — a pipe in a PR URL shifts the
// columns exactly as a pipe in a variant name does.
//
// Newlines are the load-bearing case and the reason this is not only about
// tables: cmd/main.go hands the rendered body to output.SetOutput, which writes
// a multi-line value to GITHUB_OUTPUT as a heredoc terminated by a bare EOF
// line. Agent-authored text carrying such a line closes the heredoc early and
// everything after it is parsed as further key=value pairs — an arbitrary
// step-output injection. Folding every newline to a space ends that.
//
// The backtick is REPLACED rather than escaped because the variant renders
// inside a `code span`, and a backslash inside a code span is a literal
// backslash, not an escape. There is no spelling of an escaped backtick that
// works there.
func escapeText(s string) string {
	r := strings.NewReplacer(
		"|", "\\|",
		"`", "'",
		"\n", " ",
		"\r", " ",
	)
	return r.Replace(s)
}
