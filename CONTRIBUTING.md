# Contributing to yoli

Thanks for your interest in contributing! yoli is a small, provider-agnostic
coding-agent CLI written in Go. This guide covers how to get set up, the
expectations for changes, and how to submit them.

## Getting started

You'll need [Go 1.23](https://go.dev/dl/) or newer.

```bash
git clone https://github.com/cpotech/yoli.git
cd yoli
go build -o yoli ./cmd/yoli
./yoli version
```

See the [README](README.md) for the project layout and the [docs/](docs/)
directory for architecture, providers, configuration, and skills.

## Development workflow

1. Fork the repository and create a branch off `main`.
2. Make your change, keeping it focused — one logical change per pull request.
3. Add or update tests for any behavior you change.
4. Make sure the full suite passes and the code is formatted.
5. Open a pull request describing the change and why it's needed.

## Testing and formatting

Before opening a pull request, please run:

```bash
go test ./...   # all tests must pass
go vet ./...    # static checks
gofmt -l .      # should print nothing; run `gofmt -w .` to fix
```

New features and bug fixes should come with tests. Tests live alongside the
code they cover using the standard `*_test.go` convention.

## Coding guidelines

- Follow standard Go conventions and keep `gofmt`/`go vet` clean.
- Keep changes minimal and focused; avoid unrelated refactors.
- Match the style and structure of the surrounding code.
- `internal/` keeps every package unimportable from outside the module —
  put implementation there and keep the public surface in `cmd/`.
- Prefer clear names and small functions over comments, but comment any
  logic that isn't self-evident.

## Commit messages

Write clear, descriptive commit messages. A short imperative subject line
(e.g. "Add Grep tool timeout") followed by an optional body explaining the
"why" works well.

## Reporting bugs and requesting features

Please use the GitHub issue tracker. Include enough detail to reproduce
bugs (command, expected vs. actual behavior, environment).

## Reporting security issues

Please do **not** open public issues for security vulnerabilities. Instead,
report them privately to the maintainers so they can be addressed before
public disclosure.

## License

By contributing, you agree that your contributions will be licensed under
the [MIT License](LICENSE) that covers this project.
