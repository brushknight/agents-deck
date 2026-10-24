#!/bin/sh
# Renders every screen of the grid and round boards from docs/fixtures/state.json
# into their docs/ folders as PNG (and the 1.47" sheet into out/). Needs a C++
# compiler and Python with pillow (PYTHON=... to pick one).
set -eu
cd "$(dirname "$0")/.."
PY=${PYTHON:-python3}
mkdir -p out
c++ -std=c++17 -O2 -Isrc -I../deck-link/src -I../deck-link/src/proto -DPANEL_MAX_AGENTS=64 -DPANEL_OPTION_LABEL=96 \
  sim/render.cpp -o out/render
for spec in lcd4:waveshare-touch-lcd-4 amoled18:waveshare-touch-amoled-18; do
  layout=${spec%%:*}
  docs=../devices/${spec#*:}/docs
  rm -rf "out/$layout" && mkdir -p "out/$layout" "$docs"
  out/render ../../docs/fixtures/state.json "out/$layout" 300 "$layout"
  rm -f "$docs"/*.png
  for f in out/$layout/*.ppm; do
    n=$(basename "$f" .ppm)
    "$PY" tools/ppm2png.py "$docs/$n.png" "$f" --gap 0 >/dev/null
  done
  echo "$docs"
done
for spec in orbit:waveshare-touch-lcd-28c dial:waveshare-touch-lcd-21; do
  layout=${spec%%:*}
  docs=../devices/${spec#*:}/docs
  rm -rf "out/$layout" && mkdir -p "out/$layout" "$docs"
  out/render ../../docs/fixtures/state.json "out/$layout" 0 "$layout"
  for n in home-3 home-8 home-22 waiting detail detail-stop-held prompt prompt-view not-approved offline connecting; do
    "$PY" tools/ppm2png.py "$docs/$n.png" "out/$layout/$n.ppm" --gap 0 >/dev/null
  done
  "$PY" tools/ppm2png.py "$docs/screens.png" out/$layout/home-3.ppm out/$layout/home-8.ppm out/$layout/home-22.ppm \
    out/$layout/waiting.ppm out/$layout/detail.ppm out/$layout/prompt.ppm out/$layout/not-approved.ppm \
    out/$layout/offline.ppm --cols 4 --gap 8 >/dev/null
  echo "$docs"
done
for spec in selftest28:waveshare-touch-lcd-28c selftest21:waveshare-touch-lcd-21; do
  layout=${spec%%:*}
  docs=../devices/${spec#*:}/docs
  rm -rf "out/$layout" && mkdir -p "out/$layout"
  out/render ../../docs/fixtures/state.json "out/$layout" 0 "$layout"
  "$PY" tools/ppm2png.py "$docs/selftest.png" out/$layout/selftest-1.ppm out/$layout/selftest-2.ppm \
    out/$layout/selftest-3.ppm --cols 3 --gap 8 >/dev/null
done
