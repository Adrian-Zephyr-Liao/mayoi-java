"""Small stdio LSP adapter for workspace paths and navigation preferences."""

import copy
import json
import os
from pathlib import Path
import subprocess
import sys
import threading
import time
from urllib.parse import unquote, urlsplit


def read_message(stream):
    headers = {}
    while True:
        line = stream.readline()
        if not line:
            return None
        if line in (b"\r\n", b"\n"):
            break
        name, value = line.decode("ascii").split(":", 1)
        headers[name.lower()] = value.strip()
    length = int(headers["content-length"])
    if not 0 <= length <= 64 * 1024 * 1024:
        raise ValueError("Invalid LSP Content-Length")
    body = bytearray()
    while len(body) < length:
        chunk = stream.read(length - len(body))
        if not chunk:
            raise EOFError("Incomplete LSP message")
        body.extend(chunk)
    return json.loads(body)


def write_message(stream, message):
    body = json.dumps(message, ensure_ascii=False, separators=(",", ":")).encode("utf-8")
    stream.write(f"Content-Length: {len(body)}\r\n\r\n".encode("ascii") + body)
    stream.flush()


class Workspace:
    def __init__(self, root):
        self.root = Path(os.path.abspath(root))
        self.aliases = [(self.root, self.root.resolve())]
        self.folders = [self.root]
        markers = ("pom.xml", "build.gradle", "build.gradle.kts", ".project")
        if not any((self.root / name).is_file() for name in markers):
            members = [p for p in sorted(self.root.iterdir()) if p.is_dir()
                       and any((p / name).is_file() for name in markers)]
            if members:
                self.folders = members
        for member in self.folders:
            self.aliases.append((member, member.resolve()))
        # Nested aliases can occur in grouped workspaces. Prefer the closest
        # path prefix and never replace an unrelated path with a common name.
        self.aliases.sort(key=lambda pair: len(pair[1].parts), reverse=True)

    def uri(self, value, to_server):
        if not isinstance(value, str) or not value.startswith("file:"):
            return value
        parsed = urlsplit(value)
        if parsed.netloc not in ("", "localhost"):
            return value
        path = Path(unquote(parsed.path))
        if to_server:
            return path.resolve().as_uri()
        for alias, canonical in self.aliases:
            try:
                return (alias / path.relative_to(canonical)).as_uri()
            except ValueError:
                continue
        return value

    def rewrite(self, value, to_server):
        if isinstance(value, dict):
            return {self.uri(key, to_server): self.rewrite(item, to_server) for key, item in value.items()}
        if isinstance(value, list):
            return [self.rewrite(item, to_server) for item in value]
        return self.uri(value, to_server)

    def server_folders(self):
        seen = set()
        folders = []
        for folder in self.folders:
            uri = folder.resolve().as_uri()
            if uri not in seen:
                seen.add(uri)
                folders.append({"uri": uri, "name": folder.name})
        return folders


def settings_for(plan, config):
    java = {
        "configuration": {"updateBuildConfiguration": "automatic"},
        "import": {"maven": {"enabled": True}, "gradle": {"enabled": True}},
        "autobuild": {"enabled": config.get("autoBuild", False)},
    }
    # The compiler preferences live in the plugin's private cache. They do
    # not change pom.xml or Eclipse settings in the source checkout.
    if not config.get("annotationProcessing", False):
        prefs = Path(plan["data"]).parent / "navigation.prefs"
        prefs.write_text("org.eclipse.jdt.core.compiler.processAnnotations=disabled\n", encoding="utf-8")
        java["settings"] = {"url": prefs.as_uri()}
    return {"java": java}


class Adapter:
    def __init__(self, root, settings, status_path):
        self.workspace = Workspace(root)
        self.settings = settings
        self.pending = {}
        self.status_path = Path(status_path)
        self.status = {"state": "starting", "folders": self.workspace.server_folders(), "definitions": []}

    def section(self, name):
        value = self.settings
        for part in name.split(".") if name else []:
            value = value.get(part) if isinstance(value, dict) else None
        return value

    def incoming(self, message):
        message = self.workspace.rewrite(copy.deepcopy(message), True)
        if message.get("method") == "initialize":
            params = message["params"]
            folders = self.workspace.server_folders()
            params["workspaceFolders"] = folders
            params["rootUri"] = folders[0]["uri"]
            options = params.setdefault("initializationOptions", {}) or {}
            params["initializationOptions"] = options
            options["workspaceFolders"] = [folder["uri"] for folder in folders]
            options["settings"] = self.settings
        if message.get("method") == "textDocument/definition":
            self.pending[message["id"]] = (time.monotonic(), message["params"])
            # Bound state for requests canceled by the editor during import.
            if len(self.pending) > 100:
                self.pending.pop(next(iter(self.pending)))
        return message

    def outgoing(self, message):
        if message.get("method") == "language/status":
            params = message.get("params", {})
            self.status["state"] = params.get("type", "unknown")
            self.save_status()
        pending = self.pending.pop(message.get("id"), None) if "method" not in message else None
        message = self.workspace.rewrite(message, False)
        if pending:
            started, params = pending
            self.status["definitions"].append({
                "request": self.workspace.rewrite(params, False),
                "result": message.get("result"), "error": message.get("error"),
                "elapsedMs": round((time.monotonic() - started) * 1000),
            })
            self.status["definitions"] = self.status["definitions"][-20:]
            self.save_status()
        return message

    def save_status(self):
        temporary = self.status_path.with_suffix(".tmp")
        temporary.write_text(json.dumps(self.status, ensure_ascii=False, indent=2), encoding="utf-8")
        temporary.replace(self.status_path)


def serve(plan, config, env):
    adapter = Adapter(Path.cwd(), settings_for(plan, config), Path(plan["data"]).parent / "status.json")
    child = subprocess.Popen(plan["command"], stdin=subprocess.PIPE, stdout=subprocess.PIPE, env=env)
    write_lock = threading.Lock()
    errors = []

    def send_to_server(message):
        with write_lock:
            write_message(child.stdin, message)

    def from_editor():
        try:
            while (message := read_message(sys.stdin.buffer)) is not None:
                send_to_server(adapter.incoming(message))
        except (OSError, ValueError, EOFError) as error:
            errors.append(str(error))
        finally:
            child.stdin.close()
            if child.poll() is None:
                child.terminate()

    threading.Thread(target=from_editor, daemon=True).start()
    try:
        while (message := read_message(child.stdout)) is not None:
            method = message.get("method")
            if "id" in message and method in ("workspace/configuration", "workspace/workspaceFolders"):
                if method == "workspace/workspaceFolders":
                    result = adapter.workspace.server_folders()
                else:
                    result = [adapter.section(item.get("section", ""))
                              for item in message.get("params", {}).get("items", [])]
                send_to_server({"jsonrpc": "2.0", "id": message["id"], "result": result})
                continue
            write_message(sys.stdout.buffer, adapter.outgoing(message))
    except (OSError, ValueError, EOFError) as error:
        errors.append(str(error))
    finally:
        if child.poll() is None:
            child.terminate()
        try:
            child.wait(timeout=10)
        except subprocess.TimeoutExpired:
            child.kill()
            child.wait()
    for error in errors:
        print(f"mayoi-java: {error}", file=sys.stderr)
    return child.returncode or 0
