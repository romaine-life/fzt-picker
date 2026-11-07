"""Test the whole dialog path through the COM hook. Runs on a hidden desktop: see TESTING.md.

    python hook_test.py <empty test folder> <scenario> <test-trigger.exe> [<injector folder>]

Scenarios: file, folder, cancel. It starts the injector (the installed one unless
given another folder) and test-trigger.exe, which opens the system file dialog.
The hook swaps in fzt's dialog, this drives it with keys, and test-trigger's
output says what came back. The injector's hook reaches only this desktop, and
only the injector started here is stopped. The folder must be outside AppData,
and the process ends itself after 40 seconds.
"""
import ctypes
import json
import os
import pathlib
import subprocess
import sys
import threading
import time
from ctypes import wintypes

INSTALLED = r"C:\Program Files\at-home\fzt-picker\current"
WM_KEYDOWN, WM_CHAR = 0x0100, 0x0102
VK_RETURN, VK_ESCAPE, VK_LEFT, VK_RIGHT = 0x0D, 0x1B, 0x25, 0x27
MIN_DIALOG_WIDTH = 100  # the process also owns zero-size helper windows

folder, which, trigger = sys.argv[1], sys.argv[2], sys.argv[3]
injector_dir = sys.argv[4] if len(sys.argv) > 4 else INSTALLED

user32 = ctypes.WinDLL("user32", use_last_error=True)
kernel32 = ctypes.WinDLL("kernel32", use_last_error=True)
ENUM = ctypes.WINFUNCTYPE(wintypes.BOOL, wintypes.HWND, wintypes.LPARAM)
user32.PostMessageW.argtypes = [wintypes.HWND, wintypes.UINT, wintypes.WPARAM, wintypes.LPARAM]
user32.GetThreadDesktop.restype = wintypes.HANDLE
user32.EnumDesktopWindows.argtypes = [wintypes.HANDLE, ENUM, wintypes.LPARAM]
user32.IsWindowVisible.argtypes = [wintypes.HWND]
user32.GetClientRect.argtypes = [wintypes.HWND, ctypes.POINTER(wintypes.RECT)]
user32.GetWindowThreadProcessId.argtypes = [wintypes.HWND, ctypes.POINTER(wintypes.DWORD)]


def dialogs(pid):
    found = []

    def each(hwnd, _):
        owner = wintypes.DWORD()
        user32.GetWindowThreadProcessId(hwnd, ctypes.byref(owner))
        rect = wintypes.RECT()
        user32.GetClientRect(hwnd, ctypes.byref(rect))
        if owner.value == pid and user32.IsWindowVisible(hwnd) and rect.right > MIN_DIALOG_WIDTH:
            found.append(hwnd)
        return True

    user32.EnumDesktopWindows(user32.GetThreadDesktop(kernel32.GetCurrentThreadId()), ENUM(each), 0)
    return found


def key(hwnd, code):
    user32.PostMessageW(hwnd, WM_KEYDOWN, code, 0)
    time.sleep(0.3)


def type_text(hwnd, text):
    for ch in text:
        user32.PostMessageW(hwnd, WM_CHAR, ord(ch), 0)
        time.sleep(0.04)
    time.sleep(0.3)


root = pathlib.Path(folder)
(root / "sub").mkdir(parents=True, exist_ok=True)
(root / "take-1.wav").write_text("a")
# The dialog opens in the app's start folder, the user folder for test-trigger: go up to
# the drive roots with Left, then walk down by name, as a person would.
steps = [root.drive] + list(root.parts[1:])

injector = subprocess.Popen([os.path.join(injector_dir, "fzt-picker.exe")])
children = [injector]


def give_up():
    for child in children:
        child.kill()
    os._exit(9)


threading.Timer(40, give_up).start()
report = {}
try:
    time.sleep(1.5)
    args = [trigger] + (["--folders"] if which == "folder" else [])
    dialog = subprocess.Popen(args, stdout=subprocess.PIPE, text=True)
    children.append(dialog)
    hwnd = None
    for _ in range(200):
        found = dialogs(dialog.pid)
        if found:
            hwnd = found[0]
            break
        if dialog.poll() is not None:
            break
        time.sleep(0.05)
    report["dialog shown"] = hwnd is not None
    if hwnd:
        time.sleep(0.8)
        if which == "cancel":
            for _ in range(10):
                if not dialogs(dialog.pid):
                    break
                key(hwnd, VK_ESCAPE)
        else:
            for _ in range(len(pathlib.Path.home().parts) + 1):
                key(hwnd, VK_LEFT)
            for step in steps:
                type_text(hwnd, step)
                key(hwnd, VK_RIGHT)
            type_text(hwnd, "take" if which == "file" else "sub")
            key(hwnd, VK_RETURN)
    out, _ = dialog.communicate(timeout=10)
    report["test-trigger"] = out.strip().splitlines()[-1] if out.strip() else ""
except Exception as e:
    report["error"] = repr(e)
finally:
    for child in children:
        if child.poll() is None:
            child.kill()
(root / f"hook-{which}.json").write_text(json.dumps(report, indent=1))
os._exit(0)
