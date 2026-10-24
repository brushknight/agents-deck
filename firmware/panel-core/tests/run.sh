#!/bin/bash
# Host tests for panel-core: the drawing, the view logic and the Deck Link
# adapter, with AddressSanitizer and UBSan. Runs with clang or gcc
# (CC / CXX). Needs only this repository: the Deck Link kit in
# firmware/deck-link and the sample fleet in docs/fixtures/state.json.
set -euo pipefail
cd "$(dirname "$0")/.."

KIT=../deck-link/src
FIXTURE=../../docs/fixtures/state.json
CC=${CC:-cc}
CXX=${CXX:-c++}
SAN="-g -fsanitize=address,undefined -fno-sanitize-recover=undefined"
for f in "$KIT/deck_link.h" "$FIXTURE"; do
  [ -f "$f" ] || { echo "missing $f (run from the agents deck repository)" >&2; exit 2; }
done

mkdir -p out/kit
# The kit's C sources, once.
KIT_OBJS=()
for src in "$KIT"/deck_link.c "$KIT"/cobs.c "$KIT"/crc16.c "$KIT"/proto/*.c; do
  obj="out/kit/$(basename "$src" .c).o"
  "$CC" -std=c99 -Wall -Wextra $SAN -I"$KIT" -c "$src" -o "$obj"
  KIT_OBJS+=("$obj")
done

for src in tests/test_*.cpp; do
  name=$(basename "$src" .cpp)
  "$CXX" -std=c++17 -Wall -Wextra -Werror $SAN \
    -Isrc -I"$KIT" -I"$KIT/proto" -DFIXTURE="\"$FIXTURE\"" "$src" "${KIT_OBJS[@]}" -o "out/$name"
  "out/$name"
  echo "ok  $name"
done
# The board's logic on stdin/stdout (sim/panel_sim.cpp): build it so it keeps compiling.
"$CXX" -std=c++17 -Wall -Wextra -Werror $SAN -Isrc -I"$KIT" -I"$KIT/proto" sim/panel_sim.cpp "${KIT_OBJS[@]}" -o out/panel_sim
echo "ok  built out/panel_sim"
# The round boards' logic the same way (sim/round_sim.cpp); backend/internal/link/round_test.go runs it.
"$CXX" -std=c++17 -Wall -Wextra -Werror $SAN -DPANEL_MAX_AGENTS=64 -DPANEL_OPTION_LABEL=96 -Isrc -I"$KIT" -I"$KIT/proto" \
  sim/round_sim.cpp "${KIT_OBJS[@]}" -o out/round_sim
echo "ok  built out/round_sim"
echo "ALL TESTS PASSED"
