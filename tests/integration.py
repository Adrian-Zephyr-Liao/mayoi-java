"""Real JDT LS + Maven definition test using the plugin manifest and Mayoi's capabilities."""

import json
import os
from pathlib import Path
import queue
import subprocess
import tempfile
import threading
import time


ROOT = Path(__file__).resolve().parents[1]


class Client:
    def __init__(self, command, cwd, env, stderr):
        self.process = subprocess.Popen(command, cwd=cwd, env=env, stdin=subprocess.PIPE,
                                        stdout=subprocess.PIPE, stderr=stderr)
        self.messages = queue.Queue()
        self.sequence = 0
        threading.Thread(target=self.read, daemon=True).start()

    def read(self):
        try:
            while True:
                headers = {}
                while True:
                    line = self.process.stdout.readline()
                    if not line:
                        raise EOFError("JDT LS closed stdout")
                    if line == b"\r\n":
                        break
                    name, value = line.decode().split(":", 1)
                    headers[name.lower()] = value.strip()
                body = self.process.stdout.read(int(headers["content-length"]))
                self.messages.put(json.loads(body))
        except Exception as error:
            self.messages.put(error)

    def send(self, message):
        body = json.dumps({"jsonrpc": "2.0", **message}).encode()
        self.process.stdin.write(f"Content-Length: {len(body)}\r\n\r\n".encode() + body)
        self.process.stdin.flush()

    def notify(self, method, params):
        self.send({"method": method, "params": params})

    def call(self, method, params, timeout=10):
        self.sequence += 1
        sequence = self.sequence
        self.send({"id": sequence, "method": method, "params": params})
        deadline = time.monotonic() + timeout
        while True:
            remaining = deadline - time.monotonic()
            if remaining <= 0:
                raise queue.Empty(f"{method} timed out")
            message = self.messages.get(timeout=remaining)
            if isinstance(message, Exception):
                raise message
            if "method" in message:
                if "id" in message:
                    # Match Mayoi's current responses rather than hide client
                    # incompatibility behind a more capable test client.
                    result = None
                    if message["method"] == "workspace/configuration":
                        result = [None for _ in message.get("params", {}).get("items", [])]
                    elif message["method"] == "workspace/workspaceFolders":
                        result = []
                    self.send({"id": message["id"], "result": result})
                continue
            if message.get("id") == sequence:
                if "error" in message:
                    raise RuntimeError(message["error"])
                return message.get("result")

    def close(self):
        if self.process.poll() is None:
            try:
                self.call("shutdown", None, timeout=5)
                self.notify("exit", None)
                self.process.wait(timeout=10)
            except Exception:
                self.process.kill()
                self.process.wait(timeout=5)
        self.process.stdin.close()
        self.process.stdout.close()


def fixture(root):
    files = {
        "pom.xml": """<project xmlns="http://maven.apache.org/POM/4.0.0">
  <modelVersion>4.0.0</modelVersion><groupId>org.mayoi.fixture</groupId>
  <artifactId>demo</artifactId><version>1.0-SNAPSHOT</version><packaging>pom</packaging>
  <modules><module>api</module><module>app</module></modules>
  <properties><maven.compiler.source>1.8</maven.compiler.source>
    <maven.compiler.target>1.8</maven.compiler.target><project.build.sourceEncoding>UTF-8</project.build.sourceEncoding></properties>
</project>
""",
        "api/pom.xml": """<project xmlns="http://maven.apache.org/POM/4.0.0">
  <modelVersion>4.0.0</modelVersion><parent><groupId>org.mayoi.fixture</groupId>
    <artifactId>demo</artifactId><version>1.0-SNAPSHOT</version></parent><artifactId>api</artifactId>
</project>
""",
        "app/pom.xml": """<project xmlns="http://maven.apache.org/POM/4.0.0">
  <modelVersion>4.0.0</modelVersion><parent><groupId>org.mayoi.fixture</groupId>
    <artifactId>demo</artifactId><version>1.0-SNAPSHOT</version></parent><artifactId>app</artifactId>
  <dependencies><dependency><groupId>org.mayoi.fixture</groupId><artifactId>api</artifactId>
    <version>1.0-SNAPSHOT</version></dependency></dependencies>
</project>
""",
        "api/src/main/java/demo/Greeting.java": """package demo;

public class Greeting {
    public static String message() { return "Hello from another module"; }
}
""",
        "app/src/main/java/demo/Main.java": """package demo;

public class Main {
    public String run() {
        return Greeting.message();
    }
}
""",
    }
    for name, text in files.items():
        path = root / name
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_text(text, encoding="utf-8")
    return root / "app/src/main/java/demo/Main.java", root / "api/src/main/java/demo/Greeting.java"


def main():
    manifest = json.loads((ROOT / "plugin.json").read_text())
    server = manifest["contributes"]["languageServers"][0]
    with tempfile.TemporaryDirectory(prefix="mayoi-java-integration-") as temporary:
        base = Path(temporary).resolve()
        profile = base / "profile"
        (profile / "plugins").mkdir(parents=True)
        (profile / "plugins/mayoi-java").symlink_to(ROOT, target_is_directory=True)
        root = base / "java8-reactor"
        source, target = fixture(root)
        env = {**os.environ, "MAYOI_DATA_DIR": str(profile)}
        # Preserve a configured non-Homebrew installation, but keep test
        # indexes isolated from the user's Mayoi profile.
        real_profile = Path(os.environ.get("MAYOI_DATA_DIR") or Path.home() / ".mayoi")
        config = real_profile / "storage/mayoi-java/config.json"
        if config.is_file():
            isolated_config = profile / "storage/mayoi-java/config.json"
            isolated_config.parent.mkdir(parents=True)
            value = json.loads(config.read_text())
            value.pop("cacheDir", None)
            isolated_config.write_text(json.dumps(value))
        env.pop("MAYOI_JAVA_CACHE_DIR", None)
        with (base / "stderr.log").open("w+") as log:
            client = Client([server["command"], *server["args"]], root, env, log)
            started = time.monotonic()
            try:
                result = client.call("initialize", {
                    "processId": None, "rootUri": root.as_uri(),
                    "workspaceFolders": [{"uri": root.as_uri(), "name": root.name}],
                    "initializationOptions": server.get("initializationOptions"),
                    "clientInfo": {"name": "Mayoi"},
                    "capabilities": {
                        "general": {"positionEncodings": ["utf-16"]},
                        "workspace": {"workspaceFolders": True, "configuration": True},
                        "textDocument": {
                            "synchronization": {"didSave": True},
                            "hover": {"contentFormat": ["markdown", "plaintext"]},
                            "definition": {"linkSupport": False},
                            "completion": {"completionItem": {"snippetSupport": True}},
                            "formatting": {}, "publishDiagnostics": {"relatedInformation": False},
                        },
                    },
                }, timeout=30)
                assert result["capabilities"].get("definitionProvider"), result
                client.notify("initialized", {})
                client.notify("textDocument/didOpen", {"textDocument": {
                    "uri": source.as_uri(), "languageId": "java", "version": 1,
                    "text": source.read_text(),
                }})
                # Retry while the real Maven import runs. Each definition
                # request has Mayoi's five-second timeout.
                for symbol, column, expected_line in (("Greeting", 17, 2), ("message", 26, 3)):
                    deadline = started + 180
                    last = None
                    while time.monotonic() < deadline:
                        try:
                            last = client.call("textDocument/definition", {
                                "textDocument": {"uri": source.as_uri()},
                                "position": {"line": 4, "character": column},
                            }, timeout=5)
                        except queue.Empty:
                            last = "request timed out during import"
                        if isinstance(last, list) and last and last[0].get("uri") == target.as_uri():
                            assert last[0]["range"]["start"]["line"] == expected_line, last
                            print(f"PASS {symbol}: {last[0]['uri']}:{expected_line + 1}", flush=True)
                            break
                        time.sleep(1)
                    else:
                        raise AssertionError(f"No cross-module definition for {symbol}: {last}")
                print(f"PASS JDT LS initialized and Maven definitions resolved in {time.monotonic() - started:.1f}s")
            except Exception:
                log.flush()
                log.seek(0)
                print(log.read()[-12000:])
                for jdt_log in profile.glob("storage/mayoi-java/cache/**/.metadata/.log"):
                    print(jdt_log.read_text(errors="replace")[-12000:])
                raise
            finally:
                client.close()


if __name__ == "__main__":
    main()
