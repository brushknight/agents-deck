#include "ui.h"

#include <stdio.h>
#include <string.h>

#include "agents.h"
#include "critter.h"
#include "debug.h"
#include "esp_heap_caps.h"
#include "display.h"
#include "link.h"
#include "lvgl.h"

// The deck's palette (backend/internal/web/static/app.css).
#define C_BG 0x0A0A0A
#define C_PANEL 0x141412
#define C_INK 0xE8E4DC
#define C_DIM 0x7A776F
#define C_ACCENT 0xFF4D00
#define C_LINE 0x262622
#define C_EMPTY 0x0B0B0B

#define HEADER_H 56
#define GAP 8
#define COLS 2
#define ROWS 3
#define VISIBLE (COLS * ROWS) // tiles on screen at once
#define TILES AGENTS_MAX       // tiles in the list, at most
#define GRID_H (DISPLAY_H - HEADER_H)
#define TILE_W ((DISPLAY_W - GAP * (COLS + 1)) / COLS)
#define TILE_H ((DISPLAY_H - HEADER_H - GAP * (ROWS + 1)) / ROWS)

typedef struct {
	lv_obj_t *box, *stripes, *spr, *chip, *subs, *title, *meta, *bar;
	char id[12];
	critter_t crit;
	bool show_crit;
} tile_t;

#define C_FOCUSED 0x24241F
#define TILE_CELL 6.0f // sprite cell in pixels: 18 x 13 cells = 108 x 78
#define CARD_CELL 5.0f
#define CELEBRATE_MS 6000

static lv_obj_t *grid_screen, *card_screen, *note_screen;
static lv_obj_t *head_title, *head_counts, *head_dot, *toast;
static lv_obj_t *list, *list_bar; // the scrolling list of tiles; where it is, while scrolling
static lv_timer_t *bar_hide;
static tile_t tiles[TILES];
static lv_obj_t *note_title, *note_body;
static lv_obj_t *card_body, *card_spr;
static critter_t card_crit;
static char open_id[12]; // the agent whose card is open ("" for the grid)
static uint32_t seen_version = UINT32_MAX;
static agentsdeck_link_v1_Agent *card_copy; // what the open card shows
static int64_t clock_offset_ms;

// ---- text -----------------------------------------------------------------

// Copies UTF-8 text for the Montserrat fonts: "·" becomes "•", "…" "...",
// line breaks spaces when flat; other characters pass through (missing
// glyphs are skipped).
static void clean(char *dst, size_t size, const char *src, bool flat)
{
	size_t n = 0;
	for (const unsigned char *p = (const unsigned char *)src; *p && n + 4 < size;) {
		if (p[0] == 0xC2 && p[1] == 0xB7) { // ·
			memcpy(dst + n, "\xE2\x80\xA2", 3), n += 3, p += 2;
		} else if (p[0] == 0xE2 && p[1] == 0x80 && p[2] == 0xA6) { // …
			memcpy(dst + n, "...", 3), n += 3, p += 3;
		} else if (flat && (*p == '\n' || *p == '\t')) {
			dst[n++] = ' ', p++;
		} else {
			dst[n++] = (char)*p++;
		}
	}
	dst[n] = '\0';
}

static void set_text(lv_obj_t *l, const char *s, bool flat)
{
	char b[512];
	clean(b, sizeof b, s ? s : "", flat);
	lv_label_set_text(l, b);
}

static const char *verb(const char *tool)
{
	static const struct {
		const char *tool, *verb;
	} verbs[] = {{"compact", "compacting"}, {"todowrite", "plan"}, {"webfetch", "web"}, {"websearch", "search"},
		     {"multiedit", "edit"}, {"notebookedit", "edit"}, {"task", "agent"}};
	static char lower[32];
	if (!tool || !tool[0])
		return "thinking";
	size_t i = 0;
	for (; tool[i] && i < sizeof lower - 1; i++)
		lower[i] = (tool[i] >= 'A' && tool[i] <= 'Z') ? tool[i] + 32 : tool[i];
	lower[i] = '\0';
	for (size_t k = 0; k < sizeof verbs / sizeof verbs[0]; k++)
		if (strcmp(lower, verbs[k].tool) == 0)
			return verbs[k].verb;
	return lower;
}

// "12s", "4m", "3h", "2d" since a deck timestamp.
static void ago(char *b, size_t size, int64_t since_ms)
{
	int64_t s = since_ms ? ((int64_t)now_ms() + clock_offset_ms - since_ms) / 1000 : 0;
	if (s < 0)
		s = 0;
	if (s < 60)
		snprintf(b, size, "%llds", (long long)s);
	else if (s < 3600)
		snprintf(b, size, "%lldm", (long long)(s / 60));
	else if (s < 86400)
		snprintf(b, size, "%lldh", (long long)(s / 3600));
	else
		snprintf(b, size, "%lldd", (long long)(s / 86400));
}

static int ctx_pct(const agentsdeck_link_v1_Agent *a)
{
	if (!a->has_context || a->context.window <= 0)
		return -1;
	return (int)(a->context.used * 100 / a->context.window);
}

static bool hungry(const agentsdeck_link_v1_Agent *a)
{
	return a->unseen && a->status == agentsdeck_link_v1_Status_STATUS_IDLE;
}

// The tile's status line, as the web board words it.
static void meta(char *b, size_t size, const agentsdeck_link_v1_Agent *a)
{
	char t[16];
	int pct = ctx_pct(a);
	switch (a->status) {
	case agentsdeck_link_v1_Status_STATUS_RUNNING:
		if (pct >= 0)
			snprintf(b, size, "%s \xE2\x80\xA2 %d%% ctx", verb(a->has_activity ? a->activity.tool : ""), pct);
		else
			snprintf(b, size, "%s", verb(a->has_activity ? a->activity.tool : ""));
		return;
	case agentsdeck_link_v1_Status_STATUS_WAITING:
		if (a->has_waiting && a->waiting.kind == agentsdeck_link_v1_PromptKind_PROMPT_PERMISSION) {
			char what[sizeof a->waiting.context];
			snprintf(what, sizeof what, "%s", a->waiting.context);
			char *dot = strstr(what, "\xC2\xB7");
			if (dot)
				*dot = '\0';
			for (char *p = what; *p; p++)
				if (*p >= 'A' && *p <= 'Z')
					*p += 32;
			size_t n = strlen(what);
			while (n && what[n - 1] == ' ')
				what[--n] = '\0';
			snprintf(b, size, n ? "allow %s?" : "allow?", what);
		} else if (a->has_waiting && a->waiting.kind == agentsdeck_link_v1_PromptKind_PROMPT_QUESTION) {
			snprintf(b, size, "pick 1 of %d", (int)a->waiting.options_count);
		} else {
			snprintf(b, size, "needs input");
		}
		return;
	case agentsdeck_link_v1_Status_STATUS_IDLE:
		if (a->unseen) {
			snprintf(b, size, "hungry \xE2\x80\xA2 feed me");
			return;
		}
		ago(t, sizeof t, a->status_since_ms);
		snprintf(b, size, "idle \xE2\x80\xA2 %s", t);
		return;
	case agentsdeck_link_v1_Status_STATUS_ERROR: snprintf(b, size, "%s", a->error[0] ? a->error : "error"); return;
	case agentsdeck_link_v1_Status_STATUS_STARTING: snprintf(b, size, "starting..."); return;
	case agentsdeck_link_v1_Status_STATUS_EXITED: snprintf(b, size, "exited"); return;
	default: snprintf(b, size, " ");
	}
}

static const char *status_word(agentsdeck_link_v1_Status st)
{
	switch (st) {
	case agentsdeck_link_v1_Status_STATUS_STARTING: return "starting";
	case agentsdeck_link_v1_Status_STATUS_RUNNING: return "running";
	case agentsdeck_link_v1_Status_STATUS_WAITING: return "needs you";
	case agentsdeck_link_v1_Status_STATUS_IDLE: return "idle";
	case agentsdeck_link_v1_Status_STATUS_ERROR: return "error";
	case agentsdeck_link_v1_Status_STATUS_EXITED: return "exited";
	default: return "";
	}
}

// The colour of an account (the deck's hat colours) or of a tool.
static uint32_t named_color(const char *name)
{
	static const struct {
		const char *name;
		uint32_t rgb;
	} colors[] = {{"teal", 0x4FB3A6}, {"blue", 0x6F8FD8}, {"yellow", 0xE8B94A}, {"green", 0x6DBE6A},
		      {"purple", 0xA68BD8}, {"pink", 0xE58BB0}, {"sky", 0x5EC4D6},  {"ink", 0xE8E4DC}};
	for (size_t i = 0; i < sizeof colors / sizeof colors[0]; i++)
		if (strcmp(name, colors[i].name) == 0)
			return colors[i].rgb;
	return C_INK;
}

static uint32_t chip_color(const agentsdeck_link_v1_Agent *a)
{
	if (a->has_account && a->account.color[0])
		return named_color(a->account.color);
	switch (a->tool) {
	case agentsdeck_link_v1_Tool_TOOL_CLAUDE: return 0xC98B6B;
	case agentsdeck_link_v1_Tool_TOOL_CODEX: return 0x4FB3A6;
	case agentsdeck_link_v1_Tool_TOOL_GEMINI: return 0x6F8FD8;
	default: return C_DIM;
	}
}

static void count(char *b, size_t size, int64_t n)
{
	if (n >= 1000000000)
		snprintf(b, size, "%.1fb", n / 1e9);
	else if (n >= 1000000)
		snprintf(b, size, "%.1fm", n / 1e6);
	else if (n >= 1000)
		snprintf(b, size, "%lldk", (long long)(n / 1000));
	else
		snprintf(b, size, "%lld", (long long)n);
}

// ---- critters -------------------------------------------------------------

static pose_t pose_of(const agentsdeck_link_v1_Agent *a)
{
	static const struct {
		const char *tool;
		pose_t pose;
	} poses[] = {{"read", POSE_READ},      {"grep", POSE_READ},        {"glob", POSE_READ},      {"ls", POSE_READ},
		     {"edit", POSE_WRITE},     {"write", POSE_WRITE},      {"multiedit", POSE_WRITE}, {"notebookedit", POSE_WRITE},
		     {"bash", POSE_BASH},      {"bashoutput", POSE_BASH},  {"killshell", POSE_BASH}, {"killbash", POSE_BASH},
		     {"webfetch", POSE_WEB},   {"websearch", POSE_WEB},    {"todowrite", POSE_PLAN}, {"task", POSE_DELEGATE},
		     {"agent", POSE_DELEGATE}, {"compact", POSE_COMPACT}};
	switch (a->status) {
	case agentsdeck_link_v1_Status_STATUS_RUNNING: {
		if (!a->has_activity || !a->activity.tool[0])
			return POSE_THINK;
		char t[32];
		size_t i = 0;
		for (; a->activity.tool[i] && i < sizeof t - 1; i++)
			t[i] = (a->activity.tool[i] >= 'A' && a->activity.tool[i] <= 'Z') ? a->activity.tool[i] + 32 : a->activity.tool[i];
		t[i] = '\0';
		for (size_t k = 0; k < sizeof poses / sizeof poses[0]; k++)
			if (strcmp(t, poses[k].tool) == 0)
				return poses[k].pose;
		return POSE_RUN;
	}
	case agentsdeck_link_v1_Status_STATUS_IDLE:
		if (a->status_since_ms && (int64_t)now_ms() + clock_offset_ms - a->status_since_ms < CELEBRATE_MS)
			return POSE_CELEBRATE;
		return a->unseen ? POSE_HUNGRY : POSE_IDLE;
	case agentsdeck_link_v1_Status_STATUS_WAITING: return POSE_WAIT;
	case agentsdeck_link_v1_Status_STATUS_ERROR: return POSE_ERR;
	case agentsdeck_link_v1_Status_STATUS_STARTING: return POSE_START;
	case agentsdeck_link_v1_Status_STATUS_EXITED: return POSE_EXIT;
	default: return POSE_IDLE;
	}
}

// The tile's background, as the web board colours it.
static uint32_t tile_bg(const agentsdeck_link_v1_Agent *a)
{
	if (a->status == agentsdeck_link_v1_Status_STATUS_WAITING)
		return a->focused ? 0xFF6A26 : C_ACCENT;
	return a->focused ? C_FOCUSED : C_PANEL;
}

// The critter for an agent, on background bg: orange while working, ink on
// an error, dim when idle, black on a waiting (orange) tile.
static void critter_of(critter_t *c, const agentsdeck_link_v1_Agent *a, uint32_t bg)
{
	memset(c, 0, sizeof *c);
	c->pose = pose_of(a);
	c->onigiri = a->tool == agentsdeck_link_v1_Tool_TOOL_CODEX;
	c->bubble = true;
	c->cut = bg;
	switch (a->status) {
	case agentsdeck_link_v1_Status_STATUS_WAITING: c->body = C_BG; break;
	case agentsdeck_link_v1_Status_STATUS_RUNNING: c->body = C_ACCENT; break;
	case agentsdeck_link_v1_Status_STATUS_ERROR: c->body = C_INK; break;
	case agentsdeck_link_v1_Status_STATUS_IDLE: c->body = a->unseen ? C_ACCENT : C_DIM; break;
	default: c->body = C_DIM;
	}
	if (a->has_hat && a->hat.shape[0]) {
		uint32_t shade;
		c->hat = a->hat.shape;
		critter_hat_color(a->hat.color, &c->hat_color, &shade);
	}
}

static void draw_critter_event(lv_event_t *e)
{
	lv_obj_t *o = lv_event_get_target(e);
	const critter_t *c = lv_event_get_user_data(e);
	if (!c || !c->body)
		return;
	lv_area_t a;
	lv_obj_get_coords(o, &a);
	float cell = (float)lv_obj_get_width(o) / 18.0f;
	critter_draw(lv_event_get_layer(e), c, a.x1, a.y1, cell, now_ms());
}

// Sliding diagonal stripes on a hungry tile.
static void draw_stripes_event(lv_event_t *e)
{
	lv_obj_t *o = lv_event_get_target(e);
	lv_area_t a;
	lv_obj_get_coords(o, &a);
	lv_draw_line_dsc_t d;
	lv_draw_line_dsc_init(&d);
	d.color = lv_color_hex(C_ACCENT);
	d.opa = LV_OPA_20;
	d.width = 8;
	int32_t h = lv_area_get_height(&a), w = lv_area_get_width(&a);
	int32_t shift = (int32_t)((now_ms() / 60) % 24);
	for (int32_t x = -h + shift; x < w; x += 24) { // drawing is clipped to the tile
		d.p1.x = a.x1 + x, d.p1.y = a.y2;
		d.p2.x = a.x1 + x + h, d.p2.y = a.y1;
		lv_draw_line(lv_event_get_layer(e), &d);
	}
}

// Moves the animations on: the sprites (and stripes) redraw ten times a second.
static void animate(lv_timer_t *t)
{
	(void)t;
	if (lv_screen_active() == grid_screen) {
		for (int i = 0; i < TILES; i++) {
			if (tiles[i].show_crit)
				lv_obj_invalidate(tiles[i].spr);
			if (!lv_obj_has_flag(tiles[i].stripes, LV_OBJ_FLAG_HIDDEN))
				lv_obj_invalidate(tiles[i].stripes);
		}
	} else if (lv_screen_active() == card_screen && card_spr) {
		lv_obj_invalidate(card_spr);
	}
}

// ---- widgets --------------------------------------------------------------

static lv_obj_t *box(lv_obj_t *parent)
{
	lv_obj_t *o = lv_obj_create(parent);
	lv_obj_remove_style_all(o);
	lv_obj_remove_flag(o, LV_OBJ_FLAG_SCROLLABLE);
	lv_obj_remove_flag(o, LV_OBJ_FLAG_CLICKABLE); // taps go to the parent unless asked
	lv_obj_add_flag(o, LV_OBJ_FLAG_GESTURE_BUBBLE);
	return o;
}

static lv_obj_t *label(lv_obj_t *parent, const lv_font_t *font, uint32_t color)
{
	lv_obj_t *l = lv_label_create(parent);
	lv_obj_set_style_text_font(l, font, 0);
	lv_obj_set_style_text_color(l, lv_color_hex(color), 0);
	lv_label_set_text(l, "");
	return l;
}

static lv_obj_t *screen(void)
{
	lv_obj_t *s = lv_obj_create(NULL);
	lv_obj_set_style_bg_color(s, lv_color_hex(C_BG), 0);
	lv_obj_set_style_bg_opa(s, LV_OPA_COVER, 0);
	lv_obj_set_style_pad_all(s, 0, 0);
	return s;
}

static void show(lv_obj_t *s)
{
	if (lv_screen_active() != s)
		lv_screen_load(s);
}

// ---- grid -----------------------------------------------------------------

static void open_card(const char *id);

static void tile_clicked(lv_event_t *e)
{
	tile_t *t = lv_event_get_user_data(e);
	if (lv_indev_get_gesture_dir(lv_indev_active()) != LV_DIR_NONE)
		return; // the end of a swipe, not a tap
	if (t->id[0])
		open_card(t->id);
}

// The bar on the left: how far down the list is, shown only while it scrolls.
static void place_bar(void)
{
	int32_t content = lv_obj_get_scroll_top(list) + lv_obj_get_scroll_bottom(list) + GRID_H;
	int32_t h = content > 0 ? GRID_H * GRID_H / content : GRID_H;
	if (h < 40)
		h = 40;
	int32_t range = lv_obj_get_scroll_top(list) + lv_obj_get_scroll_bottom(list);
	int32_t y = range > 0 ? (GRID_H - h) * lv_obj_get_scroll_top(list) / range : 0;
	if (y < 0)
		y = 0;
	if (y > GRID_H - h)
		y = GRID_H - h;
	lv_obj_set_size(list_bar, 4, h);
	lv_obj_set_pos(list_bar, 2, HEADER_H + y);
}

static void hide_bar(lv_timer_t *t)
{
	(void)t;
	lv_obj_add_flag(list_bar, LV_OBJ_FLAG_HIDDEN);
}

static void list_scrolled(lv_event_t *e)
{
	lv_event_code_t c = lv_event_get_code(e);
	if (lv_obj_get_scroll_top(list) + lv_obj_get_scroll_bottom(list) <= 0)
		return; // everything fits: nothing to show
	if (c == LV_EVENT_SCROLL_BEGIN || c == LV_EVENT_SCROLL) {
		lv_timer_pause(bar_hide);
		place_bar();
		lv_obj_remove_flag(list_bar, LV_OBJ_FLAG_HIDDEN);
	} else if (c == LV_EVENT_SCROLL_END) { // settled on a row: fade out shortly
		lv_timer_reset(bar_hide);
		lv_timer_resume(bar_hide);
	}
}

static void build_grid(void)
{
	grid_screen = screen();

	head_title = label(grid_screen, &lv_font_montserrat_28, C_INK);
	lv_label_set_text(head_title, "agents");
	lv_obj_align(head_title, LV_ALIGN_TOP_LEFT, 14, 12);
	head_counts = label(grid_screen, &lv_font_montserrat_20, C_DIM);
	lv_obj_align(head_counts, LV_ALIGN_TOP_LEFT, 128, 18);
	head_dot = box(grid_screen); // the link: ink online, accent when it needs you, dim offline
	lv_obj_set_size(head_dot, 12, 12);
	lv_obj_set_style_bg_opa(head_dot, LV_OPA_COVER, 0);
	lv_obj_align(head_dot, LV_ALIGN_TOP_RIGHT, -16, 24);

	// The list: every agent, two per row, scrolling up and down. Let go and
	// it settles on the nearest row, so the tiles on screen are whole.
	list = box(grid_screen);
	lv_obj_set_size(list, DISPLAY_W, GRID_H);
	lv_obj_set_pos(list, 0, HEADER_H);
	lv_obj_add_flag(list, LV_OBJ_FLAG_SCROLLABLE | LV_OBJ_FLAG_CLICKABLE | LV_OBJ_FLAG_SCROLL_MOMENTUM |
				      LV_OBJ_FLAG_SCROLL_ELASTIC);
	lv_obj_remove_flag(list, LV_OBJ_FLAG_SCROLL_CHAIN);
	lv_obj_set_scroll_dir(list, LV_DIR_VER);
	lv_obj_set_scroll_snap_y(list, LV_SCROLL_SNAP_START);
	lv_obj_set_scrollbar_mode(list, LV_SCROLLBAR_MODE_OFF); // ours is on the left
	lv_obj_set_style_pad_top(list, GAP, 0);
	lv_obj_set_style_pad_bottom(list, GAP, 0);
	lv_obj_add_event_cb(list, list_scrolled, LV_EVENT_SCROLL_BEGIN, NULL);
	lv_obj_add_event_cb(list, list_scrolled, LV_EVENT_SCROLL, NULL);
	lv_obj_add_event_cb(list, list_scrolled, LV_EVENT_SCROLL_END, NULL);

	for (int i = 0; i < TILES; i++) {
		tile_t *t = &tiles[i];
		t->box = box(list);
		lv_obj_set_size(t->box, TILE_W, TILE_H);
		lv_obj_set_pos(t->box, GAP + (i % COLS) * (TILE_W + GAP), (i / COLS) * (TILE_H + GAP));
		lv_obj_set_style_radius(t->box, 4, 0);
		lv_obj_set_style_bg_opa(t->box, LV_OPA_COVER, 0);
		lv_obj_set_style_border_width(t->box, 2, 0);
		lv_obj_add_flag(t->box, LV_OBJ_FLAG_CLICKABLE);
		lv_obj_add_flag(t->box, LV_OBJ_FLAG_GESTURE_BUBBLE);
		lv_obj_add_event_cb(t->box, tile_clicked, LV_EVENT_CLICKED, t);

		t->stripes = box(t->box);
		lv_obj_set_size(t->stripes, lv_pct(100), lv_pct(100));
		lv_obj_add_event_cb(t->stripes, draw_stripes_event, LV_EVENT_DRAW_MAIN, NULL);
		lv_obj_add_flag(t->stripes, LV_OBJ_FLAG_HIDDEN);
		t->spr = box(t->box);
		lv_obj_set_size(t->spr, (int32_t)(18 * TILE_CELL), (int32_t)(13 * TILE_CELL));
		// centre the body (cells 0..14), not body and bubble
		lv_obj_align(t->spr, LV_ALIGN_TOP_MID, (int32_t)(2 * TILE_CELL), 10);
		lv_obj_add_event_cb(t->spr, draw_critter_event, LV_EVENT_DRAW_MAIN, &t->crit);
		lv_obj_add_flag(t->spr, LV_OBJ_FLAG_OVERFLOW_VISIBLE);

		t->chip = box(t->box);
		lv_obj_set_size(t->chip, 12, 12);
		lv_obj_set_style_bg_opa(t->chip, LV_OPA_COVER, 0);
		lv_obj_align(t->chip, LV_ALIGN_TOP_RIGHT, -12, 12);
		t->subs = label(t->box, &lv_font_montserrat_16, C_DIM);
		lv_obj_align(t->subs, LV_ALIGN_TOP_LEFT, 12, 8);

		t->title = label(t->box, &lv_font_montserrat_24, C_INK);
		lv_obj_set_width(t->title, TILE_W - 24);
		lv_label_set_long_mode(t->title, LV_LABEL_LONG_DOT);
		lv_obj_set_style_text_align(t->title, LV_TEXT_ALIGN_CENTER, 0);
		lv_obj_set_height(t->title, lv_font_get_line_height(&lv_font_montserrat_24));
		lv_obj_align(t->title, LV_ALIGN_TOP_MID, 0, 96);
		t->meta = label(t->box, &lv_font_montserrat_20, C_DIM);
		lv_obj_set_width(t->meta, TILE_W - 20);
		lv_label_set_long_mode(t->meta, LV_LABEL_LONG_DOT);
		lv_obj_set_style_text_align(t->meta, LV_TEXT_ALIGN_CENTER, 0);
		lv_obj_set_height(t->meta, lv_font_get_line_height(&lv_font_montserrat_20));
		lv_obj_align(t->meta, LV_ALIGN_TOP_MID, 0, 126);

		t->bar = lv_bar_create(t->box); // context used
		lv_obj_set_size(t->bar, TILE_W - 24, 6);
		lv_obj_align(t->bar, LV_ALIGN_BOTTOM_MID, 0, -8);
		lv_obj_set_style_radius(t->bar, 0, 0);
		lv_obj_set_style_radius(t->bar, 0, LV_PART_INDICATOR);
		lv_obj_remove_flag(t->bar, LV_OBJ_FLAG_CLICKABLE);
	}

	list_bar = box(grid_screen);
	lv_obj_set_style_bg_color(list_bar, lv_color_hex(C_DIM), 0);
	lv_obj_set_style_bg_opa(list_bar, LV_OPA_COVER, 0);
	lv_obj_set_style_radius(list_bar, 2, 0);
	lv_obj_add_flag(list_bar, LV_OBJ_FLAG_HIDDEN);
	bar_hide = lv_timer_create(hide_bar, 700, NULL);
	lv_timer_pause(bar_hide);

	toast = label(grid_screen, &lv_font_montserrat_20, C_BG);
	lv_obj_set_style_bg_color(toast, lv_color_hex(C_INK), 0);
	lv_obj_set_style_bg_opa(toast, LV_OPA_COVER, 0);
	lv_obj_set_style_pad_all(toast, 10, 0);
	lv_obj_set_width(toast, DISPLAY_W - 32);
	lv_label_set_long_mode(toast, LV_LABEL_LONG_WRAP);
	lv_obj_set_style_text_align(toast, LV_TEXT_ALIGN_CENTER, 0);
	lv_obj_align(toast, LV_ALIGN_TOP_MID, 0, 10); // over the header, never over a button
	lv_obj_add_flag(toast, LV_OBJ_FLAG_HIDDEN);
}

static void draw_tile(tile_t *t, const agentsdeck_link_v1_Agent *a)
{
	if (!a) {
		t->id[0] = '\0';
		lv_obj_set_style_bg_color(t->box, lv_color_hex(C_EMPTY), 0);
		lv_obj_set_style_border_color(t->box, lv_color_hex(C_PANEL), 0);
		lv_obj_add_flag(t->chip, LV_OBJ_FLAG_HIDDEN);
		lv_obj_add_flag(t->bar, LV_OBJ_FLAG_HIDDEN);
		lv_obj_add_flag(t->stripes, LV_OBJ_FLAG_HIDDEN);
		t->show_crit = false;
		t->crit.body = 0;
		lv_obj_invalidate(t->spr);
		lv_label_set_text(t->title, "");
		lv_label_set_text(t->meta, "");
		lv_label_set_text(t->subs, "");
		return;
	}
	snprintf(t->id, sizeof t->id, "%s", a->id);
	bool waiting = a->status == agentsdeck_link_v1_Status_STATUS_WAITING;
	bool quiet = a->status == agentsdeck_link_v1_Status_STATUS_IDLE && !a->unseen;
	quiet = quiet || a->status == agentsdeck_link_v1_Status_STATUS_EXITED ||
		a->status == agentsdeck_link_v1_Status_STATUS_STARTING;
	uint32_t bg = tile_bg(a);
	uint32_t ink = waiting ? C_BG : quiet ? C_DIM : C_INK;
	uint32_t border = waiting || hungry(a) ? C_ACCENT : bg;

	lv_obj_set_style_bg_color(t->box, lv_color_hex(bg), 0);
	lv_obj_set_style_border_color(t->box, lv_color_hex(border), 0);
	critter_of(&t->crit, a, bg);
	t->show_crit = true;
	lv_obj_invalidate(t->spr);
	if (hungry(a))
		lv_obj_remove_flag(t->stripes, LV_OBJ_FLAG_HIDDEN);
	else
		lv_obj_add_flag(t->stripes, LV_OBJ_FLAG_HIDDEN);
	lv_obj_remove_flag(t->chip, LV_OBJ_FLAG_HIDDEN);
	lv_obj_set_style_bg_color(t->chip, lv_color_hex(waiting ? C_BG : chip_color(a)), 0);
	set_text(t->title, a->title, true);
	lv_obj_set_style_text_color(t->title, lv_color_hex(ink), 0);

	char m[160];
	meta(m, sizeof m, a);
	set_text(t->meta, m, true);
	uint32_t mc = waiting ? C_BG : (hungry(a) || a->status == agentsdeck_link_v1_Status_STATUS_ERROR) ? C_ACCENT : C_DIM;
	lv_obj_set_style_text_color(t->meta, lv_color_hex(mc), 0);

	if (a->subagent_count > 0) {
		char s[16];
		snprintf(s, sizeof s, "+%u", (unsigned)a->subagent_count);
		lv_label_set_text(t->subs, s);
	} else {
		lv_label_set_text(t->subs, "");
	}
	lv_obj_set_style_text_color(t->subs, lv_color_hex(waiting ? C_BG : C_DIM), 0);

	int pct = ctx_pct(a);
	if (pct < 0) {
		lv_obj_add_flag(t->bar, LV_OBJ_FLAG_HIDDEN);
	} else {
		lv_obj_remove_flag(t->bar, LV_OBJ_FLAG_HIDDEN);
		lv_bar_set_value(t->bar, pct > 100 ? 100 : pct, LV_ANIM_OFF);
		lv_obj_set_style_bg_color(t->bar, lv_color_hex(waiting ? 0xB23600 : C_LINE), 0);
		lv_obj_set_style_bg_opa(t->bar, LV_OPA_COVER, 0);
		lv_obj_set_style_bg_color(t->bar, lv_color_hex(ink), LV_PART_INDICATOR);
	}
}

static void draw_grid(const agents_t *s)
{
	// Whole rows, at least a screenful: empty slots fill the rest.
	int shown = s->count < VISIBLE ? VISIBLE : (s->count + COLS - 1) / COLS * COLS;
	int last_top = shown / COLS - ROWS; // the last row that can be at the top of a full screen
	for (int i = 0; i < TILES; i++) {
		if (i / COLS <= last_top) // snap only where a whole screen of rows follows
			lv_obj_add_flag(tiles[i].box, LV_OBJ_FLAG_SNAPPABLE);
		else
			lv_obj_remove_flag(tiles[i].box, LV_OBJ_FLAG_SNAPPABLE);
		if (i < shown) {
			lv_obj_remove_flag(tiles[i].box, LV_OBJ_FLAG_HIDDEN);
			draw_tile(&tiles[i], i < s->count ? s->agents[i] : NULL);
		} else {
			lv_obj_add_flag(tiles[i].box, LV_OBJ_FLAG_HIDDEN);
		}
	}
	int waiting = agents_count_status(s, agentsdeck_link_v1_Status_STATUS_WAITING);
	int running = agents_count_status(s, agentsdeck_link_v1_Status_STATUS_RUNNING);
	char b[64];
	if (waiting)
		snprintf(b, sizeof b, "%d waiting \xE2\x80\xA2 %d running", waiting, running);
	else
		snprintf(b, sizeof b, "%d running \xE2\x80\xA2 %d in all", running, s->count);
	lv_label_set_text(head_counts, b);
	lv_obj_set_style_text_color(head_counts, lv_color_hex(waiting ? C_ACCENT : C_DIM), 0);
	lv_obj_set_style_bg_color(head_dot, lv_color_hex(waiting ? C_ACCENT : C_INK), 0);
}

// ---- card -----------------------------------------------------------------

static void close_card(void)
{
	open_id[0] = '\0';
	seen_version = UINT32_MAX;
	show(grid_screen);
}

static void back_clicked(lv_event_t *e)
{
	(void)e;
	close_card();
}

static void card_gesture(lv_event_t *e)
{
	(void)e;
	if (lv_indev_get_gesture_dir(lv_indev_active()) == LV_DIR_RIGHT)
		close_card();
}

typedef struct {
	action_kind_t kind;
	char key[8];
} button_action_t;

static button_action_t button_actions[12];
static int button_action_n;

static void action_clicked(lv_event_t *e)
{
	button_action_t *b = lv_event_get_user_data(e);
	action_t a = {.kind = b->kind};
	snprintf(a.agent_id, sizeof a.agent_id, "%s", card_copy->id);
	snprintf(a.prompt_id, sizeof a.prompt_id, "%s", card_copy->waiting.id);
	snprintf(a.key, sizeof a.key, "%s", b->key);
	agents_t *s = agents_lock();
	agents_say(s, link_send(&a) ? "sent" : "busy, try again", 2000);
	agents_unlock();
}

static lv_obj_t *button(lv_obj_t *parent, const char *text, bool solid, action_kind_t kind, const char *key)
{
	if (button_action_n >= (int)(sizeof button_actions / sizeof button_actions[0]))
		return NULL;
	button_action_t *ba = &button_actions[button_action_n++];
	ba->kind = kind;
	snprintf(ba->key, sizeof ba->key, "%s", key ? key : "");

	lv_obj_t *b = box(parent);
	lv_obj_set_size(b, lv_pct(100), LV_SIZE_CONTENT);
	lv_obj_set_style_min_height(b, 60, 0);
	lv_obj_set_style_pad_all(b, 14, 0);
	lv_obj_set_style_radius(b, 14, 0);
	lv_obj_set_style_border_width(b, 2, 0);
	lv_obj_set_style_border_color(b, lv_color_hex(C_INK), 0);
	lv_obj_set_style_bg_color(b, lv_color_hex(solid ? C_INK : C_BG), 0);
	lv_obj_set_style_bg_opa(b, LV_OPA_COVER, 0);
	lv_obj_set_style_bg_color(b, lv_color_hex(C_ACCENT), LV_STATE_PRESSED);
	lv_obj_add_flag(b, LV_OBJ_FLAG_CLICKABLE);
	lv_obj_add_event_cb(b, action_clicked, LV_EVENT_CLICKED, ba);
	lv_obj_t *l = label(b, &lv_font_montserrat_24, solid ? C_BG : C_INK);
	lv_obj_set_width(l, lv_pct(100));
	lv_label_set_long_mode(l, LV_LABEL_LONG_WRAP);
	lv_obj_set_style_text_align(l, LV_TEXT_ALIGN_CENTER, 0);
	lv_obj_center(l);
	set_text(l, text, true);
	return b;
}

static lv_obj_t *section(lv_obj_t *parent, const char *heading)
{
	lv_obj_t *h = label(parent, &lv_font_montserrat_16, C_DIM);
	lv_label_set_text(h, heading);
	lv_obj_set_style_pad_top(h, 10, 0);
	return h;
}

static lv_obj_t *para(lv_obj_t *parent, const lv_font_t *font, uint32_t color, const char *text)
{
	lv_obj_t *l = label(parent, font, color);
	lv_obj_set_width(l, lv_pct(100));
	lv_label_set_long_mode(l, LV_LABEL_LONG_WRAP);
	set_text(l, text, false);
	return l;
}

// A paragraph of at most `lines` lines ("..." past them): its height stays
// put while the text changes, so the card doesn't jump under a scroll.
static lv_obj_t *lines_para(lv_obj_t *parent, const lv_font_t *font, uint32_t color, const char *text, int lines)
{
	lv_obj_t *l = para(parent, font, color, text);
	lv_label_set_long_mode(l, LV_LABEL_LONG_DOT);
	lv_obj_set_height(l, lv_font_get_line_height(font) * lines);
	return l;
}

// One of the four numbers: a small heading over a big value.
static void number(lv_obj_t *row, const char *heading, const char *value)
{
	lv_obj_t *c = box(row);
	lv_obj_set_size(c, lv_pct(48), LV_SIZE_CONTENT);
	lv_obj_set_flex_flow(c, LV_FLEX_FLOW_COLUMN);
	lv_obj_t *h = label(c, &lv_font_montserrat_16, C_DIM);
	lv_label_set_text(h, heading);
	lv_obj_t *v = label(c, &lv_font_montserrat_28, C_INK);
	lv_label_set_text(v, value);
}

#ifdef PANEL_DEBUG
static void card_debug_event(lv_event_t *e)
{
	lv_event_code_t c = lv_event_get_code(e);
	if (c == LV_EVENT_PRESSED || c == LV_EVENT_SCROLL_BEGIN || c == LV_EVENT_SCROLL_END || c == LV_EVENT_RELEASED ||
	    c == LV_EVENT_GESTURE || c == LV_EVENT_DELETE)
		debug_note("ev%d t=%p cur=%p y=%d", (int)c, (void *)lv_event_get_target(e),
			   (void *)lv_event_get_current_target(e), (int)lv_obj_get_scroll_y(lv_event_get_current_target(e)));
}
#endif

static void draw_card(const agentsdeck_link_v1_Agent *a)
{
#ifdef PANEL_DEBUG
	debug_note("draw_card scr=%p", (void *)card_screen);
#endif
	int32_t scroll = card_body ? lv_obj_get_scroll_y(card_body) : 0;
	if (card_screen == NULL) {
		card_screen = screen();
		lv_obj_add_event_cb(card_screen, card_gesture, LV_EVENT_GESTURE, NULL);
	}
	lv_obj_clean(card_screen);
	button_action_n = 0;

	// The top bar: back, and the status pill.
	lv_obj_t *back = label(card_screen, &lv_font_montserrat_28, C_INK);
	lv_label_set_text(back, LV_SYMBOL_LEFT " back");
	lv_obj_set_style_pad_all(back, 12, 0);
	lv_obj_add_flag(back, LV_OBJ_FLAG_CLICKABLE);
	lv_obj_add_event_cb(back, back_clicked, LV_EVENT_CLICKED, NULL);
	lv_obj_align(back, LV_ALIGN_TOP_LEFT, 2, 2);

	char b[160], t[16];
	bool waiting = a->status == agentsdeck_link_v1_Status_STATUS_WAITING;
	ago(t, sizeof t, a->status_since_ms);
	snprintf(b, sizeof b, "%s \xE2\x80\xA2 %s", status_word(a->status), t);
	lv_obj_t *pill = label(card_screen, &lv_font_montserrat_20, waiting ? C_BG : C_BG);
	lv_label_set_text(pill, b);
	lv_obj_set_style_bg_color(pill, lv_color_hex(waiting || hungry(a) ? C_ACCENT : C_INK), 0);
	lv_obj_set_style_bg_opa(pill, LV_OPA_COVER, 0);
	lv_obj_set_style_radius(pill, 10, 0);
	lv_obj_set_style_pad_hor(pill, 12, 0);
	lv_obj_set_style_pad_ver(pill, 6, 0);
	lv_obj_align(pill, LV_ALIGN_TOP_RIGHT, -14, 16);

	// The body scrolls.
	card_body = box(card_screen);
	lv_obj_add_flag(card_body, LV_OBJ_FLAG_SCROLLABLE);
	// Presses on its text land here (LVGL gives a press to the nearest
	// clickable object), so a swipe anywhere on the card scrolls it.
	lv_obj_add_flag(card_body, LV_OBJ_FLAG_CLICKABLE);
	lv_obj_add_flag(card_body, LV_OBJ_FLAG_GESTURE_BUBBLE);
#ifdef PANEL_DEBUG
	lv_obj_add_event_cb(card_body, card_debug_event, LV_EVENT_ALL, NULL);
#endif
	lv_obj_set_scroll_dir(card_body, LV_DIR_VER);
	lv_obj_set_size(card_body, DISPLAY_W, DISPLAY_H - 64);
	lv_obj_set_pos(card_body, 0, 64);
	lv_obj_set_style_pad_hor(card_body, 18, 0);
	lv_obj_set_style_pad_bottom(card_body, 24, 0);
	lv_obj_set_style_pad_row(card_body, 6, 0);
	lv_obj_set_flex_flow(card_body, LV_FLEX_FLOW_COLUMN);

	lv_obj_t *head = box(card_body); // the critter, then the title
	lv_obj_set_size(head, lv_pct(100), LV_SIZE_CONTENT);
	lv_obj_set_flex_flow(head, LV_FLEX_FLOW_ROW);
	lv_obj_set_flex_align(head, LV_FLEX_ALIGN_START, LV_FLEX_ALIGN_CENTER, LV_FLEX_ALIGN_CENTER);
	lv_obj_set_style_pad_column(head, 16, 0);
	lv_obj_set_style_pad_top(head, 6, 0);
	critter_of(&card_crit, a, C_BG);
	if (waiting) // black on the waiting tile; orange on the card's dark background
		card_crit.body = C_ACCENT;
	card_spr = box(head);
	lv_obj_set_size(card_spr, (int32_t)(18 * CARD_CELL), (int32_t)(13 * CARD_CELL));
	lv_obj_add_flag(card_spr, LV_OBJ_FLAG_OVERFLOW_VISIBLE);
	lv_obj_add_event_cb(card_spr, draw_critter_event, LV_EVENT_DRAW_MAIN, &card_crit);
	lv_obj_t *title = label(head, &lv_font_montserrat_36, C_INK);
	lv_obj_set_flex_grow(title, 1);
	lv_label_set_long_mode(title, LV_LABEL_LONG_WRAP);
	set_text(title, a->title, false);
	if (a->ai_title[0] && strcmp(a->ai_title, a->title) != 0)
		lines_para(card_body, &lv_font_montserrat_20, C_DIM, a->ai_title, 1);

	// What it asks, with the answers.
	if (waiting && a->has_waiting) {
		lv_obj_t *p = box(card_body);
		lv_obj_set_size(p, lv_pct(100), LV_SIZE_CONTENT);
		lv_obj_set_flex_flow(p, LV_FLEX_FLOW_COLUMN);
		lv_obj_set_style_pad_all(p, 14, 0);
		lv_obj_set_style_pad_row(p, 10, 0);
		lv_obj_set_style_border_width(p, 2, 0);
		lv_obj_set_style_border_color(p, lv_color_hex(C_ACCENT), 0);
		lv_obj_set_style_radius(p, 8, 0);
		lv_obj_set_style_margin_top(p, 8, 0);
		para(p, &lv_font_montserrat_24, C_ACCENT, a->waiting.title[0] ? a->waiting.title : "needs you");
		if (a->waiting.detail[0])
			para(p, &lv_font_montserrat_20, C_INK, a->waiting.detail);
		if (a->waiting.context[0])
			para(p, &lv_font_montserrat_16, C_DIM, a->waiting.context);
		for (pb_size_t i = 0; i < a->waiting.options_count; i++) {
			const agentsdeck_link_v1_Option *o = &a->waiting.options[i];
			snprintf(b, sizeof b, "%s. %s", o->key, o->label);
			button(p, b, o->primary, ACTION_ANSWER, o->key);
		}
		if (a->waiting.options_count == 0)
			para(p, &lv_font_montserrat_20, C_DIM, "answer it in the terminal");
	}

	// Always there, one line: the tool while it works (not its argument,
	// which changes every moment), else its state; the card keeps its shape.
	section(card_body, "doing now");
	lines_para(card_body, &lv_font_montserrat_24, a->status == agentsdeck_link_v1_Status_STATUS_RUNNING ? C_INK : C_DIM,
		   a->status == agentsdeck_link_v1_Status_STATUS_RUNNING ? verb(a->has_activity ? a->activity.tool : "")
									  : status_word(a->status),
		   1);
	if (a->status == agentsdeck_link_v1_Status_STATUS_ERROR && a->error[0]) {
		section(card_body, "error");
		para(card_body, &lv_font_montserrat_20, C_ACCENT, a->error);
	}

	int pct = ctx_pct(a);
	if (pct >= 0) {
		char used[16], win[16];
		count(used, sizeof used, a->context.used);
		count(win, sizeof win, a->context.window);
		snprintf(b, sizeof b, "context  %d%%  \xE2\x80\xA2  %s / %s", pct, used, win);
		section(card_body, b);
		lv_obj_t *bar = lv_bar_create(card_body);
		lv_obj_set_size(bar, lv_pct(100), 10);
		lv_obj_set_style_radius(bar, 0, 0);
		lv_obj_set_style_radius(bar, 0, LV_PART_INDICATOR);
		lv_obj_set_style_bg_color(bar, lv_color_hex(C_LINE), 0);
		lv_obj_set_style_bg_opa(bar, LV_OPA_COVER, 0);
		lv_obj_set_style_bg_color(bar, lv_color_hex(C_INK), LV_PART_INDICATOR);
		lv_bar_set_value(bar, pct > 100 ? 100 : pct, LV_ANIM_OFF);
	}

	if (a->has_tokens || a->cost_usd > 0 || a->turns > 0) {
		lv_obj_t *row = box(card_body);
		lv_obj_set_size(row, lv_pct(100), LV_SIZE_CONTENT);
		lv_obj_set_flex_flow(row, LV_FLEX_FLOW_ROW_WRAP);
		lv_obj_set_style_pad_row(row, 10, 0);
		lv_obj_set_style_pad_top(row, 12, 0);
		lv_obj_set_flex_align(row, LV_FLEX_ALIGN_SPACE_BETWEEN, LV_FLEX_ALIGN_START, LV_FLEX_ALIGN_START);
		char v[24];
		count(v, sizeof v, a->has_tokens ? a->tokens.output : 0);
		number(row, "tokens out", v);
		count(v, sizeof v, a->has_tokens ? a->tokens.cache_read : 0);
		number(row, "cache read", v);
		snprintf(v, sizeof v, "$%.2f", a->cost_usd);
		number(row, "cost \xE2\x80\xA2 est", v);
		snprintf(v, sizeof v, "%u", (unsigned)a->turns);
		number(row, "turns", v);
	}

	if (a->last_prompt[0]) {
		section(card_body, "last prompt");
		lines_para(card_body, &lv_font_montserrat_20, C_INK, a->last_prompt, 3);
	}

	// Where it runs.
	b[0] = '\0';
	const char *parts[] = {a->model_label[0] ? a->model_label : a->model, a->effort, a->folder, a->branch,
			       a->app};
	for (size_t i = 0; i < sizeof parts / sizeof parts[0]; i++) {
		if (!parts[i][0])
			continue;
		if (b[0])
			strncat(b, " \xE2\x80\xA2 ", sizeof b - strlen(b) - 1);
		strncat(b, parts[i], sizeof b - strlen(b) - 1);
	}
	if (b[0]) {
		lv_obj_t *l = para(card_body, &lv_font_montserrat_16, C_DIM, b);
		lv_obj_set_style_pad_top(l, 12, 0);
	}
	if (a->has_account && a->account.name[0]) {
		lv_obj_t *row = box(card_body);
		lv_obj_set_size(row, lv_pct(100), LV_SIZE_CONTENT);
		lv_obj_set_flex_flow(row, LV_FLEX_FLOW_ROW);
		lv_obj_set_flex_align(row, LV_FLEX_ALIGN_START, LV_FLEX_ALIGN_CENTER, LV_FLEX_ALIGN_CENTER);
		lv_obj_set_style_pad_column(row, 8, 0);
		lv_obj_t *sq = box(row);
		lv_obj_set_size(sq, 12, 12);
		lv_obj_set_style_bg_color(sq, lv_color_hex(named_color(a->account.color)), 0);
		lv_obj_set_style_bg_opa(sq, LV_OPA_COVER, 0);
		lv_obj_t *n = label(row, &lv_font_montserrat_16, C_DIM);
		snprintf(b, sizeof b, "%s account", a->account.name);
		set_text(n, b, true);
	}

	// Actions.
	lv_obj_t *gap = box(card_body);
	lv_obj_set_size(gap, 1, 8);
	if (a->status != agentsdeck_link_v1_Status_STATUS_EXITED)
		button(card_body, hungry(a) ? "review in terminal" : "focus terminal", !waiting, ACTION_FOCUS, NULL);
	if (a->status == agentsdeck_link_v1_Status_STATUS_RUNNING && (!a->external || a->app[0]))
		button(card_body, "interrupt", false, ACTION_INTERRUPT, NULL);

	lv_obj_update_layout(card_body);
	lv_obj_scroll_to_y(card_body, scroll, LV_ANIM_OFF);

	lv_obj_t *t2 = label(card_screen, &lv_font_montserrat_20, C_BG); // the toast, above the body
	lv_obj_set_style_bg_color(t2, lv_color_hex(C_INK), 0);
	lv_obj_set_style_bg_opa(t2, LV_OPA_COVER, 0);
	lv_obj_set_style_pad_all(t2, 10, 0);
	lv_obj_set_width(t2, DISPLAY_W - 32);
	lv_label_set_long_mode(t2, LV_LABEL_LONG_WRAP);
	lv_obj_set_style_text_align(t2, LV_TEXT_ALIGN_CENTER, 0);
	lv_obj_align(t2, LV_ALIGN_TOP_MID, 0, 10);
	lv_obj_add_flag(t2, LV_OBJ_FLAG_HIDDEN);
	lv_obj_set_user_data(card_screen, t2);
}

static void open_card(const char *id)
{
	snprintf(open_id, sizeof open_id, "%s", id);
	card_body = NULL; // a fresh card starts at the top
	seen_version = UINT32_MAX;
}

// ---- notes (no deck) -----------------------------------------------------

static void build_note(void)
{
	note_screen = screen();
	note_title = label(note_screen, &lv_font_montserrat_36, C_INK);
	lv_obj_set_width(note_title, DISPLAY_W - 48);
	lv_label_set_long_mode(note_title, LV_LABEL_LONG_WRAP);
	lv_obj_align(note_title, LV_ALIGN_TOP_LEFT, 24, 140);
	note_body = label(note_screen, &lv_font_montserrat_24, C_DIM);
	lv_obj_set_width(note_body, DISPLAY_W - 48);
	lv_label_set_long_mode(note_body, LV_LABEL_LONG_WRAP);
	lv_obj_align_to(note_body, note_title, LV_ALIGN_OUT_BOTTOM_LEFT, 0, 24);
}

static void draw_note(const agents_t *s)
{
	char b[200];
	switch (s->link) {
	case LINK_NOT_APPROVED:
		lv_label_set_text(note_title, "not approved yet");
		snprintf(b, sizeof b, "on the Mac, run:\n\nagentctl devices approve\n    %s\n\nand to answer from here:\n\nagentctl devices scope\n    %s act",
			 s->device_name, s->device_name);
		break;
	case LINK_OFFLINE:
		lv_label_set_text(note_title, "deck offline");
		snprintf(b, sizeof b, "the deck stopped answering. it reconnects by itself when agentctl runs again.");
		break;
	default:
		lv_label_set_text(note_title, "waiting for the deck");
		snprintf(b, sizeof b, "on the Mac, run:\n\nagentctl link add <port>\n\n(agentctl link ports lists it)");
	}
	lv_label_set_text(note_body, b);
	lv_obj_align_to(note_body, note_title, LV_ALIGN_OUT_BOTTOM_LEFT, 0, 24);
}

// ---- update ---------------------------------------------------------------

void ui_init(void)
{
	build_grid();
	build_note();
	card_copy = heap_caps_calloc(1, sizeof *card_copy, MALLOC_CAP_SPIRAM);
	lv_screen_load(note_screen);
	lv_timer_create(animate, 100, NULL);
}

static void draw_toast(lv_obj_t *t, const agents_t *s)
{
	if (!t)
		return;
	if (s->message[0] && (int32_t)(s->message_until_ms - now_ms()) > 0) {
		set_text(t, s->message, true);
		lv_obj_remove_flag(t, LV_OBJ_FLAG_HIDDEN);
	} else {
		lv_obj_add_flag(t, LV_OBJ_FLAG_HIDDEN);
	}
}

void ui_update(void)
{
	static uint32_t last_tick, last_card;
	uint32_t now = now_ms();
	agents_t *s = agents_lock();
	// Redraw on a change, and every second for the "ago" times and the toast.
	if (s->version == seen_version && now - last_tick < 1000) {
		agents_unlock();
		return;
	}
	seen_version = s->version;
	last_tick = now;
	clock_offset_ms = s->clock_offset_ms;

	if (s->link != LINK_ONLINE) {
		open_id[0] = '\0';
		draw_note(s);
		agents_unlock();
		show(note_screen);
		return;
	}
	const agentsdeck_link_v1_Agent *a = open_id[0] ? agents_find(s, open_id) : NULL;
	if (open_id[0] && !a)
		open_id[0] = '\0'; // it left the deck
	if (a) {
		bool changed = card_body == NULL || memcmp(card_copy, a, sizeof *a) != 0 || now - last_card > 30000;
		// Rebuilding the card under a finger would stop the scroll: wait
		// until it's let go and the scroll has settled.
		bool busy = card_body && (lv_indev_get_state(lv_indev_get_next(NULL)) == LV_INDEV_STATE_PRESSED ||
					  lv_obj_is_scrolling(card_body));
		if (changed && busy) {
			seen_version = UINT32_MAX; // look again next time round
		} else if (changed) {
			*card_copy = *a;
			last_card = now;
			draw_card(card_copy);
		}
		draw_toast(lv_obj_get_user_data(card_screen), s);
		agents_unlock();
		show(card_screen);
		return;
	}
	draw_grid(s);
	draw_toast(toast, s);
	agents_unlock();
	show(grid_screen);
}
