// oled-tile: a 128x64 OLED and one button.
//
// Lists the agents (title and status). When one is waiting for you it shows
// that prompt and its options; a short press answers with the primary option
// (on an agent without a prompt it focuses that agent's terminal), a long
// press moves to the next agent.
//
// Answering needs scope act: agentctl devices scope <name> act
// Deck Link runs on Serial; nothing else is printed there.
#include <Arduino.h>
#include <Adafruit_GFX.h>
#include <Adafruit_SSD1306.h>
#include <DeckLink.h>
#include <Wire.h>

#ifndef BUTTON_PIN
#define BUTTON_PIN 0 // the BOOT button on most ESP32 dev boards; active low
#endif
#ifndef OLED_ADDR
#define OLED_ADDR 0x3C
#endif
#define LONG_PRESS_MS 600
#define AUTO_SELECT_IDLE_MS 10000 // jump to a waiting agent after this long without a press

static Adafruit_SSD1306 oled(128, 64, &Wire, -1);
static DeckLink deck;
static deck_tiles_t tiles;

static char deviceName[40] = "";
static char selected[12] = ""; // the selected agent's id
static uint32_t lastPressMs;
static uint32_t answerSeq;     // the Answer we wait a Result for
static char message[22] = "";  // a short status line, shown for a few seconds
static uint32_t messageUntil;
static bool dirty = true;

static void say(const char *text, uint32_t ms = 3000)
{
	snprintf(message, sizeof message, "%s", text);
	messageUntil = millis() + ms;
	dirty = true;
}

static void onFrame(deck_link_t *, const deck_frame_t *f)
{
	deck_tiles_handle(&tiles, f);
}

static void onWelcome(deck_link_t *, const char *name, agentsdeck_link_v1_Scope, bool)
{
	snprintf(deviceName, sizeof deviceName, "%s", name);
	dirty = true;
}

static void onResult(deck_link_t *, uint32_t seq, uint32_t code, const char *)
{
	if (seq != answerSeq)
		return;
	answerSeq = 0;
	switch (code) {
	case 204: say("answered"); break;
	case 403: say("needs scope act"); break;
	case 409: say("prompt changed"); break;
	default: {
		char b[22];
		snprintf(b, sizeof b, "error %u", (unsigned)code);
		say(b);
	}
	}
}

static void onStateChange(deck_link_t *) { dirty = true; }

// Copies UTF-8 text into an ASCII line for the built-in font: "…" becomes
// "~", other non-ASCII characters "?"; at most max characters.
static void ascii(char *dst, size_t size, const char *src, size_t max)
{
	size_t n = 0;
	if (max > size - 1)
		max = size - 1;
	for (const uint8_t *p = (const uint8_t *)src; *p && n < max;) {
		if (*p < 0x80) {
			dst[n++] = (*p == '\n' || *p == '\t') ? ' ' : (char)*p;
			p++;
			continue;
		}
		bool ellipsis = p[0] == 0xE2 && p[1] == 0x80 && p[2] == 0xA6;
		dst[n++] = ellipsis ? '~' : '?';
		p++;
		while ((*p & 0xC0) == 0x80)
			p++;
	}
	dst[n] = '\0';
}

static const deck_tile_t *selectedTile()
{
	const deck_tile_t *t = selected[0] ? deck_tiles_find(&tiles, selected) : nullptr;
	if (!t && tiles.count > 0)
		t = &tiles.tiles[0];
	return t;
}

static void selectTile(const deck_tile_t *t)
{
	snprintf(selected, sizeof selected, "%s", t ? t->id : "");
	dirty = true;
}

static void line(int row, const char *text, bool inverse = false)
{
	char buf[22];
	ascii(buf, sizeof buf, text, 21);
	if (inverse) {
		oled.fillRect(0, row * 8, 128, 8, SSD1306_WHITE);
		oled.setTextColor(SSD1306_BLACK);
	} else {
		oled.setTextColor(SSD1306_WHITE);
	}
	oled.setCursor(0, row * 8);
	oled.print(buf);
}

static void draw()
{
	char b[64];
	oled.clearDisplay();
	uint32_t now = millis();
	bool showMessage = message[0] && (int32_t)(messageUntil - now) > 0;

	if (deck.state() == DECK_LINK_OFFLINE) {
		line(0, "deck offline", true);
		line(2, "waiting for the deck");
	} else if (!deck.online()) {
		line(0, "connecting...", true);
	} else if (!deck.approved()) {
		line(0, "not approved yet", true);
		line(2, "on the Mac, run:");
		line(3, "agentctl devices");
		snprintf(b, sizeof b, "approve %s", deviceName);
		line(4, b);
		if (strlen(deviceName) > 13)
			line(5, deviceName + 13);
	} else {
		const deck_tile_t *t = selectedTile();
		int waiting = deck_tiles_count_status(&tiles, agentsdeck_link_v1_Status_STATUS_WAITING);
		snprintf(b, sizeof b, "%d agents  %d waiting", tiles.count, waiting);
		line(0, showMessage ? message : b, true);
		if (!t) {
			line(2, "no agents");
		} else if (t->has_prompt) {
			line(1, t->title);
			char p[64];
			ascii(p, sizeof p, t->prompt_title, 42);
			line(2, p);
			if (strlen(p) > 21)
				line(3, p + 21);
			int row = 4;
			for (int i = 0; i < t->option_count && row < 8; i++, row++) {
				snprintf(b, sizeof b, "%c%s %s%s", t->options[i].primary ? '>' : ' ', t->options[i].key,
					 t->options[i].selected ? "[x] " : "", t->options[i].label);
				line(row, b, t->options[i].primary);
			}
			if (t->option_count == 0)
				line(4, "answer in the terminal");
		} else {
			// A list of up to 7 agents around the selected one.
			int sel = deck_tiles_index(&tiles, t->id);
			int first = sel > 6 ? sel - 6 : 0;
			for (int i = first, row = 1; i < tiles.count && row < 8; i++, row++) {
				const deck_tile_t *x = &tiles.tiles[i];
				char title[22];
				ascii(title, sizeof title, x->title, 12);
				snprintf(b, sizeof b, "%-12s %s", title, deck_status_name(x->status));
				line(row, b, i == sel);
			}
		}
	}
	oled.display();
}

static void shortPress()
{
	const deck_tile_t *t = selectedTile();
	if (!t || !deck.approved())
		return;
	if (t->has_prompt) {
		int i = deck_tile_primary(t);
		if (i < 0) {
			say("no default option");
			return;
		}
		answerSeq = deck.answer(t->id, t->prompt_id, t->options[i].key);
		say("sending...");
	} else {
		deck.focus(t->id);
		say("focused");
	}
}

static void longPress()
{
	if (tiles.count == 0)
		return;
	const deck_tile_t *t = selectedTile();
	int i = t ? deck_tiles_index(&tiles, t->id) : -1;
	selectTile(&tiles.tiles[(i + 1) % tiles.count]);
}

static void button(uint32_t now)
{
	static bool down;
	static bool longDone;
	static uint32_t downAt;
	bool pressed = digitalRead(BUTTON_PIN) == LOW;
	if (pressed && !down && now - downAt > 30) { // debounced press
		down = true;
		longDone = false;
		downAt = now;
	} else if (pressed && down && !longDone && now - downAt >= LONG_PRESS_MS) {
		longDone = true;
		lastPressMs = now;
		longPress();
	} else if (!pressed && down && now - downAt > 30) {
		down = false;
		if (!longDone) {
			lastPressMs = now;
			shortPress();
		}
		downAt = now;
	}
}

void setup()
{
	Serial.begin(115200);
	pinMode(BUTTON_PIN, INPUT_PULLUP);
	Wire.begin();
	oled.begin(SSD1306_SWITCHCAPVCC, OLED_ADDR);
	oled.cp437(true);
	oled.setTextSize(1);
	deck_tiles_init(&tiles);

	DeckLinkConfig cfg = {};
	cfg.board_id = DeckLink::chipId();
	cfg.type = "oled-tile";
	cfg.firmware = "0.1.0";
	cfg.display_width = 128;
	cfg.display_height = 64;
	cfg.inputs[0] = agentsdeck_link_v1_Input_INPUT_BUTTON;
	cfg.input_count = 1;
	cfg.fields = agentsdeck_link_v1_FieldGroup_FIELDS_TILE | agentsdeck_link_v1_FieldGroup_FIELDS_PROMPT;
	cfg.on_frame = onFrame;
	cfg.on_welcome = onWelcome;
	cfg.on_result = onResult;
	cfg.on_online = onStateChange;
	cfg.on_offline = onStateChange;
	deck.begin(Serial, cfg);
}

void loop()
{
	static uint32_t seenVersion, lastDraw;
	deck.loop();
	uint32_t now = millis();
	button(now);

	// Nobody is pressing: follow the first agent that needs you.
	const deck_tile_t *w = deck_tiles_first_waiting(&tiles);
	const deck_tile_t *t = selectedTile();
	if (w && t && !t->has_prompt && now - lastPressMs > AUTO_SELECT_IDLE_MS)
		selectTile(w);

	if (tiles.version != seenVersion) {
		seenVersion = tiles.version;
		dirty = true;
	}
	if (dirty || now - lastDraw > 500) { // also expires messages
		dirty = false;
		lastDraw = now;
		draw();
	}
	delay(5);
}
