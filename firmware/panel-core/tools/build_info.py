# PlatformIO pre-script for every board on panel-core: which board this
# firmware is for and which version it reports. In each device project's
# platformio.ini:  extra_scripts = pre:<path to here>/build_info.py
#
#   AGENTS_DECK_BOARD    the device folder's name (firmware/devices/<board>,
#                        also for its idf5/ project): a board takes only
#                        firmware updates for its own board (ota.h).
#   AGENTS_DECK_VERSION  from the environment variable of the same name when
#                        it is set (the release workflow exports it): the
#                        version in Hello. Unset: the one in main.cpp.
import os
import re
from pathlib import Path

Import("env")  # noqa: F821 (SCons)

project = Path(env.subst("$PROJECT_DIR")).resolve()  # noqa: F821
board = project.parent.name if project.name == "idf5" else project.name
defines = [("AGENTS_DECK_BOARD", env.StringifyMacro(board))]  # noqa: F821

version = os.environ.get("AGENTS_DECK_VERSION", "").strip()
if version:
    # Hello.firmware holds 23 bytes; keep it to what a version looks like.
    if not re.fullmatch(r"[A-Za-z0-9][A-Za-z0-9._+-]{0,22}", version):
        raise SystemExit(f"build_info.py: AGENTS_DECK_VERSION {version!r} isn't a version (at most 23 of A-Z a-z 0-9 . _ + -)")
    defines.append(("AGENTS_DECK_VERSION", env.StringifyMacro(version)))  # noqa: F821

env.Append(CPPDEFINES=defines)  # noqa: F821
print(f"build_info.py: board {board}, version {version or 'from main.cpp'}")
