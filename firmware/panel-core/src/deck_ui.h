#pragma once
// The deck's look, shared by the touch layouts: the palette, how a status
// paints a tile (AgentLook in clients/flutter/agents_ui), the tile's status
// line (agentMetaLine), and text helpers for the bitmap fonts.
#include <ctype.h>
#include <stdio.h>
#include <string.h>

#include "canvas.h"
#include "model.h"
#include "view.h"

// ---------------------------------------------------------------- theme

struct Theme {
  uint16_t bg, panel, ink, dim, accent, line;
  uint16_t emptyFill, emptyOutline, ringTrack, ringTrackOnAccent, focusedFill, focusedAccent;
};

// The deck's palette (clients/flutter/agents_ui/lib/src/theme.dart,
// agents_style.dart). An AMOLED wants a true-black background.
static inline Theme deckTheme(bool trueBlack) {
  Theme t;
  t.bg = trueBlack ? rgb565(0, 0, 0) : rgb565(0x0A, 0x0A, 0x0A);
  t.panel = rgb565(0x14, 0x14, 0x12);
  t.ink = rgb565(0xE8, 0xE4, 0xDC);
  t.dim = rgb565(0x7A, 0x77, 0x6F);
  t.accent = rgb565(0xFF, 0x4D, 0x00);
  t.line = rgb565(0x26, 0x26, 0x22);
  t.emptyFill = trueBlack ? rgb565(0x06, 0x06, 0x06) : rgb565(0x0B, 0x0B, 0x0B);
  t.emptyOutline = rgb565(0x26, 0x26, 0x22);
  t.ringTrack = rgb565(0x2A, 0x29, 0x25);
  t.ringTrackOnAccent = blend565(t.accent, t.bg, 4);   // 0x400A0A0A over the accent
  t.focusedFill = rgb565(0x24, 0x24, 0x1F);
  t.focusedAccent = rgb565(0xFF, 0x6A, 0x26);
  return t;
}

static inline uint16_t toolColor(const Theme& t, ToolKind k) {
  switch (k) {
    case ToolKind::Claude: return rgb565(0xC9, 0x8B, 0x6B);
    case ToolKind::Codex:  return rgb565(0x4F, 0xB3, 0xA6);
    case ToolKind::Gemini: return rgb565(0x6F, 0x8F, 0xD8);
    default:               return t.dim;
  }
}

// How a status paints its tile (AgentLook.of in agents_style.dart).
struct Look {
  uint16_t fill, body, label, meta, chip, ring, ringTrack;
  bool stripes;
};

static inline bool hungry(const Agent& a) { return a.status == Status::Idle && a.unseen; }

static inline Look lookOf(const Theme& t, const Agent& a) {
  const uint16_t chip = toolColor(t, a.tool);
  Look l{t.panel, t.dim, t.dim, t.dim, chip, t.accent, t.ringTrack, false};
  switch (a.status) {
    case Status::Waiting:
      l = Look{t.accent, t.bg, t.bg, t.bg, t.bg, t.bg, t.ringTrackOnAccent, false};
      break;
    case Status::Error:    l = Look{t.panel, t.ink, t.ink, t.accent, chip, t.accent, t.ringTrack, false}; break;
    case Status::Running:  l = Look{t.panel, t.accent, t.ink, t.dim, chip, t.accent, t.ringTrack, false}; break;
    case Status::Starting: l = Look{t.panel, t.dim, t.ink, t.dim, chip, t.accent, t.ringTrack, false}; break;
    case Status::Idle:
      if (a.unseen) l = Look{t.panel, t.accent, t.ink, t.accent, chip, t.accent, t.ringTrack, true};
      break;
    default: break;
  }
  if (a.focused) l.fill = l.fill == t.accent ? t.focusedAccent : t.focusedFill;   // its terminal is in front
  return l;
}

// ---------------------------------------------------------------- text

// The tile's second line (agentMetaLine in agents_model.dart).
static inline void metaLine(const Agent& a, int64_t nowEpoch, char* out, size_t cap) {
  char age[8];
  fmtAge(a.statusSince ? nowEpoch - a.statusSince : 0, age, sizeof age);
  switch (a.status) {
    case Status::Running: {
      char verb[16];
      size_t i = 0;
      for (; a.actTool[i] && i < sizeof verb - 1; i++) verb[i] = (char)tolower((unsigned char)a.actTool[i]);
      verb[i] = '\0';
      const char* v = verb[0] ? verb : "working";
      if (!strcmp(verb, "todowrite")) v = "plan";
      else if (!strcmp(verb, "multiedit") || !strcmp(verb, "notebookedit")) v = "edit";
      else if (!strcmp(verb, "webfetch")) v = "fetch";
      else if (!strcmp(verb, "websearch")) v = "search";
      else if (!strcmp(verb, "compact")) v = "compacting";
      snprintf(out, cap, "%s" DECK_DOT "%d%% ctx", v, ctxPct(a));
      return;
    }
    case Status::Waiting:
      if (!a.waitKind[0]) { snprintf(out, cap, "needs you"); return; }
      if (!strcmp(a.waitKind, "permission")) {
        char word[16];
        size_t n = 0;
        const char* p = a.promptCtx;
        while (*p == ' ') p++;
        while (*p && *p != ' ' && (unsigned char)*p != 0xC2 && n < sizeof word - 1)
          word[n++] = (char)tolower((unsigned char)*p++);
        word[n] = '\0';
        if (n) snprintf(out, cap, "allow %s?", word); else snprintf(out, cap, "allow?");
      } else if (!strcmp(a.waitKind, "question")) {
        if (a.optCount) snprintf(out, cap, "pick 1 of %u", (unsigned)a.optCount); else snprintf(out, cap, "question");
      } else {
        snprintf(out, cap, "needs input");
      }
      return;
    case Status::Idle:
      if (a.unseen) snprintf(out, cap, "hungry" DECK_DOT "feed me");
      else snprintf(out, cap, "done" DECK_DOT "%s ago", age);
      return;
    case Status::Unknown: snprintf(out, cap, "done" DECK_DOT "%s ago", age); return;
    case Status::Error:    snprintf(out, cap, "%s", a.errMsg[0] ? a.errMsg : "error"); return;
    case Status::Starting: snprintf(out, cap, "starting"); return;
    case Status::Exited:
      snprintf(out, cap, "%s", a.lost ? "lost with tmux" : a.resumable ? "ended" DECK_DOT "resumable" : "exited");
      return;
  }
  out[0] = '\0';
}

static inline int textW(const Font& f, const char* s) { return utf8Len(s) * f.cellW; }

static inline void textCentered(Canvas& c, const Font& f, int cx, int y, const char* s, uint16_t col, int maxW) {
  const int w = textW(f, s);
  const int shown = w > maxW ? (maxW / f.cellW) * f.cellW : w;
  c.text(f, cx - shown / 2, y, s, col, maxW);
}

// Word-wrapped text in at most maxLines lines of maxW pixels; the last line
// ends in "…" when the text doesn't fit. Returns the lines used.
static inline int textWrapped(Canvas& c, const Font& f, int x, int y, const char* s, uint16_t col, int maxW,
                              int maxLines, int lineH = 0) {
  if (!lineH) lineH = f.cellH;
  const int per = maxW / f.cellW;
  if (per <= 0 || maxLines <= 0) return 0;
  int lines = 0;
  const char* p = s;
  char buf[256];
  while (*p && lines < maxLines) {
    while (*p == ' ') p++;
    if (!*p) break;
    // Take up to `per` code points; break at the last space if the text goes on.
    const char* q = p;
    const char* lastSpace = nullptr;
    int n = 0;
    while (*q && n < per) {
      if (*q == ' ') lastSpace = q;
      utf8Next(q);
      n++;
    }
    const char* end = q;
    if (*q && lastSpace && lastSpace > p && lines < maxLines - 1) end = lastSpace;
    if (lines == maxLines - 1 && *q) end = p + strlen(p);   // last line: let text() add the ellipsis
    size_t len = (size_t)(end - p);
    if (len >= sizeof buf) len = sizeof buf - 1;
    memcpy(buf, p, len);
    buf[len] = '\0';
    c.text(f, x, y + lines * lineH, buf, col, maxW);
    lines++;
    p = end;
  }
  return lines;
}

