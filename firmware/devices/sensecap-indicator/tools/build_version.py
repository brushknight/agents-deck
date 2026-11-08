"""Identify the actual source used to build a board (Hello has 23 usable bytes)."""
import subprocess

Import("env")

# pioarduino 55.03.311 requires this Core version (upstream issue #556).
if env["PIOENV"].startswith("idf5"):
    from platformio import __version__ as core_version
    if core_version != "6.1.19":
        raise RuntimeError("IDF5 requires PlatformIO Core 6.1.19; use uvx --from platformio==6.1.19 pio")


def git(*args):
    return subprocess.check_output(
        ["git", "-C", env["PROJECT_DIR"], *args], text=True, stderr=subprocess.DEVNULL
    ).strip()


try:
    revision = git("rev-parse", "--short=7", "HEAD")[:7]
    dirty = bool(git("status", "--porcelain", "--untracked-files=normal"))
    version = "0.2.0+" + revision + ("-dirty" if dirty else "")
except (OSError, subprocess.CalledProcessError):
    version = "0.2.0+unknown"

assert len(version) < 24
env.Append(CPPDEFINES=[("FW_VERSION", '\\"' + version + '\\"')])
print("SenseCAP firmware: " + version)
