# Java for Mayoi

Java source navigation for [Mayoi](https://github.com/sorrycc/Mayoi), powered by
[Eclipse JDT Language Server](https://github.com/eclipse-jdtls/eclipse.jdt.ls).
Press **F12** on a project class, method or Lombok getter to jump to its source.
Mayoi also consumes JDT LS diagnostics, hover and completion.

## Compatibility

**0.3 is a development version.** It requires Mayoi's native language server
resolver, merged into [Mayoi main](https://github.com/sorrycc/Mayoi/tree/main) in
[PR #135](https://github.com/sorrycc/Mayoi/pull/135). Use an editor build containing
that change. Existing Mayoi releases without `contributes.languageServers[].resolve`
cannot run this version. The previous Python-based implementation is available at
[`50768f2`](https://github.com/Adrian-Zephyr-Liao/mayoi-java/tree/50768f2).

Runtime requirements are Mayoi, a prebuilt `mayoi-java` tool, **JDK 25+** and an
Eclipse JDT LS distribution. Python, Node and a Go compiler are unnecessary for
users. Go 1.23+ is needed only when building the tool from source.

## Install

Install JDT LS and its modern runtime JDK. For Homebrew users:

```sh
brew install jdtls
```

Download the appropriate tool archive and `SHA256SUMS` from the
[releases page](https://github.com/Adrian-Zephyr-Liao/mayoi-java/releases) once
0.3 is published, verify the checksum, and put the executable on PATH.
Development archives can be built with `go run ./tools/release 0.3.0-dev`.
macOS/Linux archives contain `mayoi-java`; Windows contains `mayoi-java.exe`.

Install the plugin from `main` while testing:

```sh
git clone --branch main https://github.com/Adrian-Zephyr-Liao/mayoi-java.git
mayoi plugin dev /absolute/path/to/mayoi-java
mayoi-java setup
```

A GUI app with a minimal PATH can use the plugin's `helperPath` setting, for example:

```json
{
  "plugins": {
    "mayoi-java": { "helperPath": "/absolute/path/to/mayoi-java" }
  }
}
```

`setup` downloads a separately versioned Lombok agent and checks the local JDK
and JDT LS installation. It verifies Lombok 1.18.48 against a pinned SHA-256
checksum before installing it in the Mayoi profile. JDK/JDT LS installation
remains explicit; server startup performs no downloads. **Plugins → Java: Check
Setup** shows the resolved launch configuration. The adjacent install command
installs Lombok and checks setup from within the editor.

Open the root Maven/Gradle project, or a workspace containing immediate child
projects. Initial import may download project dependencies and needs time before
F12 is ready. Grouped workspaces import each canonical project once, with source
locations mapped back to the workspace aliases so existing tabs are reused.

## Configure

The JDK running JDT LS is separate from the project's source/target level.
Java 8 projects can keep their build settings and shell `JAVA_HOME`.
The tool detects modern Homebrew JDKs, `JAVA_HOME`, PATH and macOS `java_home`.
Other installations should specify absolute `javaHome` and `jdtlsHome` paths.

Optional configuration lives in `~/.mayoi/storage/mayoi-java/config.json`.
When `MAYOI_DATA_DIR` is set, use that profile directory instead of `~/.mayoi`.
Use the same profile for setup and the editor.

```json
{
  "javaHome": "/absolute/path/to/jdk-25",
  "jdtlsHome": "/absolute/path/to/jdt-language-server",
  "maxHeapMiB": 2048,
  "workspaces": {
    "/absolute/path/to/legacy-workspace": {
      "maxHeapMiB": 4096,
      "autoBuild": false,
      "annotationProcessing": false
    }
  }
}
```

`jdtlsHome` contains `plugins/` and `config_mac`, `config_mac_arm`,
`config_linux`, `config_linux_arm` or `config_win` for the host platform.
`cacheDir` relocates private indexes; `lombokJar` selects an existing compatible
agent. All configured paths must be absolute. Restart Mayoi after changes.

Normal defaults enable auto-build and annotation processing. Older projects with
processors incompatible with the language server JDK can disable them in a
workspace override, as above. Those preferences live in the private server cache;
they do not change source checkout files or Maven/Gradle builds. Such projects
may need their normal build to produce sources from other processors.

Precedence, lowest to highest: defaults → profile → matching canonical workspace
override → explicitly stored Mayoi plugin settings → environment variables.
The editor exposes `javaHome`, `jdtlsHome` and `maxHeapMiB`; compatibility options
stay in the profile's workspace configuration.

| Environment variable | Configuration key |
| --- | --- |
| `MAYOI_JAVA_HOME` | `javaHome` |
| `MAYOI_JAVA_JDTLS_HOME` | `jdtlsHome` |
| `MAYOI_JAVA_CACHE_DIR` | `cacheDir` |
| `MAYOI_JAVA_MAX_HEAP_MIB` | `maxHeapMiB` |

## Architecture and cost

The TypeScript plugin declares settings and setup commands. Its Go tool prepares
a launch description and exits. Mayoi starts Java directly and owns all LSP
traffic, configuration requests and URI mappings. There is no resident proxy.
See [ARCHITECTURE.md](ARCHITECTURE.md) for the native resolver contract.

One JVM runs per open workspace using Java. Its initial heap is 128 MiB, with a
2 GiB maximum by default; native JVM memory is additional. Large projects may
need 4 GiB. Import consumes CPU, disk and build dependency downloads; persistent
indexes are separated by canonical workspace path. Identity URI mappings are
skipped by the native client. Version 0.3 creates `data-v3-standard` or
`data-v3-no-apt` indexes to isolate old sessions and processor modes. Old caches
are retained and may be removed manually after closing the editor.

## Troubleshoot and migrate

```sh
mayoi-java doctor --root /absolute/workspace
mayoi-java install-lombok
```

`doctor` prepares the private configuration and prints launch JSON without
starting Java. Errors appear in **Plugins → Show Plugin Log**. JDT LS writes
`<data directory>/.metadata/.log`; its data directory follows `-data` in the
launch description. The old proxy's `status.json` is no longer produced.

0.2 profile files remain readable. If you previously put `autoBuild: false` or
`annotationProcessing: false` at the top level, move them into the relevant
workspace override to retain normal behavior elsewhere. Install the native
editor build and tool, update the plugin, then restart Mayoi. No project file
changes or global JDK switch are required.

## Develop and validate

```sh
go test -race ./...
go vet ./...
go build -o dist/mayoi-java .
go run ./tools/release 0.3.0-dev
mayoi plugin dev /absolute/path/to/mayoi-java
```

The release builder cross-compiles macOS arm64/amd64, Linux arm64/amd64 and
Windows amd64 and writes archives plus checksums under `dist/`. GitHub Actions
runs host tests on all three OS families and publishes archives when a `v*` tag
is pushed. A cross-build is not a runtime acceptance test on those platforms.

Mayoi's source includes opt-in `TestResolvedLanguageServerIntegration`.
Set `MAYOI_LSP_HELPER`, `MAYOI_LSP_ROOT` and `MAYOI_LSP_CASES` (a JSON array of
`{Path, Line, Column, WantPath, WantLine}`, relative paths and one-based positions).
For JDT LS also set `MAYOI_LSP_READY_METHOD=language/status` and
`MAYOI_LSP_READY_STATE=Started`. Run:

```sh
go test ./cmd/mayoi -run '^TestResolvedLanguageServerIntegration$' -count=1 -v
```

This exercises the real native transport, resolver and path mappings. Local
acceptance covers a grouped Maven workspace, same-file/cross-module methods and
a Lombok getter on macOS arm64. Gradle and SSH runtime acceptance are pending.
For SSH, install the tool, JDK and JDT LS on the remote host and configure paths
for that host. Windows/Linux runtime verification remains pending locally.

Mayoi currently opens `file:` definitions. JDK/dependency `jdt:` virtual documents
need additional editor support. This plugin uses F12; Command-click is separate.

MIT licensed. JDT LS is separately installed under its
[EPL-2.0 license](https://github.com/eclipse-jdtls/eclipse.jdt.ls/blob/main/LICENSE).
