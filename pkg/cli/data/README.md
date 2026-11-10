# data Package

> Embedded data files shared by the `gh-aw` CLI packages.

## Public API

`ModelsJSON` is the package's exported `[]byte` containing the bundled `models.json` catalogue, embedded at build time with `go:embed`. It is keyed by provider and read by CLI packages that need the bundled catalogue.

This package owns the bundled data asset; parsing and model-selection behavior live in the packages that consume it.
