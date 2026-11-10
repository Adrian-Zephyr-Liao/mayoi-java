import { commands, process, settings, window, workspace } from "mayoi";

async function check(install: boolean) {
  const helper = settings.get<string>("helperPath");
  const root = await workspace.root();
  if (!root) { await window.fail("Open a Java project folder first."); return; }
  await window.progress(install ? "Preparing Java tools…" : "Checking Java setup…", async () => {
    try {
      if (install) {
        const setup = await process.run(helper, ["install-lombok"], { timeout: 90000 });
        if (setup.code !== 0) throw new Error(setup.stderr || "Lombok installation failed");
      }
      const checked = await process.run(helper, ["resolve"], {
        input: JSON.stringify({ root: root.replace(/^\/ssh:[^/]+/, ""), settings: settings.all() }),
        timeout: 30000,
      });
      if (checked.code !== 0) throw new Error(checked.stderr || "Java setup failed");
      await window.openText(checked.stdout, { title: "Java Setup", language: "json" });
      if (install) await window.notify("Java tools are ready. Restart Mayoi to reload the language server.");
    } catch (error) {
      await window.fail(`${String(error)}\nInstall the mayoi-java tool or set Java's helperPath setting.`);
    }
  });
}

commands.register("mayoi-java.doctor", () => check(false));
commands.register("mayoi-java.setup", () => check(true));
