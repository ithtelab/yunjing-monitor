# Contributing

Thank you for helping improve 云镜监控 / Yunjing Monitor.

## Before contributing

1. Search existing issues and pull requests first.
2. Use an issue to discuss large features, schema changes, protocol changes, or user-interface rewrites before implementation.
3. Never submit credentials, production data, private monitoring endpoints, or generated backup files.
4. Review [OPEN_SOURCE_READINESS.md](OPEN_SOURCE_READINESS.md). Code redistribution remains blocked until the inherited frontend licensing question is resolved.

## Development checks

Backend:

```bash
go test ./...
go vet ./...
```

Frontend:

```bash
cd web
npm ci
npm run check
```

Before opening a pull request, also verify that installation scripts parse successfully and that no secret is present:

```bash
go run github.com/zricethezav/gitleaks/v8@v8.30.1 git . --no-banner --redact --log-opts="--all"
```

## Pull requests

- Keep each pull request focused on one change.
- Add tests for backend behavior and utility logic where practical.
- Update structured release notes for user-visible changes.
- Describe migration, rollback, compatibility, and responsive-layout impact.
- Preserve JSON and SQLite compatibility unless a documented migration is included.

By submitting a contribution, you confirm that you have the right to submit it and that it does not include third-party code without compatible permission.
