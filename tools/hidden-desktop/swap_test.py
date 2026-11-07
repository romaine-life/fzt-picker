"""A release swap under a running app. Runs on a hidden desktop: see TESTING.md.

    python swap_test.py <empty test folder> <folder holding fzt-picker.exe and fzt_picker_hook.dll> <test-trigger.exe>

Lays out two version folders, v1 and v2, with a `current` junction to v1, as
home-sync installs them. It starts the injector through `current`, then an app
(test-trigger --repeat) that opens a dialog every five seconds and lives like a
program in between. After the first dialog it swaps `current` to v2 and restarts
the injector, as a release does. With no dialog DLL beside the hook, each dialog
fails at once, and picker.log names the folder that copy of the hook looked in,
so the report shows which release answered each dialog. After the swap it should
be v2.
"""
import _winapi
import json
import os
import pathlib
import re
import shutil
import subprocess
import sys
import time

folder, build, trigger = map(pathlib.Path, sys.argv[1:4])
sys.excepthook = lambda kind, value, tb: (folder / "swap-error.txt").write_text(
    "".join(__import__("traceback").format_exception(kind, value, tb)))
log = pathlib.Path(os.environ["TEMP"]) / "picker.log"
root = folder / "swap"
current = root / "current"
for version in ("v1", "v2"):
    (root / version).mkdir(parents=True, exist_ok=True)
    for name in ("fzt-picker.exe", "fzt_picker_hook.dll"):
        shutil.copy2(build / name, root / version / name)
_winapi.CreateJunction(str(root / "v1"), str(current))

start = log.stat().st_size if log.exists() else 0
injector = subprocess.Popen([str(current / "fzt-picker.exe")])
time.sleep(1.5)
app = subprocess.Popen([str(trigger), "--repeat", "3", "--gap-ms", "5000"], stdout=subprocess.PIPE, text=True)
time.sleep(2.5)
os.rmdir(current)
_winapi.CreateJunction(str(root / "v2"), str(current))
injector.kill()
injector.wait()
injector = subprocess.Popen([str(current / "fzt-picker.exe")])
try:
    app.communicate(timeout=30)
finally:
    for child in (app, injector):
        if child.poll() is None:
            child.kill()

with open(log, "rb") as f:
    f.seek(start)
    lines = f.read().decode(errors="replace").splitlines()
answered = [m.group(1).rsplit("\\", 1)[-1]
            for line in lines if line.startswith(f"[{app.pid}]")
            for m in [re.search(r"not beside the hook in (.+)$", line)] if m]
report = {"answered by": answered, "passed": answered[:1] == ["v1"] and answered[1:] == ["v2"] * (len(answered) - 1) and len(answered) == 3}
(folder / "swap.json").write_text(json.dumps(report, indent=1))
os.rmdir(current)
