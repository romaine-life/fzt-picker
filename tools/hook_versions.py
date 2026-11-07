"""Which release's hook each running process holds: a read of the live PC, changing nothing.

    python tools/hook_versions.py

Each process is matched to a version folder under C:\\Program Files\\at-home\\fzt-picker by
its hook's in-memory PE header (the loader rewrites only ImageBase, which is masked).
After a release, every process should move to the new version as its windows next do
anything; one held on an older version is not getting the new hook (FP-6).
"""
import ctypes
import glob
import os
from collections import Counter
from ctypes import wintypes

PROCESS_QUERY_INFORMATION, PROCESS_VM_READ = 0x0400, 0x0010
HEADER = 0x400
psapi = ctypes.WinDLL("psapi")
kernel32 = ctypes.WinDLL("kernel32")
kernel32.OpenProcess.restype = wintypes.HANDLE
kernel32.ReadProcessMemory.argtypes = [wintypes.HANDLE, ctypes.c_void_p, ctypes.c_void_p,
                                       ctypes.c_size_t, ctypes.POINTER(ctypes.c_size_t)]
psapi.EnumProcessModulesEx.argtypes = [wintypes.HANDLE, ctypes.POINTER(ctypes.c_void_p), wintypes.DWORD,
                                       ctypes.POINTER(wintypes.DWORD), wintypes.DWORD]
psapi.GetModuleFileNameExW.argtypes = [wintypes.HANDLE, ctypes.c_void_p, wintypes.LPWSTR, wintypes.DWORD]
psapi.GetProcessImageFileNameW.argtypes = [wintypes.HANDLE, wintypes.LPWSTR, wintypes.DWORD]


def masked(header):
    """The header without ImageBase, which the loader rewrites when it relocates the DLL."""
    pe = int.from_bytes(header[0x3C:0x40], "little")
    at = pe + 24 + 24  # PE32+ optional header: ImageBase at offset 24
    return header[:at] + header[at + 8:]


versions = {}
for path in glob.glob(r"C:\Program Files\at-home\fzt-picker\v*\fzt_picker_hook.dll"):
    with open(path, "rb") as f:
        versions[masked(f.read(HEADER))] = path.split("\\")[-2]

pids = (wintypes.DWORD * 4096)()
used = wintypes.DWORD()
psapi.EnumProcesses(pids, ctypes.sizeof(pids), ctypes.byref(used))
rows = []
for pid in pids[: used.value // 4]:
    process = kernel32.OpenProcess(PROCESS_QUERY_INFORMATION | PROCESS_VM_READ, False, pid)
    if not process:
        continue
    modules = (ctypes.c_void_p * 2048)()
    need = wintypes.DWORD()
    if psapi.EnumProcessModulesEx(process, modules, ctypes.sizeof(modules), ctypes.byref(need), 3):
        for module in modules[: need.value // ctypes.sizeof(ctypes.c_void_p)]:
            name = ctypes.create_unicode_buffer(1024)
            psapi.GetModuleFileNameExW(process, module, name, 1024)
            if not name.value.lower().endswith("fzt_picker_hook.dll"):
                continue
            header = ctypes.create_string_buffer(HEADER)
            got = ctypes.c_size_t()
            kernel32.ReadProcessMemory(process, module, header, HEADER, ctypes.byref(got))
            image = ctypes.create_unicode_buffer(1024)
            psapi.GetProcessImageFileNameW(process, image, 1024)
            loaded_as = name.value.split("\\")[-2]
            rows.append((versions.get(masked(header.raw), "unknown"), loaded_as, os.path.basename(image.value), pid))
    kernel32.CloseHandle(process)

print("versions installed:", ", ".join(sorted(versions.values())))
print("processes per version:", dict(Counter(row[0] for row in rows)))
for version, loaded_as, image, pid in sorted(rows):
    print(f"{version:10} loaded as {loaded_as:10} {image} {pid}")
