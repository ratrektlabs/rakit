# Contributing to rakit

Thanks for your interest in contributing! This document outlines how to get
your changes reviewed and merged.

## Ground rules

- Open issues before large changes so we can align on scope.
- Keep pull requests focused: one feature or fix per PR.
- Follow Go conventions (`gofmt`, `go vet`, no exported symbol without a doc
  comment).
- Add or update tests for any behaviour change.
- Do not commit secrets. `.env` files and credentials are not accepted.

## Local development

Requirements: Go 1.25+.

```bash
git clone https://github.com/ratrektlabs/rakit.git
cd rakit
make test        # run tests
make test-race   # run tests with the race detector
make build       # build all packages
make vet         # go vet
```

To run the example local server:

```bash
export OPENAI_API_KEY=sk-...     # or GEMINI_API_KEY
make run-local
```

Then open http://localhost:8080.

## Testing

- Unit tests live next to the code in `_test.go` files.
- We run `go test -race -count=1 ./...` in CI. If your change races, CI will
  fail.
- Integration tests that touch external services (OpenAI, Firestore, etc.) must
  be skipped when the corresponding env var is absent — look at existing
  `mcp/client_test.go` for the pattern (uses `httptest`).

## Pull requests

1. Fork the repo and create a branch off `main`.
2. Make your change with tests.
3. Run `make test-race vet` locally.
4. Open the PR against `main`. CI must pass before merge.
5. A maintainer will review. Expect to iterate — comments are a chance to
   improve the change, not a rejection.

## Commit messages

- Use the imperative mood ("add", "fix", "update").
- Keep the subject line under ~72 characters.
- Describe the *why*, not the *what*, in the body. Link issues with
  `Fixes #123` or `Refs #123`.

## Code of conduct

By participating you agree to abide by the [Code of
Conduct](./CODE_OF_CONDUCT.md).
