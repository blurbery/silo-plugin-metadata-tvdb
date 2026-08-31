# Contributing to the TVDB Metadata Plugin

The [Silo contribution guide](https://github.com/Silo-Server/.github/blob/main/CONTRIBUTING.md)
covers project-wide coordination, focused changes, evidence, AI disclosure, and
pull request expectations. Those requirements apply here; this guide adds the
plugin-specific workflow.

## Before you start

Open an [issue](https://github.com/Silo-Server/silo-plugin-metadata-tvdb/issues)
before changing matching, metadata mapping, image resolution, configuration, or
the advertised capabilities. This repository owns TVDB provider behavior;
plugin contracts belong in
[`silo-plugin-sdk`](https://github.com/Silo-Server/silo-plugin-sdk), while host
metadata orchestration belongs in
[`silo-server`](https://github.com/Silo-Server/silo-server).

## Development setup

Use the Go version declared in `go.mod`. A local `go.work` may point at a sibling
SDK checkout while developing both repositories, but committed code and CI must
resolve the tagged SDK dependency with `GOWORK=off`. Never commit provider
credentials issued to you, captured private data, or a local filesystem
`replace` directive.

## Validate your change

```sh
GOWORK=off go test ./...
GOWORK=off go vet ./...
GOWORK=off go build ./...
GOWORK=off go run . manifest >/dev/null
gofmt -l .
```

The manifest command must exit successfully. `gofmt -l .` should print nothing;
if it reports unrelated pre-existing drift, none of the Go files touched by your
change may appear in the output. Do not add to the output, and report what
remains. Add focused coverage for matching, field mapping, image paths,
pagination, and upstream error handling when those behaviors change.

## Open the pull request

Use a Conventional Commit title, explain any matching, attribution, or upstream
service risk, and paste the actual validation results. Read the
[AI-assisted contribution policy](https://github.com/Silo-Server/silo-server/blob/main/docs/ai-contributions.md)
and include its disclosure block.
