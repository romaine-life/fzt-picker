"""Drive the file dialog's frontend DLL. Runs on a hidden desktop: see TESTING.md.

    python dialog_test.py <empty test folder> <scenario> [<dll>]

Scenarios: file, folder, inside, cancel, twice. <dll> defaults to the installed
build. The folder gets test files, screenshots and report-<scenario>.json, and
must be outside AppData: the picker can't walk down through a hidden folder.
The process ends itself after 25 seconds, so a stuck dialog can't stay behind.
"""
import ctypes
import json
import os
import pathlib
import struct
import sys
import threading
import time
import traceback
import zlib
from ctypes import wintypes

INSTALLED = r"C:\Program Files\at-home\fzt-picker\current\fzt_picker_frontend.dll"
WM_KEYDOWN, WM_CHAR = 0x0100, 0x0102
VK_RETURN, VK_ESCAPE, VK_RIGHT = 0x0D, 0x1B, 0x27
PW_CLIENTONLY_RENDERFULL = 3
MIN_DIALOG_WIDTH = 100  # the process also owns zero-size helper windows

folder, which = sys.argv[1], sys.argv[2]
dll_path = sys.argv[3] if len(sys.argv) > 3 else INSTALLED

user32 = ctypes.WinDLL("user32", use_last_error=True)
gdi32 = ctypes.WinDLL("gdi32", use_last_error=True)
kernel32 = ctypes.WinDLL("kernel32", use_last_error=True)
ENUM = ctypes.WINFUNCTYPE(wintypes.BOOL, wintypes.HWND, wintypes.LPARAM)
user32.PostMessageW.argtypes = [wintypes.HWND, wintypes.UINT, wintypes.WPARAM, wintypes.LPARAM]
user32.GetDC.argtypes = [wintypes.HWND]
user32.GetDC.restype = wintypes.HDC
user32.ReleaseDC.argtypes = [wintypes.HWND, wintypes.HDC]
user32.PrintWindow.argtypes = [wintypes.HWND, wintypes.HDC, wintypes.UINT]
user32.GetThreadDesktop.restype = wintypes.HANDLE
user32.EnumDesktopWindows.argtypes = [wintypes.HANDLE, ENUM, wintypes.LPARAM]
user32.IsWindowVisible.argtypes = [wintypes.HWND]
user32.GetClientRect.argtypes = [wintypes.HWND, ctypes.POINTER(wintypes.RECT)]
user32.GetWindowThreadProcessId.argtypes = [wintypes.HWND, ctypes.POINTER(wintypes.DWORD)]
gdi32.CreateCompatibleDC.argtypes = [wintypes.HDC]
gdi32.CreateCompatibleDC.restype = wintypes.HDC
gdi32.CreateCompatibleBitmap.argtypes = [wintypes.HDC, ctypes.c_int, ctypes.c_int]
gdi32.CreateCompatibleBitmap.restype = wintypes.HBITMAP
gdi32.SelectObject.argtypes = [wintypes.HDC, wintypes.HGDIOBJ]
gdi32.SelectObject.restype = wintypes.HGDIOBJ
gdi32.GetDIBits.argtypes = [wintypes.HDC, wintypes.HBITMAP, wintypes.UINT, wintypes.UINT,
                            ctypes.c_void_p, ctypes.c_void_p, wintypes.UINT]
gdi32.DeleteObject.argtypes = [wintypes.HGDIOBJ]
gdi32.DeleteDC.argtypes = [wintypes.HDC]


def dialogs():
    """This process's visible windows big enough to be the dialog."""
    found = []
    pid = kernel32.GetCurrentProcessId()

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


def wait_dialog():
    for _ in range(200):
        found = dialogs()
        if found:
            return found[0]
        time.sleep(0.05)
    raise RuntimeError("no dialog window appeared")


def write_png(path, width, height, bgra):
    rows = bytearray()
    for y in range(height):
        rows.append(0)
        for x in range(width):
            at = (y * width + x) * 4
            rows += bytes((bgra[at + 2], bgra[at + 1], bgra[at]))

    def chunk(kind, data):
        body = kind + data
        return struct.pack(">I", len(data)) + body + struct.pack(">I", zlib.crc32(body))

    with open(path, "wb") as f:
        f.write(b"\x89PNG\r\n\x1a\n" + chunk(b"IHDR", struct.pack(">IIBBBBB", width, height, 8, 2, 0, 0, 0))
                + chunk(b"IDAT", zlib.compress(bytes(rows), 6)) + chunk(b"IEND", b""))


def capture(hwnd, name):
    rect = wintypes.RECT()
    user32.GetClientRect(hwnd, ctypes.byref(rect))
    width, height = rect.right, rect.bottom
    window = user32.GetDC(hwnd)
    dc = gdi32.CreateCompatibleDC(window)
    bitmap = gdi32.CreateCompatibleBitmap(window, width, height)
    old = gdi32.SelectObject(dc, bitmap)
    user32.PrintWindow(hwnd, dc, PW_CLIENTONLY_RENDERFULL)
    header = struct.pack("<IiiHHIIiiII", 40, width, -height, 1, 32, 0, 0, 0, 0, 0, 0)
    pixels = ctypes.create_string_buffer(width * height * 4)
    gdi32.GetDIBits(dc, bitmap, 0, height, pixels, header, 0)
    gdi32.SelectObject(dc, old)
    gdi32.DeleteObject(bitmap)
    gdi32.DeleteDC(dc)
    user32.ReleaseDC(hwnd, window)
    write_png(os.path.join(folder, name + ".png"), width, height, pixels.raw)


def key(hwnd, code):
    user32.PostMessageW(hwnd, WM_KEYDOWN, code, 0)
    time.sleep(0.2)


def type_text(hwnd, text):
    for ch in text:
        user32.PostMessageW(hwnd, WM_CHAR, ord(ch), 0)
        time.sleep(0.04)
    time.sleep(0.2)


dll = ctypes.CDLL(dll_path)
dll.PickFile.restype = ctypes.c_void_p
dll.PickFile.argtypes = [ctypes.c_char_p, ctypes.c_int, ctypes.c_char_p, ctypes.c_size_t]
dll.FreeString.argtypes = [ctypes.c_void_p]


def pick(folders_only):
    pointer = dll.PickFile(b"", folders_only, folder.encode(), 0)
    value = ctypes.string_at(pointer).decode() if pointer else None
    if pointer:
        dll.FreeString(pointer)
    return value


def scenario(name, folders_only, steps):
    result = {}

    def run():
        result["picked"] = pick(folders_only)

    thread = threading.Thread(target=run, daemon=True)
    thread.start()
    hwnd = wait_dialog()
    time.sleep(0.8)
    capture(hwnd, name + "-open")
    steps(hwnd)
    thread.join(5)
    result["finished"] = not thread.is_alive()
    return result


def file_steps(hwnd):
    type_text(hwnd, "take")
    capture(hwnd, "file-typed")
    key(hwnd, VK_RETURN)


def folder_steps(hwnd):
    type_text(hwnd, "sub")
    key(hwnd, VK_RETURN)


def inside_steps(hwnd):
    type_text(hwnd, "sub")
    key(hwnd, VK_RIGHT)
    capture(hwnd, "inside-entered")
    key(hwnd, VK_RETURN)


def cancel_steps(hwnd):
    for _ in range(8):
        if not dialogs():
            return
        key(hwnd, VK_ESCAPE)


def twice():
    """Two dialogs one after the other on one thread, as an app opens them."""
    result = {}

    def run():
        result["first"] = pick(0)
        result["second"] = pick(1)

    thread = threading.Thread(target=run, daemon=True)
    thread.start()
    first = wait_dialog()
    time.sleep(0.8)
    type_text(first, "take")
    key(first, VK_RETURN)
    for _ in range(100):
        now = dialogs()
        if now and now[0] != first:
            break
        time.sleep(0.05)
    time.sleep(0.8)
    second = wait_dialog()
    type_text(second, "sub")
    key(second, VK_RETURN)
    thread.join(5)
    result["finished"] = not thread.is_alive()
    return result


threading.Timer(25, lambda: os._exit(9)).start()
root = pathlib.Path(folder)
(root / "sub" / "deep").mkdir(parents=True, exist_ok=True)
(root / "take-1.wav").write_text("a")
(root / "notes.txt").write_text("b")

report = {}
try:
    if which == "twice":
        report = twice()
    else:
        steps = {"file": (0, file_steps), "folder": (1, folder_steps),
                 "inside": (1, inside_steps), "cancel": (0, cancel_steps)}[which]
        report = scenario(which, *steps)
except Exception:
    report["error"] = traceback.format_exc()
(root / f"report-{which}.json").write_text(json.dumps(report, indent=1))
os._exit(0)
