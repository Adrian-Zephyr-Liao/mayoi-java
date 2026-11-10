#!/usr/bin/env python3
"""Launch JDT LS on stdio without changing the project's Java environment."""

import argparse
import hashlib
import json
import os
from pathlib import Path
import platform
import re
import shutil
import subprocess
import sys


MIN_JAVA = 25
CONFIG_KEYS = {"javaHome", "jdtlsHome", "cacheDir", "maxHeapMiB"}


class SetupError(Exception):
    pass


def data_dir():
    return Path(os.environ.get("MAYOI_DATA_DIR") or Path.home() / ".mayoi").expanduser().resolve()


def load_config():
    path = data_dir() / "storage" / "mayoi-java" / "config.json"
    if not path.exists():
        return {}
    try:
        config = json.loads(path.read_text(encoding="utf-8"))
    except (OSError, ValueError) as error:
        raise SetupError(f"Cannot read {path}: {error}") from error
    if not isinstance(config, dict) or config.keys() - CONFIG_KEYS:
        raise SetupError(f"{path} must be an object with only: {', '.join(sorted(CONFIG_KEYS))}")
    for key in ("javaHome", "jdtlsHome", "cacheDir"):
        if key in config and (not isinstance(config[key], str) or not config[key].strip()):
            raise SetupError(f"{key} must be a non-empty absolute path in {path}")
    return config


def absolute_path(value, name):
    path = Path(value).expanduser()
    if not path.is_absolute():
        raise SetupError(f"{name} must be an absolute path: {value}")
    return path.resolve()


def java_major(executable):
    try:
        result = subprocess.run(
            [str(executable), "-version"], capture_output=True, text=True, timeout=10,
        )
    except (OSError, subprocess.TimeoutExpired):
        return 0
    match = re.search(r'(?:openjdk|java) version "(?:1\.)?(\d+)', result.stderr + result.stdout)
    return int(match[1]) if result.returncode == 0 and match else 0


def find_java(config):
    explicit = os.environ.get("MAYOI_JAVA_HOME") or config.get("javaHome")
    if explicit:
        candidates = [absolute_path(explicit, "javaHome") / "bin" / "java"]
    else:
        # Homebrew's JDK is often not registered with java_home, and GUI apps
        # may inherit a Java 8 JAVA_HOME or a minimal PATH.
        candidates = [
            Path(prefix) / "opt" / name / "bin" / "java"
            for prefix in ("/opt/homebrew", "/usr/local", "/home/linuxbrew/.linuxbrew")
            for name in ("openjdk@25", "openjdk")
        ]
        if os.environ.get("JAVA_HOME"):
            candidates.append(Path(os.environ["JAVA_HOME"]) / "bin" / "java")
        if shutil.which("java"):
            candidates.append(Path(shutil.which("java")))
        if sys.platform == "darwin":
            try:
                result = subprocess.run(
                    ["/usr/libexec/java_home", "-v", f"{MIN_JAVA}+"],
                    capture_output=True, text=True, timeout=5,
                )
                if result.returncode == 0:
                    candidates.append(Path(result.stdout.strip()) / "bin" / "java")
            except (OSError, subprocess.TimeoutExpired):
                pass
    for executable in dict.fromkeys(candidates):
        if executable.is_file() and java_major(executable) >= MIN_JAVA:
            return executable.resolve()
    raise SetupError(
        f"JDT LS needs JDK {MIN_JAVA} or newer. Install one and set javaHome in "
        f"{data_dir() / 'storage/mayoi-java/config.json'} or MAYOI_JAVA_HOME. "
        "Your project's JAVA_HOME does not need to change."
    )


def find_jdtls(config):
    explicit = os.environ.get("MAYOI_JAVA_JDTLS_HOME") or config.get("jdtlsHome")
    if explicit:
        candidates = [absolute_path(explicit, "jdtlsHome")]
    else:
        candidates = [
            Path(prefix) / "opt/jdtls/libexec"
            for prefix in ("/opt/homebrew", "/usr/local", "/home/linuxbrew/.linuxbrew")
        ]
        launcher = shutil.which("jdtls")
        if launcher:
            base = Path(launcher).resolve().parent.parent
            candidates.extend([base, base / "libexec"])
    for home in candidates:
        jars = sorted((home / "plugins").glob("org.eclipse.equinox.launcher_*.jar"))
        if len(jars) == 1:
            return home.resolve(), jars[0].resolve()
    raise SetupError(
        "Cannot find a JDT LS distribution with one Equinox launcher JAR. "
        "Install jdtls (macOS: brew install jdtls), or set jdtlsHome / MAYOI_JAVA_JDTLS_HOME."
    )


def configuration_source(home):
    system = {"darwin": "mac", "linux": "linux"}.get(sys.platform)
    if not system:
        raise SetupError("This version supports macOS and Linux.")
    arm = platform.machine().lower() in ("arm64", "aarch64")
    name = "config_" + system + ("_arm" if arm else "")
    source = home / name
    if not (source / "config.ini").is_file():
        raise SetupError(f"Missing {source / 'config.ini'}; install JDT LS for this platform.")
    return source


def workspace_id(root):
    # Full paths separate sibling projects, Git worktrees, and equal basenames.
    return hashlib.sha256(os.fsencode(root.resolve())).hexdigest()[:24]


def launch_plan(root, config):
    java = find_java(config)
    home, jar = find_jdtls(config)
    source = configuration_source(home)
    cache = os.environ.get("MAYOI_JAVA_CACHE_DIR") or config.get("cacheDir")
    cache = absolute_path(cache, "cacheDir") if cache else data_dir() / "storage/mayoi-java/cache"
    folder = cache / workspace_id(root)
    distribution = hashlib.sha256(os.fsencode(home) + source.joinpath("config.ini").read_bytes()).hexdigest()[:12]
    configuration = folder / distribution / "configuration"
    raw_heap = os.environ.get("MAYOI_JAVA_MAX_HEAP_MIB", config.get("maxHeapMiB", 1024))
    if isinstance(raw_heap, bool) or not re.fullmatch(r"\d+", str(raw_heap)) or not 256 <= int(raw_heap) <= 32768:
        raise SetupError("maxHeapMiB / MAYOI_JAVA_MAX_HEAP_MIB must be an integer from 256 to 32768.")
    command = [
        str(java), "-Declipse.application=org.eclipse.jdt.ls.core.id1",
        "-Dosgi.bundles.defaultStartLevel=4", "-Declipse.product=org.eclipse.jdt.ls.core.product",
        "-Dlog.protocol=false", "-Dlog.level=WARNING", "-Xms128m", f"-Xmx{raw_heap}m",
        "--add-modules=ALL-SYSTEM", "--add-opens", "java.base/java.util=ALL-UNNAMED",
        "--add-opens", "java.base/java.lang=ALL-UNNAMED",
        "-jar", str(jar), "-configuration", str(configuration), "-data", str(folder / "data"),
    ]
    return {
        "workspace": str(root.resolve()), "java": str(java), "jdtls": str(home),
        "data": str(folder / "data"), "configuration": str(configuration),
        "configurationSource": str(source), "command": command,
    }


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--check", action="store_true", help="Print setup and launch command without starting Java")
    args = parser.parse_args()
    try:
        plan = launch_plan(Path.cwd(), load_config())
        if args.check:
            print(json.dumps(plan, indent=2, ensure_ascii=False))
            return 0
        configuration = Path(plan["configuration"])
        if not configuration.exists():
            configuration.parent.mkdir(parents=True, exist_ok=True)
            shutil.copytree(plan["configurationSource"], configuration)
        Path(plan["data"]).parent.mkdir(parents=True, exist_ok=True)
        # Never touch the parent's environment. JDT LS uses stdio, even when
        # an editor inherited socket settings from another Java tool.
        env = os.environ.copy()
        for key in ("CLIENT_HOST", "CLIENT_PORT", "JDTLS_JVM_ARGS", "JAVA_TOOL_OPTIONS", "JDK_JAVA_OPTIONS", "_JAVA_OPTIONS"):
            env.pop(key, None)
        env["JAVA_HOME"] = str(Path(plan["java"]).parent.parent)
        os.execve(plan["java"], plan["command"], env)
    except (SetupError, OSError) as error:
        print(f"mayoi-java: {error}", file=sys.stderr)
        return 1


if __name__ == "__main__":
    sys.exit(main())
