import io
import json
from pathlib import Path
import sys
import tempfile
import unittest

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))
from transport import Adapter, Workspace, read_message, settings_for, write_message


class TransportTest(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory(prefix="mayoi-路径-")
        self.addCleanup(self.temp.cleanup)
        self.base = Path(self.temp.name).resolve()
        self.root = self.base / "workspace"
        self.root.mkdir()
        self.real = self.base / "canonical/repository/worktree"
        self.real.mkdir(parents=True)
        (self.real / "pom.xml").touch()
        self.alias = self.root / "repository"
        self.alias.symlink_to(self.real, target_is_directory=True)

    def test_unicode_framing_counts_bytes_and_preserves_source(self):
        message = {"params": {"text": "中文 😀 file:///should-stay-source", "uri": "file:///中文"}}
        stream = io.BytesIO()
        write_message(stream, message)
        stream.seek(0)
        self.assertEqual(read_message(stream), message)
        self.assertIsNone(read_message(stream))
        with self.assertRaises(EOFError):
            read_message(io.BytesIO(b'Content-Length: 20\r\n\r\n{}'))

    def test_symlink_uris_roundtrip_without_duplicate_tabs(self):
        workspace = Workspace(self.root)
        source = self.alias / "src/示例.java"
        target = self.real / "src/示例.java"
        self.assertEqual(workspace.uri(source.as_uri(), True), target.as_uri())
        self.assertEqual(workspace.uri(target.as_uri(), False), source.as_uri())
        unrelated = self.base / "canonical/repository/worktree-other/Test.java"
        self.assertEqual(workspace.uri(unrelated.as_uri(), False), unrelated.as_uri())
        self.assertEqual(workspace.server_folders(), [{"uri": self.real.as_uri(), "name": "repository"}])

    def test_workspace_edits_rewrite_uri_keys_and_preserve_virtual_uris(self):
        workspace = Workspace(self.root)
        uri = (self.real / "Test.java").as_uri()
        value = {"changes": {uri: [{"newText": "String text = \"中文\";"}]}, "uri": "jdt://contents/Test.class"}
        result = workspace.rewrite(value, False)
        self.assertIn((self.alias / "Test.java").as_uri(), result["changes"])
        self.assertEqual(result["uri"], value["uri"])

    def test_initialization_and_definition_responses_use_same_workspace(self):
        adapter = Adapter(self.root, {"java": {"autobuild": {"enabled": False}}}, self.base / "status.json")
        init = adapter.incoming({"id": 1, "method": "initialize", "params": {"initializationOptions": None}})
        self.assertEqual(init["params"]["workspaceFolders"][0]["uri"], self.real.as_uri())
        self.assertEqual(adapter.section("java.autobuild.enabled"), False)
        self.assertIsNone(adapter.section("java.unknown"))
        request = {"id": 2, "method": "textDocument/definition", "params": {
            "textDocument": {"uri": (self.alias / "Main.java").as_uri()},
            "position": {"line": 4, "character": 12},
        }}
        original = json.dumps(request)
        adapter.incoming(request)
        result = adapter.outgoing({"id": 2, "result": [{"uri": (self.real / "Target.java").as_uri()}]})
        self.assertEqual(result["result"][0]["uri"], (self.alias / "Target.java").as_uri())
        self.assertEqual(json.dumps(request), original)
        self.assertEqual(json.loads((self.base / "status.json").read_text())["definitions"][-1]["request"], request["params"])

    def test_navigation_mode_uses_private_compiler_preferences(self):
        plan = {"data": str(self.base / "data-v2")}
        settings = settings_for(plan, {})
        self.assertFalse(settings["java"]["autobuild"]["enabled"])
        self.assertIn("processAnnotations=disabled", (self.base / "navigation.prefs").read_text())
        self.assertNotIn("settings", settings_for(plan, {"annotationProcessing": True})["java"])


if __name__ == "__main__":
    unittest.main()
