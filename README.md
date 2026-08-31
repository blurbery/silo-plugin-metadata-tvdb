# TVDB Metadata Plugin for Silo

First-party [Silo](https://github.com/Silo-Server/silo-server) metadata plugin
backed by TheTVDB. It provides series, season, and episode metadata and resolves
`tvdb://` artwork references.

## Setup

TVDB Metadata is installed as a default Silo plugin. Add or enable **TVDB** in a
television library's metadata provider chain; no plugin-specific configuration
is required.

## Dependency Model

This repository consumes `github.com/Silo-Server/silo-plugin-sdk` as a normal Go module dependency. CI and release builds run with `GOWORK=off` and expect the SDK version in `go.mod` to resolve from a published semver tag.

For local multi-repository development, use a `go.work` file that points at a
sibling SDK checkout. Do not commit machine-local filesystem replacements.

## Development

```sh
go test ./...
go build .
```

## Contributing

Read [CONTRIBUTING.md](CONTRIBUTING.md) before opening a pull request. Matching,
metadata mapping, image resolution, configuration, or advertised capability
changes should start as an issue.

## Attribution

Metadata provided by [TheTVDB](https://thetvdb.com/). Please consider [adding missing information](https://thetvdb.com/) or [subscribing](https://thetvdb.com/subscribe).

<a href="https://thetvdb.com/">
  <img src="https://thetvdb.com/images/attribution/logo1.png" alt="TheTVDB Logo" width="200">
</a>

## License

`silo-plugin-metadata-tvdb` is licensed under `AGPL-3.0-or-later`. See [LICENSE](LICENSE).
