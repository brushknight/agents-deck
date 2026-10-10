#pragma once
// Server-Sent Events framing, reduced to what the agents-deck stream needs:
// find where each "data:" payload starts. The payload itself is read by the
// JSON parser straight from the socket, so a line is never buffered here.
#include <stdint.h>

enum class SseEvent : uint8_t { None, DataStart };

struct SseScanner {
  // Feed every byte of the stream that the JSON parser did not consume.
  // Returns DataStart on the ':' of a "data:" field at the start of a line;
  // the payload (after one optional space) begins with the next byte.
  SseEvent feed(char c) {
    if (c == '\n' || c == '\r') {
      matched_ = 0;
      return SseEvent::None;
    }
    if (matched_ < 0) return SseEvent::None;          // not a data line: skip to its end
    static const char kField[] = "data:";
    if (c != kField[matched_]) {
      matched_ = -1;
      return SseEvent::None;
    }
    if (++matched_ < 5) return SseEvent::None;
    matched_ = -1;                                    // the rest of the line is payload
    return SseEvent::DataStart;
  }

  void reset() { matched_ = 0; }

 private:
  int8_t matched_ = 0;   // chars of "data:" seen at line start; -1 = ignore until newline
};
