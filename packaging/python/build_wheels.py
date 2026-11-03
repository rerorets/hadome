"""Builds the Python wheels of one release from its binaries.

    python3 build_wheels.py <version> <binaries dir> <out dir>

<binaries dir> holds <os>_<arch>/hadome[.exe]. Each binary becomes one wheel whose script directory
holds it, so pip puts it on PATH. Platforms whose binary is missing are left out. Only the standard
library is used, so the script runs anywhere Python does.
"""

import base64
import hashlib
import os
import re
import sys
import zipfile

# Linux binaries are linked statically, so one wheel serves glibc and musl systems alike.
TAGS = {
    ("linux", "amd64"): "manylinux_2_17_x86_64.manylinux2014_x86_64.musllinux_1_1_x86_64",
    ("linux", "arm64"): "manylinux_2_17_aarch64.manylinux2014_aarch64.musllinux_1_1_aarch64",
    ("darwin", "amd64"): "macosx_11_0_x86_64",  # Go needs macOS 11 or later
    ("darwin", "arm64"): "macosx_11_0_arm64",
    ("windows", "amd64"): "win_amd64",
    ("windows", "arm64"): "win_arm64",
}


def pep440(version):
    """The release version as Python packaging writes it: 1.2.0, or 1.2.0rc1 for 1.2.0-rc.1."""
    m = re.fullmatch(r"(\d+\.\d+\.\d+)(?:-(a|alpha|b|beta|rc)\.?(\d+))?", version)
    if not m:
        sys.exit(f"{version} is not a release version (X.Y.Z, or X.Y.Z-rc.N)")
    pre = {"alpha": "a", "beta": "b"}.get(m[2], m[2])
    return m[1] + (f"{pre}{m[3]}" if pre else "")


def record_line(name, data):
    """The RECORD line of one file of the wheel: its name, SHA-256 and size."""
    digest = base64.urlsafe_b64encode(hashlib.sha256(data).digest()).rstrip(b"=").decode()
    return f"{name},sha256={digest},{len(data)}"


def build(version, binary, exe, tag, out):
    """Writes the wheel of one platform into out and returns its path."""
    dist = f"hadome-{version}"
    files = {
        "hadome/__init__.py": b'"""hadome: stops regressions in code and tests, not progress. Run it as `hadome`."""\n',
        f"{dist}.data/scripts/{exe}": open(binary, "rb").read(),
        f"{dist}.dist-info/METADATA": (
            "Metadata-Version: 2.1\nName: hadome\n"
            f"Version: {version}\nSummary: Reports the violations a change adds and the tests it weakens.\n"
            "Home-page: https://github.com/rerorets/hadome\nLicense: MIT\nRequires-Python: >=3.8\n"
        ).encode(),
        f"{dist}.dist-info/WHEEL": (
            "Wheel-Version: 1.0\nGenerator: hadome build_wheels.py\nRoot-Is-Purelib: false\n"
            + "".join(f"Tag: py3-none-{t}\n" for t in tag.split("."))
        ).encode(),
    }
    record = f"{dist}.dist-info/RECORD"
    lines = [record_line(n, d) for n, d in files.items()] + [f"{record},,"]
    files[record] = ("\n".join(lines) + "\n").encode()
    path = os.path.join(out, f"{dist}-py3-none-{tag}.whl")
    with zipfile.ZipFile(path, "w", zipfile.ZIP_DEFLATED) as whl:
        for name, data in files.items():
            info = zipfile.ZipInfo(name, date_time=(2020, 1, 1, 0, 0, 0))
            # pip keeps the executable bit only when the mode also says "regular file".
            info.external_attr = (0o100755 if ".data/scripts/" in name else 0o100644) << 16
            info.compress_type = zipfile.ZIP_DEFLATED
            whl.writestr(info, data)
    return path


def main():
    """Builds a wheel for every platform whose binary is present."""
    if len(sys.argv) != 4:
        sys.exit("usage: python3 build_wheels.py <version> <binaries dir> <out dir>")
    version, binaries, out = sys.argv[1:]
    version = pep440(version)
    os.makedirs(out, exist_ok=True)
    for (goos, goarch), tag in TAGS.items():
        exe = "hadome.exe" if goos == "windows" else "hadome"
        binary = os.path.join(binaries, f"{goos}_{goarch}", exe)
        if os.path.exists(binary):
            print("built", build(version, binary, exe, tag, out))


if __name__ == "__main__":
    main()
