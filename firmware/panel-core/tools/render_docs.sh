#!/bin/sh
# Renders every screen of the grid boards from docs/fixtures/state.json into
# their docs/ folders as PNG (and the 1.47" sheet into out/). Needs a C++
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
  for n in grid grid-page2 card prompt prompt-view question notice-connecting notice-unapproved offline; do
    "$PY" tools/ppm2png.py "$docs/$n.png" "out/$layout/$n.ppm" --gap 0 >/dev/null
  done
  echo "$docs"
done
