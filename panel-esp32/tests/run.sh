#!/bin/bash
# Host tests for lib/core.
#
# Needs the ArduinoJson headers (run `pio pkg install` once, or point
# ARDUINOJSON_SRC at them) and the sample fleet of the agents-deck repository.
set -euo pipefail
cd "$(dirname "$0")/.."

AJ=${ARDUINOJSON_SRC:-.pio/libdeps/waveshare-s3-147b/ArduinoJson/src}
if [ ! -f "$AJ/ArduinoJson.h" ]; then
  echo "ArduinoJson headers not found in $AJ — run: pio pkg install" >&2
  exit 2
fi

# This folder lives inside the agents-deck repository, or next to a clone of it.
FIXTURE=""
for repo in .. ../agents-deck; do
  [ -f "$repo/docs/fixtures/state.json" ] && FIXTURE="$repo/docs/fixtures/state.json" && break
done
if [ -z "$FIXTURE" ]; then
  echo "docs/fixtures/state.json of agents-deck not found in .. or ../agents-deck" >&2
  exit 2
fi

mkdir -p out
shopt -s nullglob
for src in tests/test_*.cpp; do
  name=$(basename "$src" .cpp)
  clang++ -std=c++17 -Wall -Wextra -Werror -g -fsanitize=address,undefined \
    -Ilib/core -I"$AJ" -DFIXTURE="\"$FIXTURE\"" "$src" -o "out/$name"
  "out/$name"
  echo "ok  $name"
done
echo "ALL TESTS PASSED"
