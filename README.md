# TVDB Metadata Plugin for Silo

First-party [Silo](https://github.com/Silo-Server/silo-server) metadata plugin
backed by TheTVDB. It provides series, season, and episode metadata and resolves
`tvdb://` artwork references.

## Setup

TVDB Metadata is installed as a default Silo plugin. Add or enable **TVDB** in a
television library's metadata provider chain. No configuration is required for
direct TVDB access.

### Silo metadata proxy

The plugin's **Configure** tab has a *Silo Metadata Proxy* section. Turning it on
routes every TVDB request through the shared proxy at
`https://metadata.siloserver.org` (or a self-hosted proxy URL you supply). The
proxy caches responses for all Silo installations, so scans reach TVDB far less
often. When the proxy is busy, the plugin waits as long as its `Retry-After`
header asks, up to the request's deadline. Saving the setting reloads the
plugin; no server restart is needed.

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
