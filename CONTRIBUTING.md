# Contributing

Raven Terminal requires Go 1.25 or newer and an OpenGL 4.1 development
environment. See `docs/installation.md` for platform packages.

Before opening a pull request, run:

```sh
make check
go test -race ./src/grid ./src/parser ./src/aitools ./src/shell ./src/websearch ./src/ollama ./src/tab
```

Keep terminal protocol behavior covered by focused parser/grid tests. Changes
that cross package boundaries should include a lifecycle or integration test
where practical. Please explain any security impact involving PTY input,
clipboard access, URL fetching, or AI tools in the pull request.

