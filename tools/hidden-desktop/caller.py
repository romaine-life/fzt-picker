"""Start the picker as fzt-automate does: stdin and stderr inherited, stdout captured.

    python caller.py <result.json> <exe> [picker arguments]

Run this in a tmux pane as a plain argument list, with no shell in between. See TESTING.md.
"""
import json
import subprocess
import sys

result, exe, args = sys.argv[1], sys.argv[2], sys.argv[3:]
done = subprocess.run([exe, *args], stdout=subprocess.PIPE)
with open(result, "w") as f:
    json.dump({"stdout": done.stdout.decode(), "exit": done.returncode}, f)
