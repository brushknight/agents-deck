"""Identify the actual source used to build a board (Hello has 23 usable bytes)."""
import subprocess

Import("env")


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
