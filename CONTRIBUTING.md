# Contributing to Saga

Thanks for considering a contribution. Saga aims to be small, conventional, and easy to work on.

## Getting started

```bash
git clone https://github.com/mopanc/saga.git
cd saga
go test ./...                # 55+ tests should pass
go run ./cmd/saga doctor     # confirms install state
```

Active development branch: `pivot/v2-go` (until v2 merges to `main`).

## Reporting bugs

Open an issue using the **Bug Report** template. Include:

- Saga version (`saga version`)
- OS + Go version
- Minimal reproduction (commands run, expected vs actual)
- `saga doctor` output

## Suggesting features

Open an issue using the **Feature Request** template. Lead with the problem you're trying to solve, not the implementation.

## Pull requests

1. Fork + branch from `pivot/v2-go`. Branch naming: `feat/...`, `fix/...`, `docs/...`, `chore/...`, `refactor/...`.
2. Write tests for new behaviour.
3. Run `go test ./...` and (if installed) `golangci-lint run`.
4. Use [Conventional Commits](https://www.conventionalcommits.org/) — they drive the auto-changelog and trigger correct semver bumps in releases.
5. Open the PR against `pivot/v2-go`. Fill the PR template.

## Commit conventions

```
type(scope): subject

Body explaining the why, not the what.

[optional footer like "Refs: #123"]
```

Common types: `feat`, `fix`, `docs`, `chore`, `refactor`, `test`, `perf`, `ci`.

Common scopes in this repo: `saga`, `mcp`, `cli`, `docs`, `ci`, `deps`.

## Sign your commits

Signed commits are required on `main` (verified GPG/SSH signature). For development branches it is recommended but not enforced.

## Code of conduct

This project follows the [Contributor Covenant 2.1](./CODE_OF_CONDUCT.md). By participating you agree to its terms.

## Security

For security issues, **do not** open a public issue or pull request. See [SECURITY.md](./SECURITY.md) for the private disclosure process.

## License

By contributing you agree your contributions are licensed under the [Apache License 2.0](./LICENSE).
