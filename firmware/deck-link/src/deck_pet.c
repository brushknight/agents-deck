#include "deck_pet.h"

#include <string.h>

/* Plain C that also builds as C++ (sim/render.cpp in panel-core includes it). */

/* ---- the art (pets.js ART, row for row), in deck_pet_art_t order -----------
 * . empty · b body · d body shade · e eye · w shell · v shell shade
 * s spot (hat colour) · k crack / dark line · g ground · p prop (the pencil) */
#define ROWS 14
static const char *const art_rows[][ROWS] = {
	{ /* egg */
		"......wwww......", "....wwwwwwww....", "...wwwwwswwwv...", "..wwwwwsssswvv..", "..wwwwwwswwwwv..",
		".wwwwwwwwwwwwvv.", ".wswwwwwwwwwsvv.", ".ssswwwwwwwsssv.", ".wswwwwwwwwwsvv.", ".wwwwwwwwwwwwvv.",
		"..wwwwwwwwwwwv..", "..wwwwwswwwwvv..", "...wwwwwwwwvv...", ".....vvvvvv.....",
	},
	{ /* crack */
		"......wwww......", "....wwwwwwww....", "...wwwwwswwwv...", "..wwwwwsssswvv..", "..wwwwwwswwwwv..",
		".wwkwwwkwwwkwvv.", ".wskkwkwkwkkkvv.", ".ssswkwwwkwsssv.", ".wswwwwwwwwwsvv.", ".wwwwwwwwwwwwvv.",
		"..wwwwwwwwwwwv..", "..wwwwwswwwwvv..", "...wwwwwwwwvv...", ".....vvvvvv.....",
	},
	{ /* baby */
		"................", "................", "................", "................", ".....bbbbbb.....",
		"....bbbbbbbb....", "...bbbbbbbbbb...", "...bbebbbbebb...", "...bbebbbbebb...", "...bbbbddbbbb...",
		"...bbbbbbbbbb...", "....bbbbbbbb....", "....b.b..b.b....", "................",
	},
	{ /* kid */
		"................", "................", "................", "................", "................",
		"...bbbbbbbbbb...", "...bbbbbbbbbb...", "..bbbebbbbebbb..", "..bbbebbbbebbb..", "...bbbbbbbbbb...",
		"...bbbbbbbbbb...", "....b.b..b.b....", "....b.b..b.b....", "................",
	},
	{ /* builder */
		"................", "..............p.", ".............pp.", ".bbbbbbbbbbbbpb.", ".bbbbbbbbbbbpbb.",
		".bbbebbbbbebbbb.", "bbbbebbbbbebbbbb", "bbbbbbbbbbbbbbbb", "bbbbbbbbbbbbbbbb", ".bbbbbbbbbbbbbb.",
		"..b.b......b.b..", "..b.b......b.b..", "................", "................",
	},
	{ /* weak */
		"................", "................", "................", "................", "..p.............",
		".ddddddddddddd..", ".ddddddddddddd..", ".ddkkddddkkdddd.", "dddddddddddddddd", "ddddddkkkddddddd",
		".dddddddddddddd.", "..d.d......d.d..", "gggggggggggggggg", "................",
	},
};

/* Where the hat sits (pets.js HAT_AT): the body's top row, twice its centre
 * column, and whether it takes the small cut. */
typedef struct { int8_t top, cx2; bool small; } hat_at_t;
static const hat_at_t hat_at[] = {
	{0, 0, false}, {0, 0, false}, {0, 0, false}, /* egg, crack, baby: no hat */
	{5, 15, true},                               /* kid */
	{3, 15, false},                              /* builder */
	{5, 16, false},                              /* weak */
};

/* The deck's hats, still (deck-core.js HAT_ART critterStill; sprites.h kHats).
 * a = hat colour, b = shade, c = highlight, d = near-black. */
typedef struct {
	int8_t x, top;
	const char *rows[5];
	struct { int8_t x, y; char key; } extras[4];
} pet_hat_t;
static const pet_hat_t hats[DECK_PET_HAT_SHAPE_COUNT] = {
	{0, -4, {"....aaaaa.....", "...acaaaaa....", "..aaaaaaaaa...", ".bbbbbbbbbbbb."}, {{0, 0, 0}}},   /* hard hat */
	{-3, -3, {"........aaaaaa", ".......aaaaacaa", "bbbbbbbbbbbbbbb"}, {{0, 0, 0}}},                  /* cap */
	{0, -4, {".....cc.......", "....aaaaaa....", "...aaaaaaaa...", "..babababab..."}, {{0, 0, 0}}},   /* beanie */
	{0, -4, {"....aaaaaa....", "...aaaaaaaa...", "..aaaacaaaaa..", "..bbbbbbbbbb.."}, {{0, 0, 0}}},   /* helmet */
	{0, -5, {"........a.....", ".......aa.....", "......aaca....", ".....aaaaaa...", "..bbbbbbbbbb.."}, {{0, 0, 0}}}, /* wizard */
	{0, -4, {"......b.......", "...aaaaaaaa...", "..aaaaaaaaaaa.", "...bbbbbbbb..."}, {{0, 0, 0}}},   /* beret */
	{0, -4, {"...cc.cc.cc...", "..cccccccccc..", "...cccccccc...", "...aaaaaaaa..."}, {{0, 0, 0}}},   /* chef */
	{0, -4, {"...a..aa..a...", "...aa.aa.aa...", "...aaaaaaaa...", "...acaacaaca.."}, {{0, 0, 0}}},   /* crown */
	{0, -3, {"....aaaaaa....", "...a......a...", "..a........a.."}, {{0, 0, 0}}},                     /* headphones */
	{0, -4, {"..cccc.aaaa...", ".......d......", "....ababab....", "...aaaaaaaa..."}, {{0, 0, 0}}},   /* propeller */
	{0, -5, {"...aaaaaaaa...", "...aaaaaaaa...", "...aaaaaaaa...", "...bbbbbbbb...", ".aaaaaaaaaaaa."}, {{0, 0, 0}}}, /* top hat */
	{0, -4, {".....aaaa.....", "....aabbaa....", "b...aaaaaa...b", "bbbbbbbbbbbbbb"}, {{0, 0, 0}}},   /* cowboy */
	{0, -1, {"..aaaaaaaaaa..", "..acacacacac.."}, {{1, -1, 'b'}, {0, -2, 'b'}, {0, 0, 'b'}, {-1, 0, 'b'}}}, /* bandana */
};
#define HAT_HEADPHONES 8

/* Smaller cuts of the hats, for the kid (pets.js SMALL_HATS), same order. */
static const char *const small_hats[DECK_PET_HAT_SHAPE_COUNT][4] = {
	{".aaaa.", "aacaaa", "bbbbbbb"},          /* hard hat */
	{"..aaaa.", ".aaaaca", "bbbbbbbb"},       /* cap */
	{"..cc..", ".aaaa.", "bababa"},           /* beanie */
	{".aaaa.", "aacaaa", "bbbbbb"},           /* helmet */
	{"...a..", "..aca.", ".aaaa.", "bbbbbb"}, /* wizard */
	{"..b...", ".aaaaa.", "aaaaaaa", ".bbbbb."}, /* beret */
	{".cccc.", "cccccc", ".cccc.", ".aaaa."}, /* chef */
	{"a.aa.a", "aaaaaa", "acaaca"},           /* crown */
	{".aaaa.", "a....a", "b....b"},           /* headphones */
	{"cc.aa", "..d..", "ababa"},              /* propeller */
	{".aaaa.", ".bbbb.", "aaaaaa"},           /* top hat */
	{"..aa..", "b.aa.b", "bbbbbb"},           /* cowboy */
	{".aaaaaa.", "acacacac"},                 /* bandana */
};

static const char *const hat_shapes[] = DECK_PET_HAT_SHAPES;
static const char *const hat_colors[] = DECK_PET_HAT_COLORS;
/* {main, shade} per colour (deck-core.js HAT_COLORS). */
static const uint32_t hat_rgb[DECK_PET_HAT_COLOR_COUNT][2] = {
	{0x4FB3A6, 0x2F7F75}, {0x6F8FD8, 0x4A63A3}, {0xE8B94A, 0xB08429}, {0x6DBE6A, 0x468A44},
	{0xA68BD8, 0x7660A6}, {0xE58BB0, 0xB05E80}, {0x5EC4D6, 0x3A8E9C}, {0xE8E4DC, 0xA8A39A},
};

/* The aura's colour and strength per stage (pets.js AURA: outer, opacity). */
typedef struct { uint32_t rgb; uint8_t alpha; } aura_t;
static const aura_t auras[] = {
	{0, 0},           /* none */
	{0xFFE08A, 191},  /* strong */
	{0x3FA9F5, 179},  /* mighty */
	{0xE8A317, 217},  /* legend */
};

/* Hat colours close to each aura's (contrast under 1.6, hue within 40
 * degrees; test_pet works it out from the colours): bits by hat colour. */
static const uint8_t aura_hat_close[] = {
	0,                        /* none */
	(1 << 2) | (1 << 7),      /* strong: yellow, ink */
	(1 << 0) | (1 << 1) | (1 << 6), /* mighty: teal, blue, sky */
	(1 << 2),                 /* legend: yellow */
};

#define WEAK_BODY 0xB0673F
#define PLATE 0x0A0A0A
#define ICE 0x8FD0F2
#define ICE_EDGE 0xDFF4FF
/* Cells round the art in cell[][]: the first and second ring. */
#define RING1 '1'
#define RING2 '2'

/* ---- the pet's state --------------------------------------------------------- */

static const char *const stage_keys[] = {"egg", "baby", "kid", "grown", "strong", "mighty", "legend"};
static const char *const stage_names[] = {"Egg", "Baby", "Kid", "Grown-up", "Strong", "Mighty", "Legend"};
static const char *const mood_keys[] = {"fed", "hungry", "weak", "asleep"};

deck_pet_stage_t deck_pet_stage_of(const char *stage)
{
	if (!stage || !*stage)
		return DECK_PET_EGG;
	for (int i = 0; i < (int)(sizeof stage_keys / sizeof stage_keys[0]); i++)
		if (strcmp(stage, stage_keys[i]) == 0)
			return (deck_pet_stage_t)i;
	return DECK_PET_GROWN;
}

deck_pet_mood_t deck_pet_mood_of(const char *mood)
{
	for (int i = 0; mood && i < (int)(sizeof mood_keys / sizeof mood_keys[0]); i++)
		if (strcmp(mood, mood_keys[i]) == 0)
			return (deck_pet_mood_t)i;
	return DECK_PET_FED;
}

const char *deck_pet_stage_name(uint8_t stage)
{
	return stage < sizeof stage_names / sizeof stage_names[0] ? stage_names[stage] : "";
}

uint8_t deck_pet_hat_of(const char *shape)
{
	for (int i = 0; shape && i < DECK_PET_HAT_SHAPE_COUNT; i++)
		if (strcmp(shape, hat_shapes[i]) == 0)
			return (uint8_t)(i + 1);
	return 0;
}

uint8_t deck_pet_hat_color_of(const char *color)
{
	for (int i = 0; color && i < DECK_PET_HAT_COLOR_COUNT; i++)
		if (strcmp(color, hat_colors[i]) == 0)
			return (uint8_t)i;
	return 0;
}

static uint16_t clamp16(uint64_t v)
{
	return v > 0xFFFF ? 0xFFFF : (uint16_t)v;
}

void deck_pet_from_proto(deck_pet_t *out, bool has_pet, const agentsdeck_link_v1_Pet *in)
{
	memset(out, 0, sizeof *out);
	if (!has_pet || !in)
		return;
	out->has = true;
	out->stage = (uint8_t)deck_pet_stage_of(in->stage);
	out->mood = (uint8_t)deck_pet_mood_of(in->mood);
	out->cracked = in->ate >= DECK_PET_CRACK_AT;
	out->hearts = in->hearts > 4 ? 4 : (uint8_t)in->hearts;
	out->level = clamp16(in->level);
	/* Fields a newer link.proto adds; nanopb names each one's tag, so these
	 * take effect when the code is regenerated with them. */
#ifdef agentsdeck_link_v1_Pet_ready_tag
	out->ready = in->ready ? true : false;
#endif
#ifdef agentsdeck_link_v1_Pet_level_from_tag
	out->level_from = clamp16(in->level_from);
#endif
	size_t n = 0;
	while (n < sizeof in->name && in->name[n])
		n++;
	if (n > sizeof out->name - 1) {
		n = sizeof out->name - 1;
		while (n > 0 && ((uint8_t)in->name[n] & 0xC0) == 0x80)
			n--;
	}
	memcpy(out->name, in->name, n);
	out->name[n] = '\0';
	for (char *p = out->name; *p; p++)
		if ((uint8_t)*p < 0x20)
			*p = ' ';
}

/* ---- the look ---------------------------------------------------------------- */

static char *cell_ref(deck_pet_look_t *l, int x, int y)
{
	x -= DECK_PET_GRID_X0;
	y -= DECK_PET_GRID_Y0;
	if (x < 0 || y < 0 || x >= DECK_PET_GRID_W || y >= DECK_PET_GRID_H)
		return NULL;
	return &l->cell[y][x];
}

static void put(deck_pet_look_t *l, int x, int y, char key)
{
	char *c = cell_ref(l, x, y);
	if (c)
		*c = key;
}

static bool art_key(char k)
{
	return k && k != RING1 && k != RING2;
}

char deck_pet_at(const deck_pet_look_t *look, int x, int y)
{
	char *c = cell_ref((deck_pet_look_t *)look, x, y);
	return c && art_key(*c) ? *c : 0;
}

/* Python's round of n / 2 (halves to even), as the design was drawn with it. */
static int round_half(int n)
{
	int f = n >= 0 ? n / 2 : -((-n + 1) / 2);
	if (n - 2 * f == 0)
		return f;
	return f % 2 ? f + 1 : f;
}

static void put_hat_cell(deck_pet_look_t *l, int x, int y, char key)
{
	if (!cell_ref(l, x, y))
		return;
	put(l, x, y, key);
	l->hat_cells[y - DECK_PET_GRID_Y0] |= 1u << (x - DECK_PET_GRID_X0);
}

bool deck_pet_hat_at(const deck_pet_look_t *look, int x, int y)
{
	if (!cell_ref((deck_pet_look_t *)look, x, y))
		return false;
	return (look->hat_cells[y - DECK_PET_GRID_Y0] >> (x - DECK_PET_GRID_X0)) & 1u;
}

bool deck_pet_hat_blends(uint8_t aura, uint8_t hat_color)
{
	return aura < sizeof aura_hat_close && hat_color < DECK_PET_HAT_COLOR_COUNT &&
	       ((aura_hat_close[aura] >> hat_color) & 1u);
}

/* pets.js HAT_KEY: a/b/c/d become h (hat colour), H (shade), c, k. */
static char hat_key(char ch)
{
	switch (ch) {
	case 'a': return 'h';
	case 'b': return 'H';
	case 'c': return 'c';
	case 'd': return 'k';
	default: return 0;
	}
}

static void put_hat(deck_pet_look_t *l, uint8_t hat)
{
	if (hat < 1 || hat > DECK_PET_HAT_SHAPE_COUNT)
		return;
	if (l->art != DECK_PET_ART_KID && l->art != DECK_PET_ART_BUILDER && l->art != DECK_PET_ART_WEAK)
		return;
	const int shape = hat - 1;
	const int top = hat_at[l->art].top, cx2 = hat_at[l->art].cx2;
	if (hat_at[l->art].small) {
		const char *const *rows = small_hats[shape];
		int n = 0;
		while (n < 4 && rows[n])
			n++;
		const int x0 = round_half(cx2 - (int)strlen(rows[0]) + 1), y0 = top - n;
		for (int i = 0; i < n; i++)
			for (int j = 0; rows[i][j]; j++)
				if (hat_key(rows[i][j]))
					put_hat_cell(l, x0 + j, y0 + i, hat_key(rows[i][j]));
		return;
	}
	const pet_hat_t *f = &hats[shape];
	const int ox = 1 + round_half(cx2 - 15);
	for (int i = 0; i < 5 && f->rows[i]; i++)
		for (int j = 0; f->rows[i][j]; j++)
			if (hat_key(f->rows[i][j]))
				put_hat_cell(l, f->x + ox + j, f->top + top + i, hat_key(f->rows[i][j]));
	for (int i = 0; i < 4 && f->extras[i].key; i++)
		put_hat_cell(l, f->extras[i].x + ox, f->extras[i].y + top, hat_key(f->extras[i].key));
	if (shape == HAT_HEADPHONES) /* the ear cups, beside the head */
		for (int dy = 0; dy < 2; dy++) {
			put_hat_cell(l, ox - 1, top + dy, 'H');
			put_hat_cell(l, ox + 14, top + dy, 'H');
		}
}

/* Eyes shut: each eye becomes a short line at its lower row, turned outwards. */
static void shut_eyes(deck_pet_look_t *l)
{
	int lx = 999, rx = -999, low = -999;
	for (int y = 0; y < ROWS; y++)
		for (int x = 0; x < 16; x++)
			if (deck_pet_at(l, x, y) == 'e') {
				if (x < lx) lx = x;
				if (x > rx) rx = x;
				if (y > low) low = y;
				put(l, x, y, 'b');
			}
	if (low < 0)
		return;
	put(l, lx - 1, low, 'e');
	put(l, lx, low, 'e');
	put(l, rx, low, 'e');
	put(l, rx + 1, low, 'e');
}

/* The open mouth of a hungry pet (pets.js MOUTH): 2 x 2 cells. */
static bool mouth_of(uint8_t art, int *mx, int *my)
{
	switch (art) {
	case DECK_PET_ART_BUILDER: *mx = 7; *my = 7; return true;
	case DECK_PET_ART_KID:
	case DECK_PET_ART_BABY: *mx = 7; *my = 9; return true;
	default: return false;
	}
}

/* Marks the empty cells round the art: RING1 next to it (eight ways), RING2 next to those. */
static void rings(deck_pet_look_t *l, int count)
{
	for (int r = 1; r <= count; r++) {
		const char mark = r == 1 ? RING1 : RING2;
		for (int y = l->y0 - r; y <= l->y1 + r; y++)
			for (int x = l->x0 - r; x <= l->x1 + r; x++) {
				char *c = cell_ref(l, x, y);
				if (!c || *c)
					continue;
				bool near = false;
				for (int dy = -1; dy <= 1 && !near; dy++)
					for (int dx = -1; dx <= 1 && !near; dx++) {
						char *n = cell_ref(l, x + dx, y + dy);
						near = n && *n && *n != mark && (r == 1 ? art_key(*n) : *n == RING1);
					}
				if (near)
					*c = mark;
			}
	}
}

/* The plate: a square of dark cells one larger than the art and hat on
 * every side (round the ice and its frost when asleep), kept in the grid. */
static void place_plate(deck_pet_look_t *l)
{
	int x0 = l->x0 - 1, x1 = l->x1 + 1, y0 = l->y0 - 1, y1 = l->y1 + 1;
	if (l->ice) { /* the cube is one round the art, the frost a row under it and one wider */
		x0 = l->x0 - 2, x1 = l->x1 + 2, y1 = l->y1 + 2;
	}
	int w = x1 - x0 + 1, h = y1 - y0 + 1;
	if (w < h) {
		x0 -= (h - w) / 2;
		x1 = x0 + h - 1;
	} else if (h < w) {
		y0 -= (w - h) / 2;
		y1 = y0 + w - 1;
	}
	if (x0 < DECK_PET_GRID_X0) x0 = DECK_PET_GRID_X0;
	if (y0 < DECK_PET_GRID_Y0) y0 = DECK_PET_GRID_Y0;
	if (x1 > DECK_PET_GRID_X0 + DECK_PET_GRID_W - 1) x1 = DECK_PET_GRID_X0 + DECK_PET_GRID_W - 1;
	if (y1 > DECK_PET_GRID_Y0 + DECK_PET_GRID_H - 1) y1 = DECK_PET_GRID_Y0 + DECK_PET_GRID_H - 1;
	l->plate_x0 = (int8_t)x0, l->plate_y0 = (int8_t)y0, l->plate_x1 = (int8_t)x1, l->plate_y1 = (int8_t)y1;
}

void deck_pet_look(deck_pet_look_t *look, const deck_pet_t *pet, const deck_pet_opts_t *opts)
{
	static const deck_pet_opts_t none = {0, 0, false, false, false};
	if (!opts)
		opts = &none;
	memset(look, 0, sizeof *look);
	const uint8_t stage = pet ? pet->stage : (uint8_t)DECK_PET_EGG;
	const uint8_t mood = pet ? pet->mood : (uint8_t)DECK_PET_FED;
	const bool egg = stage == DECK_PET_EGG;
	const bool weak = mood == DECK_PET_WEAK && !egg;
	const bool asleep = mood == DECK_PET_ASLEEP;
	uint8_t art = egg ? (pet && pet->cracked ? DECK_PET_ART_CRACK : DECK_PET_ART_EGG)
	            : stage == DECK_PET_BABY ? DECK_PET_ART_BABY
	            : stage == DECK_PET_KID  ? DECK_PET_ART_KID
	                                     : DECK_PET_ART_BUILDER;
	if (weak && art == DECK_PET_ART_BUILDER)
		art = DECK_PET_ART_WEAK;
	look->art = art;
	look->weak = weak;
	look->ice = asleep;
	look->plate = opts->plate;
	look->ready = egg && pet && pet->ready;
	look->bob = opts->working && !asleep;
	if (!weak && !asleep && stage >= DECK_PET_STRONG && stage <= DECK_PET_LEGEND)
		look->aura = (uint8_t)(DECK_PET_AURA_STRONG + (stage - DECK_PET_STRONG));
	const uint8_t col = opts->hat && opts->hat_color < DECK_PET_HAT_COLOR_COUNT ? opts->hat_color : 0;
	look->hat_rgb = hat_rgb[col][0];
	look->hat_shade_rgb = hat_rgb[col][1];
	if (opts->hat && deck_pet_hat_blends(look->aura, col)) { /* the hat in its shade, to stand out from the aura */
		look->hat_rgb = look->hat_shade_rgb;
		look->hat_toned = true;
	}

	for (int y = 0; y < ROWS; y++)
		for (int x = 0; x < 16; x++)
			if (art_rows[art][y][x] != '.')
				put(look, x, y, art_rows[art][y][x]);
	if (!egg && art != DECK_PET_ART_WEAK) {
		int mx, my;
		if (mood == DECK_PET_HUNGRY && mouth_of(art, &mx, &my)) {
			for (int i = 0; i < 4; i++)
				put(look, mx + i % 2, my + i / 2, 'm');
		} else if (asleep || weak || opts->idle) {
			shut_eyes(look);
		}
	}
	if (art != DECK_PET_ART_BABY && !egg)
		put_hat(look, opts->hat);

	look->x0 = look->y0 = 127;
	look->x1 = look->y1 = -128;
	for (int y = DECK_PET_GRID_Y0; y < DECK_PET_GRID_Y0 + DECK_PET_GRID_H; y++)
		for (int x = DECK_PET_GRID_X0; x < DECK_PET_GRID_X0 + DECK_PET_GRID_W; x++)
			if (deck_pet_at(look, x, y)) {
				if (x < look->x0) look->x0 = (int8_t)x;
				if (x > look->x1) look->x1 = (int8_t)x;
				if (y < look->y0) look->y0 = (int8_t)y;
				if (y > look->y1) look->y1 = (int8_t)y;
			}
	if (look->plate)
		place_plate(look);
	if (look->ice)
		return;
	if (look->aura)
		rings(look, 1);
	if (look->aura) { /* a cell of background between the aura and the hat */
		const char mark = RING1;
		for (int y = DECK_PET_GRID_Y0; y < DECK_PET_GRID_Y0 + DECK_PET_GRID_H; y++)
			for (int x = DECK_PET_GRID_X0; x < DECK_PET_GRID_X0 + DECK_PET_GRID_W; x++) {
				char *c = cell_ref(look, x, y);
				bool near = false;
				for (int d = 0; d < 9 && !near && *c == mark; d++)
					near = deck_pet_hat_at(look, x + d % 3 - 1, y + d / 3 - 1);
				if (near)
					*c = 0;
			}
	}
}

uint32_t deck_pet_rgb(const deck_pet_look_t *look, char key)
{
	switch (key) {
	case 'b': return look->weak ? WEAK_BODY : 0xFF4D00;
	case 'd': return look->weak ? WEAK_BODY : 0xC23A00;
	case 'e': return 0x141412;
	case 'w': return 0xF4F0E6;
	case 'v': return 0xB9B4A8;
	case 'k': return 0x0A0A0A;
	case 'g': return 0x55524B;
	case 'p': return 0xE8E4DC;
	case 'm': return 0xBDB8AD; /* light grey, so it reads on a dark tile */
	case 'c': return 0xF7F4EC;
	case 's':
	case 'h': return look->hat_rgb;
	case 'H': return look->hat_shade_rgb;
	default: return 0;
	}
}

/* ---- drawing ------------------------------------------------------------------ */

typedef struct {
	deck_pet_fill_fn fill;
	void *user;
	int x, y, s;
} pen_t;

static void cells(const pen_t *p, int cx, int cy, int n, uint32_t rgb, uint8_t alpha)
{
	p->fill(p->user, p->x + cx * p->s, p->y + cy * p->s, n * p->s, p->s, rgb, alpha);
}

/* Each row's runs of one colour, for the cells `pick` gives a colour (alpha 0: skip). */
typedef bool (*pick_fn)(const deck_pet_look_t *l, char key, uint32_t *rgb, uint8_t *alpha);

static void runs(const deck_pet_look_t *l, const pen_t *p, pick_fn pick)
{
	for (int y = DECK_PET_GRID_Y0; y < DECK_PET_GRID_Y0 + DECK_PET_GRID_H; y++) {
		int start = 0, n = 0;
		uint32_t run_rgb = 0;
		uint8_t run_a = 0;
		for (int x = DECK_PET_GRID_X0; x <= DECK_PET_GRID_X0 + DECK_PET_GRID_W; x++) {
			uint32_t rgb = 0;
			uint8_t a = 0;
			const bool in = x < DECK_PET_GRID_X0 + DECK_PET_GRID_W &&
			                pick(l, l->cell[y - DECK_PET_GRID_Y0][x - DECK_PET_GRID_X0], &rgb, &a);
			if (n && (!in || rgb != run_rgb || a != run_a)) {
				cells(p, start, y, n, run_rgb, run_a);
				n = 0;
			}
			if (in) {
				if (!n) {
					start = x;
					run_rgb = rgb;
					run_a = a;
				}
				n++;
			}
		}
	}
}

static bool pick_ring(const deck_pet_look_t *l, char key, uint32_t *rgb, uint8_t *alpha)
{
	if (key != RING1 && key != RING2)
		return false;
	*rgb = auras[l->aura].rgb;
	*alpha = auras[l->aura].alpha;
	return true;
}

static bool pick_art(const deck_pet_look_t *l, char key, uint32_t *rgb, uint8_t *alpha)
{
	if (!art_key(key))
		return false;
	*rgb = deck_pet_rgb(l, key);
	*alpha = 255;
	return true;
}

void deck_pet_draw(const deck_pet_look_t *l, int x, int y, int scale, deck_pet_fill_fn fill, void *user)
{
	if (!l || !fill || scale < 1 || l->x1 < l->x0)
		return;
	const pen_t p = {fill, user, x, y, scale};
	const int x0 = l->x0 - 1, x1 = l->x1 + 1, y0 = l->y0 - 1, y1 = l->y1 + 1; /* the ice cube */
	if (l->plate) /* the dark plate under it all */
		fill(user, x + l->plate_x0 * scale, y + l->plate_y0 * scale, (l->plate_x1 - l->plate_x0 + 1) * scale,
		     (l->plate_y1 - l->plate_y0 + 1) * scale, PLATE, 255);
	if (l->ice) {
		cells(&p, x0 - 1, y1 + 1, x1 - x0 + 3, ICE_EDGE, 115); /* the frost under it */
	} else if (l->aura) {
		runs(l, &p, pick_ring);
	}
	runs(l, &p, pick_art);
	if (l->ice) {
		fill(user, x + x0 * scale, y + y0 * scale, (x1 - x0 + 1) * scale, (y1 - y0 + 1) * scale, ICE, 102);
		cells(&p, x0, y0, x1 - x0 + 1, ICE_EDGE, 230);
		cells(&p, x0, y1, x1 - x0 + 1, ICE_EDGE, 230);
		for (int cy = y0 + 1; cy < y1; cy++) {
			cells(&p, x0, cy, 1, ICE_EDGE, 230);
			cells(&p, x1, cy, 1, ICE_EDGE, 230);
		}
		for (int i = 0; i < 3; i++) /* the shine */
			cells(&p, x0 + 1 + i, y0 + 1 + i, 1, 0xFFFFFF, 204);
	}
}

void deck_pet_bounds(const deck_pet_look_t *l, int scale, int *bx, int *by, int *bw, int *bh)
{
	/* Two cells round the art cover the aura and the ice; the plate may reach further. */
	const int pad = 2;
	int x0 = l->x0 - pad, y0 = l->y0 - pad, x1 = l->x1 + pad, y1 = l->y1 + pad;
	if (l->plate) {
		if (l->plate_x0 < x0) x0 = l->plate_x0;
		if (l->plate_y0 < y0) y0 = l->plate_y0;
		if (l->plate_x1 > x1) x1 = l->plate_x1;
		if (l->plate_y1 > y1) y1 = l->plate_y1;
	}
	*bx = x0 * scale;
	*by = y0 * scale;
	*bw = (x1 - x0 + 1) * scale;
	*bh = (y1 - y0 + 1) * scale;
}

void deck_pet_motion(const deck_pet_look_t *l, uint32_t ms, int scale, int *dx, int *dy)
{
	const int half = scale / 2 > 0 ? scale / 2 : 1;
	*dx = *dy = 0;
	if (l->ready) { /* rock, rock, then rest: 1.6 s */
		static const int8_t rock[8] = {0, 1, 0, -1, 0, 0, 0, 0};
		*dx = rock[ms % 1600 / 200] * half;
	} else if (l->bob && ms % 1000 >= 500) {
		*dy = -half;
	}
}
