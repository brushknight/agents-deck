#include <assert.h>
#include <string>
#include <vector>
#include "sse.h"

// Feeds a whole stream and returns the text that follows each DataStart, up to the line end.
static std::vector<std::string> payloads(const std::string& in) {
  SseScanner sc;
  std::vector<std::string> out;
  for (size_t i = 0; i < in.size(); i++) {
    if (sc.feed(in[i]) == SseEvent::DataStart) {
      size_t end = in.find_first_of("\r\n", i + 1);
      out.push_back(in.substr(i + 1, end == std::string::npos ? end : end - i - 1));
    }
  }
  return out;
}

int main() {
  auto p = payloads("event: state\ndata: {\"a\":1}\n\n: ping\n\ndata:{\"b\":2}\n\n");
  assert(p.size() == 2);
  assert(p[0] == " {\"a\":1}");   // the JSON parser skips the leading space
  assert(p[1] == "{\"b\":2}");

  assert(payloads("datax: 1\n").empty());
  assert(payloads("id: data: 1\n").empty());
  assert(payloads("xdata: 1\n").empty());
  assert(payloads("dat\na: 1\n").empty());
  assert(payloads(": data: comment\n").empty());

  // CRLF line ends, and a data line that is the very first line.
  p = payloads("data: 1\r\nevent: x\r\ndata: 2\r\n\r\n");
  assert(p.size() == 2 && p[0] == " 1" && p[1] == " 2");

  // "data:" inside a payload does not start a second payload.
  p = payloads("data: {\"k\":\"data: x\"}\n");
  assert(p.size() == 1);

  // reset() forgets a half-read line.
  SseScanner sc;
  for (char c : std::string("event: sta")) sc.feed(c);
  sc.reset();
  std::string d = "data:";
  SseEvent last = SseEvent::None;
  for (char c : d) last = sc.feed(c);
  assert(last == SseEvent::DataStart);
  return 0;
}
