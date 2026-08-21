# Development

Guide for building, testing, and contributing to this GitHub Action.

<br/>

## Prerequisites

- Go 1.26+
- Docker
- Make

<br/>

## Build

```bash
make build           # Build binary → ./agent-fanout
make clean           # Remove build artifacts
```

<br/>

## Testing

```bash
make test            # Run unit tests (alias)
make test-unit       # go test ./internal/... ./cmd/... -v -cover
make cover           # Generate coverage report
make cover-html      # Open coverage report in browser
```

<br/>

## Docker

Build and test the Docker image locally:

```bash
# Build
docker build -t agent-fanout:local .

# Run against the checked-in fixtures (dry-run: renders, posts nothing)
docker run --rm \
  -e INPUT_RESULTS_DIR="/fixtures" \
  -e INPUT_VARIANTS="conservative,refactor,minimal-diff" \
  -e INPUT_DRY_RUN="true" \
  -v "$PWD/tests/fixtures/results:/fixtures:ro" \
  agent-fanout:local
```

Two fixtures exist and three variants are declared, so the expected output is
`attempt_count=3 success_count=2 missing_count=1` with `minimal-diff` rendered
as a `missing` row. A run that prints two rows is the regression this repository
exists to prevent.

<br/>

## Workflow

```bash
make check-gh        # Verify gh CLI is installed and authenticated
make branch name=feature-name   # Create feature branch from main
make pr title="feat: add feature"   # Test → push → create PR
```

<br/>

## Action Testing

Test the action locally using [act](https://github.com/nektos/act) or by pushing to a branch and using `uses: ./`:

```yaml
- name: Test Local Action
  uses: ./
  with:
    output_file: output.txt
    dry_run: 'true'
```

<br/>

## CI/CD Workflows

| Workflow | Trigger | Description |
|----------|---------|-------------|
| `ci.yml` | push (main), PR, dispatch | Unit tests → Docker build → Action integration test |
| `release.yml` | tag push `v*` | GitHub release + major tag update (v1) |
| `changelog-generator.yml` | after release, PR merge | Auto-generate CHANGELOG.md |
| `contributors.yml` | after changelog | Auto-generate CONTRIBUTORS.md |
| `stale-issues.yml` | daily cron | Auto-close stale issues |
| `dependabot-auto-merge.yml` | PR (dependabot) | Auto-merge minor/patch updates |
| `issue-greeting.yml` | issue opened | Welcome message |

### Workflow Chain

```
tag push v* → Create release + update major tag (v1)
                └→ Generate changelog
                      └→ Generate Contributors
```

<br/>

## Release

```bash
git tag v1.0.0
git push origin v1.0.0
```

The release workflow automatically:
1. Creates a GitHub release with notes
2. Updates the major version tag (e.g., `v1` → points to `v1.0.0`)

Users can then reference the action as `uses: somaz94/agent-fanout@v1`.

<br/>

## Conventions

- **Commits**: Conventional Commits (`feat:`, `fix:`, `docs:`, `refactor:`, `test:`, `ci:`, `chore:`)
- **paths-ignore**: CI skips `.github/workflows/**` and `**/*.md` changes
