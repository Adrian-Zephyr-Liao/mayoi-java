import importlib.util
import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest
from unittest.mock import patch


ROOT = Path(__file__).resolve().parents[1]
spec = importlib.util.spec_from_file_location("server", ROOT / "server.py")
server = importlib.util.module_from_spec(spec)
spec.loader.exec_module(server)


class LauncherTest(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory(prefix="mayoi java ")
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name).resolve()
        self.env = patch.dict(os.environ, {"MAYOI_DATA_DIR": str(self.root / "profile")}, clear=True)
        self.env.start()
        self.addCleanup(self.env.stop)

    def config(self, value):
        path = server.data_dir() / "storage/mayoi-java/config.json"
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_text(json.dumps(value), encoding="utf-8")

    def test_config_is_optional_but_malformed_config_fails(self):
        self.assertEqual(server.load_config(), {})
        for value in ([], {"javaHome": 8}, {"javaHome": ""}, {"typo": True}):
            with self.subTest(value=value):
                self.config(value)
                with self.assertRaises(server.SetupError):
                    server.load_config()

    def test_workspace_cache_separates_equal_names_and_resolves_symlinks(self):
        a = self.root / "first/project"
        b = self.root / "second/project"
        a.mkdir(parents=True)
        b.mkdir(parents=True)
        alias = self.root / "alias"
        alias.symlink_to(a, target_is_directory=True)
        self.assertNotEqual(server.workspace_id(a), server.workspace_id(b))
        self.assertEqual(server.workspace_id(a), server.workspace_id(alias))

    def test_explicit_old_java_fails_without_silent_fallback(self):
        java = self.root / "jdk8/bin/java"
        java.parent.mkdir(parents=True)
        java.touch()
        with patch.object(server, "java_major", return_value=8):
            with self.assertRaisesRegex(server.SetupError, "JDK 25"):
                server.find_java({"javaHome": str(java.parent.parent)})

    def test_explicit_java_env_overrides_config(self):
        java = self.root / "jdk25/bin/java"
        java.parent.mkdir(parents=True)
        java.touch()
        os.environ["MAYOI_JAVA_HOME"] = str(java.parent.parent)
        with patch.object(server, "java_major", return_value=25):
            self.assertEqual(server.find_java({"javaHome": "/missing"}), java)

    def test_java_version_parsing(self):
        for value, expected in (("1.8.0_291", 8), ("25.0.1", 25), ("26-ea", 26)):
            result = subprocess.CompletedProcess([], 0, "", f'openjdk version "{value}"')
            with patch.object(subprocess, "run", return_value=result):
                self.assertEqual(server.java_major("java"), expected)

    def test_distribution_and_architecture_must_match(self):
        home = self.root / "jdtls"
        (home / "plugins").mkdir(parents=True)
        jar = home / "plugins/org.eclipse.equinox.launcher_1.jar"
        jar.touch()
        self.assertEqual(server.find_jdtls({"jdtlsHome": str(home)}), (home, jar))
        with patch.object(sys, "platform", "darwin"), patch.object(server.platform, "machine", return_value="arm64"):
            with self.assertRaisesRegex(server.SetupError, "config_mac_arm"):
                server.configuration_source(home)
            (home / "config_mac_arm").mkdir()
            (home / "config_mac_arm/config.ini").touch()
            self.assertEqual(server.configuration_source(home), home / "config_mac_arm")
        (home / "plugins/org.eclipse.equinox.launcher_2.jar").touch()
        with self.assertRaisesRegex(server.SetupError, "one Equinox"):
            server.find_jdtls({"jdtlsHome": str(home)})

    def test_launch_plan_preserves_paths_and_bounds_memory_without_writes(self):
        home = self.root / "jdtls space"
        source = home / "config_linux"
        source.mkdir(parents=True)
        (source / "config.ini").write_text("config", encoding="utf-8")
        with patch.object(server, "find_java", return_value=Path("/jdk space/bin/java")), \
             patch.object(server, "find_jdtls", return_value=(home, home / "launcher.jar")), \
             patch.object(server, "configuration_source", return_value=source):
            plan = server.launch_plan(self.root, {})
            self.assertEqual(plan["command"][0], "/jdk space/bin/java")
            self.assertIn("-Xmx1024m", plan["command"])
            self.assertFalse(Path(plan["data"]).exists())
            self.assertFalse(Path(plan["configuration"]).exists())
            for value in (True, 128, "1g", 1024.5, 32769):
                with self.subTest(value=value), self.assertRaises(server.SetupError):
                    server.launch_plan(self.root, {"maxHeapMiB": value})

    def test_manifest_bootstrap_respects_custom_profile_and_spaces(self):
        plugins = server.data_dir() / "plugins"
        plugins.mkdir(parents=True)
        (plugins / "mayoi-java").symlink_to(ROOT, target_is_directory=True)
        manifest = json.loads((ROOT / "plugin.json").read_text())
        contribution = manifest["contributes"]["languageServers"][0]
        result = subprocess.run(
            [sys.executable, *contribution["args"], "--help"],
            capture_output=True, text=True, cwd=self.root, timeout=5,
        )
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn("--check", result.stdout)
        self.assertEqual(contribution["languages"], ["java"])


if __name__ == "__main__":
    unittest.main()
