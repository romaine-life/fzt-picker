"""Run a command on a hidden desktop, and wait for it.

    python hidden_launch.py <desktop name> <command line>

The desktop is never switched to, so nothing on it can reach the screen or take
focus. It lasts while a process is on it, so a server started here stays there.
Use a new name for each run. See TESTING.md.
"""
import ctypes
import sys
from ctypes import wintypes

GENERIC_ALL = 0x10000000
CREATE_NO_WINDOW = 0x08000000
INFINITE = 0xFFFFFFFF

user32 = ctypes.WinDLL("user32", use_last_error=True)
kernel32 = ctypes.WinDLL("kernel32", use_last_error=True)


class STARTUPINFO(ctypes.Structure):
    _fields_ = [
        ("cb", wintypes.DWORD), ("lpReserved", wintypes.LPWSTR), ("lpDesktop", wintypes.LPWSTR),
        ("lpTitle", wintypes.LPWSTR), ("dwX", wintypes.DWORD), ("dwY", wintypes.DWORD),
        ("dwXSize", wintypes.DWORD), ("dwYSize", wintypes.DWORD), ("dwXCountChars", wintypes.DWORD),
        ("dwYCountChars", wintypes.DWORD), ("dwFillAttribute", wintypes.DWORD), ("dwFlags", wintypes.DWORD),
        ("wShowWindow", wintypes.WORD), ("cbReserved2", wintypes.WORD), ("lpReserved2", ctypes.c_void_p),
        ("hStdInput", wintypes.HANDLE), ("hStdOutput", wintypes.HANDLE), ("hStdError", wintypes.HANDLE),
    ]


class PROCESS_INFORMATION(ctypes.Structure):
    _fields_ = [
        ("hProcess", wintypes.HANDLE), ("hThread", wintypes.HANDLE),
        ("dwProcessId", wintypes.DWORD), ("dwThreadId", wintypes.DWORD),
    ]


user32.CreateDesktopW.restype = wintypes.HANDLE
user32.CreateDesktopW.argtypes = [wintypes.LPCWSTR, wintypes.LPCWSTR, ctypes.c_void_p,
                                  wintypes.DWORD, wintypes.DWORD, ctypes.c_void_p]
kernel32.CreateProcessW.argtypes = [wintypes.LPCWSTR, wintypes.LPWSTR, ctypes.c_void_p, ctypes.c_void_p,
                                    wintypes.BOOL, wintypes.DWORD, ctypes.c_void_p, wintypes.LPCWSTR,
                                    ctypes.POINTER(STARTUPINFO), ctypes.POINTER(PROCESS_INFORMATION)]
kernel32.WaitForSingleObject.argtypes = [wintypes.HANDLE, wintypes.DWORD]
kernel32.GetExitCodeProcess.argtypes = [wintypes.HANDLE, ctypes.POINTER(wintypes.DWORD)]

name, command = sys.argv[1], sys.argv[2]
desktop = user32.CreateDesktopW(name, None, None, 0, GENERIC_ALL, None)
if not desktop:
    sys.exit(f"CreateDesktopW failed: {ctypes.get_last_error()}")

startup = STARTUPINFO()
startup.cb = ctypes.sizeof(startup)
startup.lpDesktop = "winsta0\\" + name
process = PROCESS_INFORMATION()
line = ctypes.create_unicode_buffer(command)
if not kernel32.CreateProcessW(None, line, None, None, False, CREATE_NO_WINDOW, None, None,
                               ctypes.byref(startup), ctypes.byref(process)):
    sys.exit(f"CreateProcessW failed: {ctypes.get_last_error()}")
kernel32.WaitForSingleObject(process.hProcess, INFINITE)
code = wintypes.DWORD()
kernel32.GetExitCodeProcess(process.hProcess, ctypes.byref(code))
sys.exit(code.value)
