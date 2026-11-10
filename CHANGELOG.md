# Changelog

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
