# Java for Mayoi

Java source navigation for [Mayoi](https://github.com/sorrycc/Mayoi), powered by
[Eclipse JDT Language Server](https://github.com/eclipse-jdtls/eclipse.jdt.ls).
Open a Java project, place the caret on a class or method, and press **F12** to
jump to its definition in project source. Mayoi also consumes JDT LS hover,
completion, and diagnostics through its existing LSP client.

## Install

Requires a Mayoi build with `contributes.languageServers`, Python 3.9+, JDK 25+
for the language server, and an installed JDT LS distribution. The initial
version targets local projects on macOS and Linux.

On macOS:

```sh
brew install python jdtls
mayoi plugin install Adrian-Zephyr-Liao/mayoi-java
```

The launcher detects Homebrew's JDK, even when the default `java` and
`JAVA_HOME` still point to Java 8. It does not change your shell configuration,
project build settings, or system Java selection. The JDK running JDT LS and
the project's source/target level are separate; existing Java 8 Maven projects
can keep their compiler settings.

Open the folder containing the root `pom.xml`, `build.gradle`, or
`build.gradle.kts` in Mayoi. For a Maven reactor, open its parent folder so JDT LS
can import all modules. The first import needs time and may download build
dependencies, just as an IDE import does. Retry F12 after indexing completes.

For a non-Homebrew installation, download JDT LS from its official project,
then configure the absolute distribution and JDK paths below.

## Configure

Optional global configuration lives at
`~/.mayoi/storage/mayoi-java/config.json`. When `MAYOI_DATA_DIR` is set, replace
`~/.mayoi` with that directory. Create the parent directories if necessary.

```json
{
  "javaHome": "/absolute/path/to/jdk-25/Contents/Home",
  "jdtlsHome": "/absolute/path/to/jdt-language-server",
  "maxHeapMiB": 1024
}
```

`jdtlsHome` contains `plugins/` and the appropriate `config_mac`,
`config_mac_arm`, `config_linux`, or `config_linux_arm` directory. Homebrew users
normally need no configuration. `cacheDir` can optionally move the index cache.
Configuration is read at server startup; restart Mayoi after changing it.

Environment overrides take precedence over the JSON file:

| Variable | Configuration key |
| --- | --- |
| `MAYOI_JAVA_HOME` | `javaHome` |
| `MAYOI_JAVA_JDTLS_HOME` | `jdtlsHome` |
| `MAYOI_JAVA_CACHE_DIR` | `cacheDir` |
| `MAYOI_JAVA_MAX_HEAP_MIB` | `maxHeapMiB` |

For GUI launches, use the JSON file when shell environment variables are not
inherited. Mayoi must be able to find `python3` in its process PATH.

## Troubleshoot

Run this from the Java project directory to check dependencies and show the
exact JVM command without starting the server or creating its cache:

```sh
python3 ~/.mayoi/plugins/mayoi-java/server.py --check
```

Use the corresponding `MAYOI_DATA_DIR` path for a development profile.
Server startup errors appear in Mayoi's **Plugins → Show Plugin Log**.
JDT LS logs live in the reported data directory at `.metadata/.log`.

The plugin starts one JVM for each folder Mayoi opens a Java file in. The
initial Java heap is 128 MiB and the maximum is 1024 MiB by default; JVM native
memory is additional. Project import/indexing consumes CPU and disk, and the
index persists under `storage/mayoi-java/cache`. Full canonical project paths
separate caches for projects and Git worktrees with identical names.

## Current boundaries

- F12 into project source is the first release's acceptance target. Maven
  module navigation is covered by the integration test. Gradle import is
  enabled through JDT LS but is not yet an acceptance test.
- Dependency and JDK definitions may use `jdt:` virtual document URIs. Mayoi's
  current client opens `file:` URIs, so JAR source/decompiled class navigation
  needs additional editor integration and is not implemented here.
- SSH projects need the launcher, Python, JDK, and JDT LS installed on the
  remote host too. This version has not been verified with Mayoi SSH sessions.
- This uses Mayoi's existing F12 command. It does not add Command-click support.

## Develop and test

```sh
mayoi plugin dev /absolute/path/to/mayoi-java
python3 -m unittest discover -s tests -v
python3 tests/integration.py
```

The integration test starts the installed JDT LS through the actual plugin
manifest, imports a temporary two-module Maven project targeting Java 8, and
checks class and method definition locations across modules. It uses JDT LS's
Maven importer and allows up to three minutes for import. It does not operate
the Mayoi UI.

The plugin is MIT licensed. JDT LS is a separate installation with its own
[EPL-2.0 license](https://github.com/eclipse-jdtls/eclipse.jdt.ls/blob/main/LICENSE).
