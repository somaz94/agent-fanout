# CLAUDE.md — agent-fanout

Two halves of one pipeline: a reusable workflow that runs N agent attempts, and a Go Docker action that reports on them.

<br/>

## Build & Test

```bash
make build       # Build binary
make test        # Unit tests with coverage
make cover       # Coverage report
make fmt         # gofmt -s -w .
make vet         # go vet
```

<br/>

## Project Structure

```
cmd/main.go                      # Entry point: load → render → summary → comment
internal/
  config/config.go               # INPUT_* env vars
  result/result.go               # Load the per-attempt JSON artifacts
  report/report.go               # Render the Markdown comparison
  gh/gh.go                       # Minimal REST client — upsert one comment
  output/output.go               # GitHub Actions output helpers
action.yml                       # The fan-IN action
.github/workflows/fanout.yml     # The fan-OUT reusable workflow
examples/                        # Caller workflows a consumer copies
tests/fixtures/results/          # Two fixtures, three declared variants
```

<br/>

## The decisions that fail silently

Each of these is enforced by a test named for it. Reverting one produces ordinary-looking output, not an error.

- **A requested variant that uploaded nothing must still get a row.** This is why the action takes a `variants` list and not just a directory. Drop it and a 3-way run that lost an attempt looks exactly like a 2-way run — after paying for three.
- **`missing` is never summed with `failed`.** Missing is a harness fault, failed is an agent fault. They are fixed in different places.
- **Non-success rows render `—`, never `0`.** "Changed 0 files" and "no figure" are opposite claims, and the `0` is the convincing wrong answer in the column a reader scans.
- **Nothing is ranked, and the caveat line says so.** Ordering by diff size is a recommendation, and smallest ≠ best.
- **A malformed result file becomes a `failed` row carrying its parse error, keyed by filename.** Returning an error instead would lose every other attempt's report.
- **A duplicate variant keeps the FIRST file walked.** Keeping the last makes the outcome depend on filesystem order.
- **The comment carries a per-issue marker so a re-run UPDATES it.** Three re-runs otherwise leave three tables with nothing saying which is current.
- **A failed comment LIST is fatal, not swallowed.** Falling through to "post a new one" appends a duplicate on every transient failure.
- **`max_variants` is checked in its own job**, before a runner starts an agent. Inside the matrix it would spend the tokens first.
- **The issue body is written to a FILE, never interpolated into `run:`.** On a public repo it is attacker-supplied text and `run:` is a script-injection sink.
- **The attempt's result step is `if: always()`** so a crashed attempt reports `failed` rather than vanishing into `missing`.
- **`fail-fast: false` on the matrix.** One attempt failing must not cancel its siblings — comparing the survivors is the point.
- **`compare` runs under `if: always()`.** The comparison is most useful when an attempt failed.

<br/>

## Cost

There is no subscription path in CI — `claude-code-action` takes an API key, Bedrock, Vertex or Foundry. N variants is N× the tokens. `max_variants`, `dry_run` and the label-only trigger are the three guards, and none of them may be softened without saying so in the README.

<br/>

## Conventions

- Go, **stdlib only** — the module has no third-party dependency and adding the first one needs a reason
- Docker action; `action.yml` stays on `image: Dockerfile` until the first release, then moves to `docker://ghcr.io/somaz94/agent-fanout:<version>`
- Coverage ≥ 90% (currently 93%)
- Tests must fail when the fix is reverted — verify by mutation, not by a passing run
- Documentation and code comments in English; single-line Conventional Commit messages
