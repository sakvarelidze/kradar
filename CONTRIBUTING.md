# Contributing

Thanks for contributing to kradar.

## Local setup

1. Install Go (version in `go.mod`).
2. Run checks before opening a PR:

```bash
make check
```

## CI runner requirements

GitHub Actions CI is configured to run on a self-hosted runner with labels: `self-hosted`, `linux`, and `zerodev`. Ensure these labels exist on the runner; otherwise, workflows will remain queued.

## Pull requests

- Keep changes focused and small.
- Add/adjust tests for behavior changes.
- Update docs for user-facing changes.

## Commit style

Use clear, descriptive commit messages in imperative mood.
