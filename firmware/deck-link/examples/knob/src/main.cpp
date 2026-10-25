// knob: a rotary encoder with a push button.
//
// Turn to pick an agent; press to open its prompt (or, without one, to focus
// its terminal). In a prompt, turn to pick an option and press to answer;
// a long press goes back.
//
// Deck Link runs on the native USB port (Serial). What the knob points at is
// printed on the UART port (Serial0) and, with -DKNOB_OLED=1, shown on an
// SSD1306. Answering and focusing need scope act:
//   agentctl devices scope <name> act
#include <Arduino.h>
#include <DeckLink.h>

#if KNOB_OLED
#include <Adafruit_GFX.h>
#include <Adafruit_SSD1306.h>
#include <Wire.h>
static Adafruit_SSD1306 oled(128, 64, &Wire, -1);
#endif

#ifndef ENC_A
#define ENC_A 4
#endif
#ifndef ENC_B
#define ENC_B 5
#endif
#ifndef ENC_BUTTON
#define ENC_BUTTON 6 // active low
#endif
#ifndef ENC_STEPS
#define ENC_STEPS 4 // quadrature steps per detent
#endif
#define LONG_PRESS_MS 600

#define UI Serial0 // never Serial: that's the Deck Link port

static DeckLink deck;
static deck_tiles_t tiles;

enum Mode { PICK_AGENT, PICK_OPTION };
static Mode mode = PICK_AGENT;
static char agentId[12] = ""; // the agent the knob points at
static int option;            // the option it points at (PICK_OPTION)
static uint32_t answerSeq;
static char status[32] = "";
static bool dirty = true;
static char deviceName[40] = "";

// ---- encoder (interrupt-driven quadrature decoder) ----------------------------

static volatile int32_t encSteps;
static volatile uint8_t encState;

static void IRAM_ATTR onEncoder()
{
	static const int8_t table[16] = {0, -1, 1, 0, 1, 0, 0, -1, -1, 0, 0, 1, 0, 1, -1, 0};
	encState = (uint8_t)(((encState << 2) | (digitalRead(ENC_A) << 1) | digitalRead(ENC_B)) & 0x0F);
	encSteps += table[encState];
}

static int takeDetents()
{
	noInterrupts();
	int32_t s = encSteps;
	int d = s / ENC_STEPS;
	encSteps = s - d * ENC_STEPS;
	interrupts();
	return d;
}

// ---- deck events --------------------------------------------------------------

static void onFrame(deck_link_t *, const deck_frame_t *f)
{
	deck_tiles_handle(&tiles, f);
}

static void onWelcome(deck_link_t *, const char *name, agentsdeck_link_v1_Scope, bool)
{
	snprintf(deviceName, sizeof deviceName, "%s", name);
	dirty = true;
}

static void onResult(deck_link_t *, uint32_t seq, uint32_t code, const char *error)
{
	if (seq != answerSeq)
		return;
	answerSeq = 0;
	if (code == 204)
		snprintf(status, sizeof status, "done");
	else if (code == 403)
		snprintf(status, sizeof status, "needs scope act");
	else if (code == 409)
		snprintf(status, sizeof status, "prompt changed");
	else
		snprintf(status, sizeof status, "%u %s", (unsigned)code, error);
	dirty = true;
}

static void onLinkState(deck_link_t *) { dirty = true; }

// ---- model --------------------------------------------------------------------

static const deck_tile_t *current()
{
	const deck_tile_t *t = agentId[0] ? deck_tiles_find(&tiles, agentId) : nullptr;
	if (!t && tiles.count > 0) {
		const deck_tile_t *w = deck_tiles_first_waiting(&tiles);
		t = w ? w : &tiles.tiles[0];
		snprintf(agentId, sizeof agentId, "%s", t->id);
	}
	return t;
}

static void turn(int d)
{
	const deck_tile_t *t = current();
	if (!t)
		return;
	if (mode == PICK_OPTION && t->option_count > 0) {
		option = ((option + d) % t->option_count + t->option_count) % t->option_count;
	} else {
		int i = deck_tiles_index(&tiles, t->id);
		i = ((i + d) % tiles.count + tiles.count) % tiles.count;
		snprintf(agentId, sizeof agentId, "%s", tiles.tiles[i].id);
	}
	status[0] = '\0';
	dirty = true;
}

static void press()
{
	const deck_tile_t *t = current();
	if (!t)
		return;
	if (mode == PICK_AGENT) {
		if (t->has_prompt && t->option_count > 0) {
			mode = PICK_OPTION;
			int p = deck_tile_primary(t);
			option = p < 0 ? 0 : p;
		} else {
			deck.focus(t->id);
			snprintf(status, sizeof status, "focused");
		}
	} else if (t->has_prompt && option < t->option_count) {
		answerSeq = deck.answer(t->id, t->prompt_id, t->options[option].key);
		snprintf(status, sizeof status, "sending...");
		mode = PICK_AGENT;
	}
	dirty = true;
}

static void longPress()
{
	mode = PICK_AGENT;
	status[0] = '\0';
	dirty = true;
}

static void button(uint32_t now)
{
	static bool down, longDone;
	static uint32_t at;
	bool pressed = digitalRead(ENC_BUTTON) == LOW;
	if (pressed && !down && now - at > 30) {
		down = true;
		longDone = false;
		at = now;
	} else if (pressed && down && !longDone && now - at >= LONG_PRESS_MS) {
		longDone = true;
		longPress();
	} else if (!pressed && down && now - at > 30) {
		down = false;
		if (!longDone)
			press();
		at = now;
	}
}

// ---- view ---------------------------------------------------------------------

static void render()
{
	char l[4][128] = {{0}};
	const deck_tile_t *t = current();
	if (deck.state() == DECK_LINK_OFFLINE) {
		snprintf(l[0], sizeof l[0], "deck offline");
	} else if (!deck.online()) {
		snprintf(l[0], sizeof l[0], "connecting...");
	} else if (!deck.approved()) {
		snprintf(l[0], sizeof l[0], "not approved yet:");
		snprintf(l[1], sizeof l[1], "agentctl devices approve %s", deviceName);
	} else if (!t) {
		snprintf(l[0], sizeof l[0], "no agents");
	} else {
		int i = deck_tiles_index(&tiles, t->id);
		snprintf(l[0], sizeof l[0], "[%d/%d] %s", i + 1, tiles.count, t->title);
		snprintf(l[1], sizeof l[1], "%s%s", deck_status_name(t->status), t->has_prompt ? ": " : "");
		if (t->has_prompt)
			strncat(l[1], t->prompt_title, sizeof l[1] - strlen(l[1]) - 1);
		if (mode == PICK_OPTION && option < t->option_count)
			snprintf(l[2], sizeof l[2], "> %s %s%s%s", t->options[option].key, t->options[option].selected ? "[x] " : "",
				 t->options[option].label,
				 t->options[option].primary ? " *" : "");
		else if (t->has_prompt && t->option_count > 0)
			snprintf(l[2], sizeof l[2], "press to answer");
		snprintf(l[3], sizeof l[3], "%s", status);
	}

	UI.printf("\n%s\n", l[0]);
	for (int k = 1; k < 4; k++)
		if (l[k][0])
			UI.printf("  %s\n", l[k]);

#if KNOB_OLED
	oled.clearDisplay();
	oled.setTextColor(SSD1306_WHITE);
	for (int k = 0; k < 4; k++) {
		oled.setCursor(0, k * 16);
		char b[22];
		snprintf(b, sizeof b, "%s", l[k]);
		for (char *p = b; *p; p++)
			if ((uint8_t)*p >= 0x80)
				*p = '?'; // the built-in font has no UTF-8
		oled.print(b);
	}
	oled.display();
#endif
}

void setup()
{
	Serial.begin(115200); // Deck Link (native USB: the rate is ignored)
	UI.begin(115200);
	pinMode(ENC_A, INPUT_PULLUP);
	pinMode(ENC_B, INPUT_PULLUP);
	pinMode(ENC_BUTTON, INPUT_PULLUP);
	attachInterrupt(digitalPinToInterrupt(ENC_A), onEncoder, CHANGE);
	attachInterrupt(digitalPinToInterrupt(ENC_B), onEncoder, CHANGE);
#if KNOB_OLED
	Wire.begin();
	oled.begin(SSD1306_SWITCHCAPVCC, 0x3C);
	oled.cp437(true);
#endif
	deck_tiles_init(&tiles);

	DeckLinkConfig cfg = {};
	cfg.board_id = DeckLink::chipId();
	cfg.type = "knob";
	cfg.firmware = "0.1.0";
#if KNOB_OLED
	cfg.display_width = 128;
	cfg.display_height = 64;
#endif
	cfg.inputs[0] = agentsdeck_link_v1_Input_INPUT_ENCODER;
	cfg.inputs[1] = agentsdeck_link_v1_Input_INPUT_BUTTON;
	cfg.input_count = 2;
	cfg.fields = agentsdeck_link_v1_FieldGroup_FIELDS_TILE | agentsdeck_link_v1_FieldGroup_FIELDS_PROMPT;
	cfg.on_frame = onFrame;
	cfg.on_welcome = onWelcome;
	cfg.on_result = onResult;
	cfg.on_online = onLinkState;
	cfg.on_offline = onLinkState;
	deck.begin(Serial, cfg);
}

void loop()
{
	static uint32_t seen;
	deck.loop();
	uint32_t now = millis();
	int d = takeDetents();
	if (d)
		turn(d);
	button(now);
	if (mode == PICK_OPTION) { // the prompt went away: back to the list
		const deck_tile_t *t = current();
		if (!t || !t->has_prompt)
			mode = PICK_AGENT;
	}
	if (tiles.version != seen) {
		seen = tiles.version;
		dirty = true;
	}
	if (dirty) {
		dirty = false;
		render();
	}
	delay(2);
}
