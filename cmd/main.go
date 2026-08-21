package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"

	"github.com/somaz94/agent-fanout/internal/config"
	"github.com/somaz94/agent-fanout/internal/gh"
	"github.com/somaz94/agent-fanout/internal/output"
	"github.com/somaz94/agent-fanout/internal/report"
	"github.com/somaz94/agent-fanout/internal/result"
)

func main() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		output.LogWarning("Received shutdown signal, cleaning up...")
		cancel()
	}()

	if err := run(ctx); err != nil {
		output.LogError(err.Error())
		os.Exit(1)
	}
}

func run(ctx context.Context) error {
	cfg := config.Load()

	attempts, err := result.Load(cfg.ResultsDir, cfg.ExpectedVariants)
	if err != nil {
		return fmt.Errorf("load results: %w", err)
	}
	if len(attempts) == 0 {
		// Nothing requested and nothing found. Failing here would be the right
		// call only if the caller meant to fan out; it did not say so, and a
		// step that fails with no table is less informative than one that says
		// exactly this.
		return fmt.Errorf("no attempt results found in %q and no variants were declared", cfg.ResultsDir)
	}

	sum := result.Summarise(attempts)
	body := report.Render(cfg.Title, cfg.Issue, attempts)

	output.LogInfo(fmt.Sprintf("Attempts: %d (succeeded %d, failed %d, missing %d)",
		sum.Total, sum.Succeeded, sum.Failed, sum.Missing))
	for _, a := range attempts {
		if a.Status == result.StatusMissing {
			// Surfaced as a warning as well as a table row: a missing attempt
			// is the one outcome a reader can mistake for "we only asked for
			// two", and the annotation puts it on the run's summary page.
			output.LogWarning(fmt.Sprintf("variant %q reported no result", a.Variant))
		}
	}

	if err := writeStepSummary(body); err != nil {
		output.LogWarning(fmt.Sprintf("Failed to write job summary: %v", err))
	}

	commentURL := ""
	switch {
	case cfg.DryRun:
		output.LogInfo("Dry run - comparison not posted:")
		fmt.Println(body)
	case cfg.Issue <= 0:
		output.LogInfo("No issue number supplied - job summary written, no comment posted")
	default:
		select {
		case <-ctx.Done():
			return fmt.Errorf("cancelled before posting comment")
		default:
		}

		client, err := gh.New(cfg.Repository, cfg.Token)
		if err != nil {
			return fmt.Errorf("github client: %w", err)
		}
		url, updated, err := client.UpsertComment(ctx, cfg.Issue, report.MarkerFor(cfg.Issue), body)
		if err != nil {
			return fmt.Errorf("post comparison: %w", err)
		}
		commentURL = url
		verb := "Posted"
		if updated {
			verb = "Updated"
		}
		output.LogInfo(fmt.Sprintf("%s comparison comment on issue #%d", verb, cfg.Issue))
	}

	setOutputs(map[string]string{
		"comparison":    body,
		"attempt_count": strconv.Itoa(sum.Total),
		"success_count": strconv.Itoa(sum.Succeeded),
		"failed_count":  strconv.Itoa(sum.Failed),
		"missing_count": strconv.Itoa(sum.Missing),
		"comment_url":   commentURL,
	})

	output.LogInfo("Action completed successfully")
	return nil
}

func setOutputs(kv map[string]string) {
	for k, v := range kv {
		if err := output.SetOutput(k, v); err != nil {
			output.LogWarning(fmt.Sprintf("Failed to set %s output: %v", k, err))
		}
	}
}

// writeStepSummary appends the comparison to the run's summary page. It is
// written even in dry run and even with no issue number, because it is the one
// place the table is guaranteed to be readable without a write to the repo.
func writeStepSummary(body string) error {
	path := os.Getenv("GITHUB_STEP_SUMMARY")
	if path == "" {
		return nil
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	// The HTML marker is stripped: it exists so a re-run can find its own
	// comment, and the summary page is not a thing this action reads back.
	clean := strings.TrimSpace(stripMarker(body))
	_, err = fmt.Fprintf(f, "%s\n", clean)
	return err
}

func stripMarker(body string) string {
	lines := strings.Split(body, "\n")
	out := lines[:0]
	for _, l := range lines {
		if strings.HasPrefix(strings.TrimSpace(l), "<!-- somaz94/agent-fanout") {
			continue
		}
		out = append(out, l)
	}
	return strings.Join(out, "\n")
}
