# Changelog

## 0.3.0 (development)

- Replace the Python launcher and protocol proxy with a prebuilt Go setup tool.
- Run JDT LS directly through Mayoi's native language server resolver.
- Add Java setup checks to the Plugins menu.
- Enable normal auto-build and annotation processing by default; keep legacy compatibility per workspace.
- Build macOS, Linux and Windows tools with SHA-256 checksums.

Requires a Mayoi build containing the native resolver from upstream PR #135.

## 0.2.0

- Preserve workspace paths when navigating through symlinked repositories.
- Import grouped workspace projects once and answer language server configuration requests.
- Add verified Lombok agent installation and generated method navigation.
- Default to navigation without automatic builds or legacy annotation processors.
- Increase the default heap to 2 GiB and allow memory overrides per workspace.
- Keep readiness and recent definition results in the profile's private index cache.

## 0.1.0

- Add Java project source definitions through Mayoi's existing F12 command and Eclipse JDT LS.
- Detect a separate language server JDK without changing project build settings.
- Isolate persistent indexes for projects and Git worktrees and bound JVM heap use.

Validated with JDT LS 1.63.0, OpenJDK 26.0.1, and a Java 8 two-module Maven
project on macOS arm64. Eight launcher tests pass. The integration test resolves
both a class and a method across modules with Mayoi-compatible LSP capabilities.
The running Mayoi Dev application also opened the target module's source file
through its Go to Definition command. Dependency `jdt:` documents, Gradle
acceptance, and SSH acceptance remain outside this release's verified scope.
