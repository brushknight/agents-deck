# esptool 4.8 (pinned in platformio.ini) needs intelhex, which PlatformIO's
# Python doesn't come with. Install it there before the build needs it.
Import("env")

try:
    import intelhex  # noqa: F401
except ImportError:
    env.Execute('"$PYTHONEXE" -m pip install --quiet intelhex')
