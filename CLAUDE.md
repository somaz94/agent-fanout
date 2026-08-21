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
- **`compare` runs under `if: always()`, but gated on `plan` succeeding.** The comparison is most useful when an attempt failed — yet `download-artifact` does not error on a pattern that matches nothing, so a run the ceiling rejected posted an empty table on top of the error the caller needed to read.
- **Every string that reaches the rendered body from outside the package is escaped, not just the ones noticed first.** A pipe in a PR URL shifts the columns exactly as a pipe in a variant name does. Newlines are the load-bearing case: the body is written to `GITHUB_OUTPUT` as a heredoc, so agent text carrying a bare `EOF` line closes it early and the rest parses as further outputs — arbitrary step-output injection, not merely a broken table.
- **The heredoc delimiter for the prompt output is RANDOM.** A fixed `EOF` lets an issue body escape the block the same way.
- **`prompt:` takes inline TEXT, never a path.** `claude-code-action` has no top-level `prompt_file`, so a path handed to `prompt` becomes the literal prompt — every variant burns a run and returns no-changes.
- **The `max_variants` ceiling refuses a non-integer instead of skipping.** `[ n -gt "$MAX" ]` exits 2 on `3.5` or an empty value, and inside an `if` that reads as *false* — the guard the whole cost story rests on was fail-OPEN.
- **The agent's timeout is on the STEP, never the job.** A job timeout CANCELS, and a cancelled job skips even `if: always()` — so a long-running agent surfaced as `missing`, a harness fault, instead of `failed`.
- **A job that other jobs `needs` must not carry a job-level `if`.** A skipped job propagates its skip: gating the credential check on `!dry_run` would have skipped the entire matrix on exactly the runs meant to exercise it. Branch inside the step instead.
- **The variant list is deduped.** The loader consumes each expected name once, so a repeat is reported `missing` — inventing a harness failure that never happened.
- **Tests run AFTER the index is staged.** `make test` writes coverage files and build output, and a later `git add -A` swept them into the PR and into the very counts the comparison exists to report.
- **`--argjson` parses its value as JSON**, so a non-numeric PR number kills the `always()` step — turning a SUCCEEDED attempt into a `missing` row.
- **Any workflow value containing a `#` must be QUOTED.** YAML reads a `#` preceded by whitespace as a comment, so `title: Agent fan-out for #${{ inputs.issue }}` silently rendered `Agent fan-out for` with the number cut off. actionlint, yamllint and the YAML parser all accept it — the file is valid, it just means something else. Only running it showed the heading.
- **An empty `STARTED` is not caught by `set -u`.** It is set-but-empty, and bash arithmetic reads that as 0, so the duration became the whole epoch and shipped as a plausible figure.

<br/>

## Cost

N variants is N× the work. Two credentials are accepted and exactly one must be supplied — `anthropic_api_key` (metered, per token) or `claude_code_oauth_token` (a Claude subscription's limits); the `credentials` job refuses both-or-neither before a runner starts. An earlier version of this file claimed CI had no subscription path at all, which was simply wrong.

`max_variants`, `dry_run`, the credential check and the label-only trigger are the guards, and none may be softened without saying so in the README.

<br/>

## Releasing

`release.yml` triggers `changelog-generator.yml` and `contributors.yml`, and **both push commits to main**. So the tag you just pushed is followed by main moving underneath you — always `git fetch` and confirm `HEAD == origin/main` before cutting the next one. Skipping that produced a tag pointing at a commit that was not on mainline, because the tag push succeeded while the branch push was rejected: git does not roll one back for the other.

The action stays on `image: Dockerfile`. Converting to `docker://ghcr.io/somaz94/agent-fanout:<version>` is an optimisation worth ~31s per consumer run and needs the converted `release.yml` (delegating to `somaz94/.github`'s reusable workflow) plus `release-docker-action.sh` — not a prerequisite for releasing.

<br/>

## Conventions

- Go, **stdlib only** — the module has no third-party dependency and adding the first one needs a reason
- Docker action; `action.yml` stays on `image: Dockerfile` until the first release, then moves to `docker://ghcr.io/somaz94/agent-fanout:<version>`
- Coverage ≥ 90% (currently 93%)
- Tests must fail when the fix is reverted — verify by mutation, not by a passing run
- Documentation and code comments in English; single-line Conventional Commit messages
