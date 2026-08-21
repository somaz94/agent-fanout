# agent-fanout

Run one coding task as **N independent agent attempts**, open a draft PR for each, and get **one comparison table** posted back to the issue.

GitHub's matrix already fans jobs out. What it does not do is fan them back in — so this repository ships two halves:

| Half | What it is | What it does |
|---|---|---|
| `.github/workflows/fanout.yml` | Reusable workflow | Bounds the variant list, runs the agent once per variant in its own branch, opens a draft PR per attempt |
| `somaz94/agent-fanout@v1` | Docker action | Reads every attempt's result, renders the comparison, posts or updates one comment |

You can use the action on its own if you already have a fan-out of your own.

<br/>

## 🔴 Read this before you wire it up

**Every variant is a full agent run, and the cost multiplies by the variant count.** Three variants is three times the work of one.

Two credentials are accepted and you supply exactly one — the workflow refuses both-or-neither before a runner starts:

| Secret | What it draws on |
|---|---|
| `anthropic_api_key` | Metered API credit, billed per token |
| `claude_code_oauth_token` | A Claude subscription's limits |

Bedrock, Vertex and Foundry are also reachable through `claude-code-action` if you wire them in the caller.

Three things exist to keep that honest:

- `max_variants` is a **hard ceiling** checked in its own job, before a runner starts an agent. Exceeding it fails the run having spent nothing. It refuses a non-integer rather than skipping the check — written as a bare `[ n -gt "$MAX" ]` the comparison exits 2 on `3.5` or on an empty value, and inside an `if` that reads as *false*, so the ceiling silently vanished.
- `dry_run: true` walks the entire pipeline — plan, branch, measure, result file, comparison comment — **without calling the agent**. Run it once first.
- The trigger in the example is a **label**, not an issue comment. Adding a label needs write access; filing an issue does not.

<br/>

## Quick start

Copy [`examples/fanout-on-label.yml`](examples/fanout-on-label.yml) into your repository, add `ANTHROPIC_API_KEY` to its secrets, then label an issue `fanout`.

```yaml
jobs:
  fanout:
    uses: somaz94/agent-fanout/.github/workflows/fanout.yml@v1
    permissions:
      contents: write
      issues: write
      pull-requests: write
    with:
      issue: ${{ github.event.issue.number }}
      task: ${{ github.event.issue.body }}
      variants: conservative,refactor,minimal-diff
      test_command: make test
    secrets:
      anthropic_api_key: ${{ secrets.ANTHROPIC_API_KEY }}
```

<br/>

## What a variant is

A variant is **a different instruction to the same agent**, not a different agent. Three ship by name:

| Variant | Instruction |
|---|---|
| `conservative` | Smallest change that solves it. No restructuring, no renames, match the surrounding style. |
| `refactor` | Solve it and leave the touched code better. Restructuring adjacent code is in scope. |
| `minimal-diff` | Optimise for review effort — fewest lines a reviewer can verify from the diff alone. |

Any other name gets a neutral instruction and is asked to state its approach in the PR body, so you can add your own without editing the workflow.

<br/>

## The comparison

```
## Agent fan-out for #42

**2/3 attempts produced a change.** 1 reported no result at all.

| Variant | Status | PR | Files | +/- | Tests | Duration |
|---|---|---|---:|---:|---|---:|
| `conservative` | ✅ success | [#101](…) | 2 | +18 / −4 | ✅ | 3m 34s |
| `refactor` | ✅ success | [#102](…) | 9 | +260 / −143 | ❌ | 10m 31s |
| `minimal-diff` | ⚠️ missing | — | — | — | — | — |

<details><summary>Notes</summary>

- `minimal-diff` — no result artifact was uploaded; the job likely failed before finishing.

</details>

> Rows are in the order the variants were requested — this table is **not** ranked. A smaller diff is not the same as a better change; open the PRs and decide.
```

Four decisions are visible in that output, and each is there because the alternative fails silently:

- **A variant that reported nothing still gets a row.** Drop it and a three-way run that lost an attempt is indistinguishable from a two-way run — after you paid for three. This is why the action takes the requested `variants` list and not just a directory.
- **Missing is counted separately from failed.** A missing attempt is a harness fault, a failed one is an agent fault. They are fixed in different places, so summing them sends you to the wrong one.
- **Non-success rows print `—`, never `0`.** "Changed 0 files" and "we have no figure" are opposite claims, and the `0` is the more convincing wrong answer in a column you scan for the smallest diff.
- **Nothing is ranked and the table says so.** Ordering by diff size would put the smallest change on top of a list read top-down, which reads as a recommendation. Smallest is not best.

<br/>

## Reusable workflow inputs

| Input | Default | Description |
|---|---|---|
| `issue` | *(required)* | Issue the task came from; the comparison is posted here |
| `task` | `''` | What the agent should do. Falls back to the issue body |
| `variants` | `conservative,refactor,minimal-diff` | Comma-separated variant names |
| `max_variants` | `3` | Hard ceiling, enforced before any agent runs |
| `base_ref` | *(default branch)* | Branch the attempts fork from |
| `model` | `''` | Passed through to Claude Code |
| `test_command` | `''` | Run in each worktree after the agent; its exit status becomes the Tests column |
| `timeout_minutes` | `30` | Per-attempt timeout |
| `dry_run` | `false` | Walk the pipeline without invoking the agent or opening PRs |

Secret: `anthropic_api_key` — not required when `dry_run` is true.

<br/>

## Action inputs

Use these directly if you fan out your own way.

| Input | Default | Description |
|---|---|---|
| `results_dir` | `results` | Directory the attempt artifacts were downloaded into, searched recursively for `*.json` |
| `variants` | `''` | Comma- or newline-separated list the fan-out requested. **Supplying it is what makes a silent attempt visible** |
| `issue` | `''` | Issue to comment on. Omit to write only the job summary |
| `repository` | `$GITHUB_REPOSITORY` | `owner/name` |
| `github_token` | `${{ github.token }}` | Needs `issues: write` |
| `title` | `Agent fan-out results` | Heading of the comparison |
| `dry_run` | `false` | Render and print without posting |

<br/>

### Outputs

| Output | Description |
|---|---|
| `comparison` | The rendered Markdown |
| `attempt_count` | Rows in the comparison, including ones that reported nothing |
| `success_count` | Attempts that produced a reviewable change |
| `failed_count` | Attempts that reported a failure |
| `missing_count` | Requested variants that uploaded no result |
| `comment_url` | Posted or updated comment URL, empty on dry run |

<br/>

## The result file

Each attempt uploads one JSON artifact named `fanout-result-<variant>`. Write your own if you are not using the bundled workflow:

```json
{
  "variant": "conservative",
  "status": "success",
  "branch": "agent-fanout/issue-42/conservative",
  "pr_number": 101,
  "pr_url": "https://github.com/owner/repo/pull/101",
  "files_changed": 2,
  "additions": 18,
  "deletions": 4,
  "duration_seconds": 214,
  "commit_sha": "1111111…",
  "test_status": "passed",
  "notes": "optional prose, rendered below the table"
}
```

`status` is one of `success`, `no-changes`, `failed`, `missing`. Anything else is rendered as `unknown` rather than passed through — the renderer switches on these strings, and an unrecognised one would land in a cell with no legend entry.

Write the file under `if: always()`. An attempt that crashed and uploaded nothing is reported as `missing`, which is a *harness* fault — you want it reported as `failed`, which is an agent fault.

<br/>

## Development

```bash
make build    # build the binary
make test     # unit tests with coverage
make fmt vet  # format and vet
```

Tests are written to **fail when the fix is reverted**, and were checked that way. Add cases the same way: break the code, confirm red, then fix.

The CI integration test declares three variants against two fixtures on purpose — it asserts `attempt_count=3`, `success_count=2`, `missing_count=1`, because a comparison that silently shrinks to the attempts that survived is the failure this action exists to prevent.

<br/>

## Licence

MIT
