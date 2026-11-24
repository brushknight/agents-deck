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

/* The deck's hats, still (deck-core.js HAT_ART critterStill; sprites.h
 * kHats). a = hat colour, b = shade, c = highlight, d = near-black. */
typedef struct {
	int8_t x, top;
	const char *rows[5];
	struct { int8_t x, y; char key; } extras[5];
} hat_frame_t;
static const hat_frame_t hats_still[13] = {
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
#define HAT_HORNS 13 /* devil horns: pets only */

/* Smaller cuts of the hats, for the kid (pets.js SMALL_HATS), same order. */
static const char *const small_hats[13][4] = {
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

/* Devil horns on the head (pets.js HORNS): every third cell in the shade. */
static const int8_t horns[][6][2] = {
	{{3, 3}, {3, 4}, {4, 4}, {12, 3}, {12, 4}, {11, 4}},  /* kid */
	{{2, 1}, {2, 2}, {3, 2}, {13, 1}, {13, 2}, {12, 2}},  /* builder */
	{{2, 3}, {2, 4}, {3, 4}, {13, 3}, {13, 4}, {12, 4}},  /* weak */
};

static const char *const hat_shapes[] = DECK_PET_HAT_SHAPES;
static const char *const hat_colors[] = DECK_PET_HAT_COLORS;
/* {main, shade} per colour (deck-core.js HAT_COLORS). */
static const uint32_t hat_rgb[DECK_PET_HAT_COLOR_COUNT][2] = {
	{0x4FB3A6, 0x2F7F75}, {0x6F8FD8, 0x4A63A3}, {0xE8B94A, 0xB08429}, {0x6DBE6A, 0x468A44},
	{0xA68BD8, 0x7660A6}, {0xE58BB0, 0xB05E80}, {0x5EC4D6, 0x3A8E9C}, {0xE8E4DC, 0xA8A39A},
};

/* The flames per stage (pets.js AURA): colour, and the tongues' heights. */
typedef struct { uint32_t rgb; uint8_t n; int8_t h[7]; } aura_t;
static const aura_t auras[] = {
	{0, 0, {0}},
	{0xFFE08A, 5, {1, 3, 1, 2, 2}},          /* strong */
	{0x3FA9F5, 6, {2, 4, 2, 3, 5, 2}},       /* mighty */
	{0xE8A317, 7, {3, 5, 2, 6, 3, 4, 5}},    /* legend */
};
#define STAR_RGB 0xFFF6C8

#define WEAK_BODY 0xB0673F
#define ICE 0x8FD0F2
#define ICE_EDGE 0xDFF4FF

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

/* How far `ate` is from `from` to `to`, 0..255. */
static uint8_t progress_of(int64_t ate, int64_t from, int64_t to)
{
	if (to <= from || ate <= from)
		return 0;
	if (ate >= to)
		return 255;
	return (uint8_t)((ate - from) * 255 / (to - from));
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
	out->hearts = in->hearts > DECK_PET_HEARTS ? DECK_PET_HEARTS : (uint8_t)in->hearts;
	out->level = clamp16(in->level);
	/* Where this level started: Pet.level_from when the deck sends it, else
	 * the default step (each level 1.25 times the one before). */
	int64_t from = out->level ? in->next_at * 4 / 5 : 0;
	/* Fields a newer link.proto adds; nanopb names each one's tag, so these
	 * take effect when the code is regenerated with them. */
#ifdef agentsdeck_link_v1_Pet_ready_tag
	out->ready = in->ready ? true : false;
#endif
#ifdef agentsdeck_link_v1_Pet_level_from_tag
	if (out->level && in->level_from > 0)
		from = in->level_from;
#endif
#ifdef agentsdeck_link_v1_Pet_hat_tag
	if (in->has_hat) {
		out->hat = deck_pet_hat_of(in->hat.shape);
		out->hat_color = deck_pet_hat_color_of(in->hat.color);
	}
#endif
	if (out->ready)
		out->cracked = true;
	out->progress = progress_of(in->ate, from, in->next_at);
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

/* ---- grids ------------------------------------------------------------------- */

typedef char grid_t[DECK_PET_GRID_H][DECK_PET_GRID_W];

static char *ref(grid_t g, int x, int y)
{
	x -= DECK_PET_GRID_X0;
	y -= DECK_PET_GRID_Y0;
	if (x < 0 || y < 0 || x >= DECK_PET_GRID_W || y >= DECK_PET_GRID_H)
		return NULL;
	return &g[y][x];
}

static void set(grid_t g, int x, int y, char key)
{
	char *c = ref(g, x, y);
	if (c)
		*c = key;
}

static char get(grid_t g, int x, int y)
{
	char *c = ref(g, x, y);
	return c ? *c : 0;
}

/* A hat's own small grid: x -3..16, y -4..7. */
#define HAT_X0 (-3)
#define HAT_Y0 (-4)
#define HAT_W 20
#define HAT_H 12
typedef char hgrid_t[HAT_H][HAT_W];

static void hset(hgrid_t g, int x, int y, char key)
{
	x -= HAT_X0;
	y -= HAT_Y0;
	if (x >= 0 && y >= 0 && x < HAT_W && y < HAT_H)
		g[y][x] = key;
}

static char hget(hgrid_t g, int x, int y)
{
	x -= HAT_X0;
	y -= HAT_Y0;
	return x >= 0 && y >= 0 && x < HAT_W && y < HAT_H ? g[y][x] : 0;
}

static bool in_grid(int x, int y)
{
	return x >= DECK_PET_GRID_X0 && y >= DECK_PET_GRID_Y0 && x < DECK_PET_GRID_X0 + DECK_PET_GRID_W &&
	       y < DECK_PET_GRID_Y0 + DECK_PET_GRID_H;
}

static void bit_set(uint32_t *rows, int x, int y)
{
	if (in_grid(x, y))
		rows[y - DECK_PET_GRID_Y0] |= 1u << (x - DECK_PET_GRID_X0);
}

static bool bit_at(const uint32_t *rows, int x, int y)
{
	return in_grid(x, y) && ((rows[y - DECK_PET_GRID_Y0] >> (x - DECK_PET_GRID_X0)) & 1u);
}

char deck_pet_at(const deck_pet_look_t *look, int x, int y)
{
	return in_grid(x, y) ? look->cell[y - DECK_PET_GRID_Y0][x - DECK_PET_GRID_X0] : 0;
}

bool deck_pet_hat_at(const deck_pet_look_t *look, int x, int y)
{
	return bit_at(look->hat_cells, x, y);
}

bool deck_pet_stroke_at(const deck_pet_look_t *look, int x, int y)
{
	return bit_at(look->stroke, x, y);
}

bool deck_pet_flame_at(const deck_pet_look_t *look, int x, int y)
{
	return bit_at(look->flame, x, y);
}

/* ---- the still look (pets.js petLook) ---------------------------------------- */

/* Python's round of n / 2 (halves to even), as the design was drawn with it. */
static int round_half(int n)
{
	int f = n >= 0 ? n / 2 : -((-n + 1) / 2);
	if (n - 2 * f == 0)
		return f;
	return f % 2 ? f + 1 : f;
}

/* pets.js HAT_KEY: a/b/c/d become h (hat colour), H (shade), c, K. */
static char hat_key(char ch)
{
	switch (ch) {
	case 'a': return 'h';
	case 'b': return 'H';
	case 'c': return 'c';
	case 'd': return 'K';
	default: return 0;
	}
}

static bool has_hat_spot(uint8_t art)
{
	return art == DECK_PET_ART_KID || art == DECK_PET_ART_BUILDER || art == DECK_PET_ART_WEAK;
}

/* One hat frame (or the still hat) on an art, into g. */
static void put_frame(hgrid_t g, uint8_t art, int shape, const hat_frame_t *f)
{
	const int top = hat_at[art].top, ox = 1 + round_half(hat_at[art].cx2 - 15);
	for (int i = 0; i < 5 && f->rows[i]; i++)
		for (int j = 0; f->rows[i][j]; j++)
			if (hat_key(f->rows[i][j]))
				hset(g, f->x + ox + j, f->top + top + i, hat_key(f->rows[i][j]));
	for (int i = 0; i < 5 && f->extras[i].key; i++)
		hset(g, f->extras[i].x + ox, f->extras[i].y + top, hat_key(f->extras[i].key));
	if (shape == HAT_HEADPHONES) /* the ear cups, beside the head */
		for (int dy = 0; dy < 2; dy++) {
			hset(g, ox - 1, top + dy, 'H');
			hset(g, ox + 14, top + dy, 'H');
		}
}

/* The pet's own hat, still, into g. */
static void put_hat(hgrid_t g, uint8_t art, uint8_t hat)
{
	if (hat < 1 || hat > DECK_PET_HAT_SHAPE_COUNT || !has_hat_spot(art))
		return;
	const int shape = hat - 1;
	if (shape == HAT_HORNS) {
		const int a = art - DECK_PET_ART_KID;
		for (int i = 0; i < 6; i++)
			hset(g, horns[a][i][0], horns[a][i][1], i % 3 == 0 ? 'H' : 'h');
		return;
	}
	if (hat_at[art].small) {
		const char *const *rows = small_hats[shape];
		int n = 0;
		while (n < 4 && rows[n])
			n++;
		const int x0 = round_half(hat_at[art].cx2 - (int)strlen(rows[0]) + 1), y0 = hat_at[art].top - n;
		for (int i = 0; i < n; i++)
			for (int j = 0; rows[i][j]; j++)
				if (hat_key(rows[i][j]))
					hset(g, x0 + j, y0 + i, hat_key(rows[i][j]));
		return;
	}
	put_frame(g, art, shape, &hats_still[shape]);
}

/* Eyes shut: each eye becomes a short line at its lower row, turned outwards. */
static void shut_eyes(grid_t g)
{
	int lx = 999, rx = -999, low = -999;
	for (int y = 0; y < ROWS; y++)
		for (int x = 0; x < 16; x++)
			if (get(g, x, y) == 'e') {
				if (x < lx) lx = x;
				if (x > rx) rx = x;
				if (y > low) low = y;
				set(g, x, y, 'b');
			}
	if (low < 0)
		return;
	set(g, lx - 1, low, 'e');
	set(g, lx, low, 'e');
	set(g, rx, low, 'e');
	set(g, rx + 1, low, 'e');
}

/* Scratch for one look: the still pet with its hat, and the hat on its own. */
static grid_t base_g;
static hgrid_t hat_g;

/* The flames round a strong, mighty or legend pet (pets.js auraSVG, its
 * still frame) and the legend's stars, from the still pet in base_g. */
static void flames(deck_pet_look_t *l)
{
	const aura_t *a = &auras[l->aura];
	int top[DECK_PET_GRID_W], sl[DECK_PET_GRID_H], sr[DECK_PET_GRID_H];
	int xs0 = 999, xs1 = -999, ys0 = 999, ys1 = -999;
	for (int i = 0; i < DECK_PET_GRID_W; i++) top[i] = 999;
	for (int i = 0; i < DECK_PET_GRID_H; i++) sl[i] = 999, sr[i] = -999;
	for (int y = DECK_PET_GRID_Y0; y < DECK_PET_GRID_Y0 + DECK_PET_GRID_H; y++)
		for (int x = DECK_PET_GRID_X0; x < DECK_PET_GRID_X0 + DECK_PET_GRID_W; x++)
			if (get(base_g, x, y)) {
				const int ix = x - DECK_PET_GRID_X0, iy = y - DECK_PET_GRID_Y0;
				if (y < top[ix]) top[ix] = y;
				if (x < sl[iy]) sl[iy] = x;
				if (x > sr[iy]) sr[iy] = x;
				if (x < xs0) xs0 = x;
				if (x > xs1) xs1 = x;
				if (y < ys0) ys0 = y;
				if (y > ys1) ys1 = y;
			}
	if (xs1 < xs0)
		return;
	/* the gap: a cell beside each row and over each column stays clear */
	uint32_t gap[DECK_PET_GRID_H];
	memset(gap, 0, sizeof gap);
	for (int y = ys0; y <= ys1; y++) {
		const int iy = y - DECK_PET_GRID_Y0;
		if (sr[iy] < sl[iy]) continue;
		bit_set(gap, sl[iy] - 1, y);
		bit_set(gap, sr[iy] + 1, y);
	}
	for (int x = xs0; x <= xs1; x++)
		if (top[x - DECK_PET_GRID_X0] != 999) bit_set(gap, x, top[x - DECK_PET_GRID_X0] - 1);
	uint32_t fl[DECK_PET_GRID_H];
	memset(fl, 0, sizeof fl);
	const int mid2 = ys0 + ys1; /* twice the middle row */
	for (int y = ys0; y <= ys1; y++) {
		const int iy = y - DECK_PET_GRID_Y0;
		if (sr[iy] < sl[iy]) continue;
		const int w = 2 * y <= mid2 ? 2 : 1;
		for (int k = 2; k < 2 + w; k++)
			if (y % 4 != 0 || k == 2) {
				bit_set(fl, sl[iy] - k, y);
				bit_set(fl, sr[iy] + k, y);
			}
	}
	int i = 0;
	for (int x = xs0 - 2; x <= xs1 + 2; x++, i++) {
		const int h = a->h[i % a->n];
		const int tx = in_grid(x, 0) ? top[x - DECK_PET_GRID_X0] : 999;
		const int tl = in_grid(x - 1, 0) ? top[x - 1 - DECK_PET_GRID_X0] : 999;
		const int tr = in_grid(x + 1, 0) ? top[x + 1 - DECK_PET_GRID_X0] : 999;
		const int t = (tx != 999 ? tx : tl != 999 ? tl : tr != 999 ? tr : ys0) - 2;
		for (int k = 0; k < h; k++) bit_set(fl, x, t - k);
	}
	for (int y = DECK_PET_GRID_Y0; y < DECK_PET_GRID_Y0 + DECK_PET_GRID_H; y++)
		for (int x = DECK_PET_GRID_X0; x < DECK_PET_GRID_X0 + DECK_PET_GRID_W; x++)
			if (bit_at(fl, x, y) && !get(base_g, x, y) && !bit_at(gap, x, y))
				bit_set(l->flame, x, y);
	if (l->aura == DECK_PET_AURA_LEGEND) {
		const int st[5][2] = {{-2, ys0 - 2}, {17, ys0}, {-3, 7}, {18, 8}, {8, ys0 - 7}};
		for (int s = 0; s < 5; s++) {
			bit_set(l->star, st[s][0], st[s][1]);
			bit_set(l->star, st[s][0] - 1, st[s][1]);
			bit_set(l->star, st[s][0] + 1, st[s][1]);
			bit_set(l->star, st[s][0], st[s][1] - 1);
			bit_set(l->star, st[s][0], st[s][1] + 1);
		}
	}
}

void deck_pet_look(deck_pet_look_t *look, const deck_pet_t *pet, const deck_pet_opts_t *opts)
{
	static const deck_pet_opts_t none = {0, 0, false};
	if (!opts)
		opts = &none;
	memset(look, 0, sizeof *look);
	memset(base_g, 0, sizeof base_g);
	memset(hat_g, 0, sizeof hat_g);
	const uint8_t stage = pet ? pet->stage : (uint8_t)DECK_PET_EGG;
	const uint8_t mood = pet ? pet->mood : (uint8_t)DECK_PET_FED;
	const bool egg = stage == DECK_PET_EGG;
	const bool weak = mood == DECK_PET_WEAK && !egg;
	const bool asleep = mood == DECK_PET_ASLEEP;
	const bool hungry = mood == DECK_PET_HUNGRY && !asleep && !egg;
	uint8_t art = egg ? (pet && pet->cracked ? DECK_PET_ART_CRACK : DECK_PET_ART_EGG)
	            : stage == DECK_PET_BABY ? DECK_PET_ART_BABY
	            : stage == DECK_PET_KID  ? DECK_PET_ART_KID
	                                     : DECK_PET_ART_BUILDER;
	if (weak && art == DECK_PET_ART_BUILDER)
		art = DECK_PET_ART_WEAK;
	look->art = art;
	look->weak = weak;
	look->ice = asleep;
	look->ready = egg && pet && pet->ready;
	if (!weak && !asleep && stage >= DECK_PET_STRONG && stage <= DECK_PET_LEGEND)
		look->aura = (uint8_t)(DECK_PET_AURA_STRONG + (stage - DECK_PET_STRONG));
	const uint8_t own = pet && pet->hat ? pet->hat_color % DECK_PET_HAT_COLOR_COUNT : 0;
	look->hat_rgb = hat_rgb[own][0];
	look->hat_shade_rgb = hat_rgb[own][1];
	look->spot_rgb = opts->hat && opts->hat_color < DECK_PET_HAT_COLOR_COUNT ? hat_rgb[opts->hat_color][0] : hat_rgb[own][0];

	/* the pet (pets.js petLook) */
	for (int y = 0; y < ROWS; y++)
		for (int x = 0; x < 16; x++)
			if (art_rows[art][y][x] != '.')
				set(base_g, x, y, art_rows[art][y][x]);
	if (!egg && art != DECK_PET_ART_WEAK) {
		if (hungry) {
			static const int8_t mouth[][2] = {{0, 0}, {0, 0}, {7, 9}, {7, 9}, {7, 7}};
			for (int i = 0; i < 4; i++) set(base_g, mouth[art][0] + i % 2, mouth[art][1] + i / 2, 'm');
		} else if (asleep || weak || opts->idle) {
			shut_eyes(base_g);
		}
	}
	if (!egg && art != DECK_PET_ART_BABY && pet)
		put_hat(hat_g, art, pet->hat);
	for (int y = DECK_PET_GRID_Y0; y < DECK_PET_GRID_Y0 + DECK_PET_GRID_H; y++)
		for (int x = DECK_PET_GRID_X0; x < DECK_PET_GRID_X0 + DECK_PET_GRID_W; x++)
			if (hget(hat_g, x, y)) {
				set(base_g, x, y, hget(hat_g, x, y));
				bit_set(look->hat_cells, x, y);
			}
	memcpy(look->cell, base_g, sizeof look->cell);

	/* the ice cube round it, the flames */
	int bx0 = 999, by0 = 999, bx1 = -999, by1 = -999;
	for (int y = DECK_PET_GRID_Y0; y < DECK_PET_GRID_Y0 + DECK_PET_GRID_H; y++)
		for (int x = DECK_PET_GRID_X0; x < DECK_PET_GRID_X0 + DECK_PET_GRID_W; x++)
			if (get(base_g, x, y)) {
				if (x < bx0) bx0 = x;
				if (x > bx1) bx1 = x;
				if (y < by0) by0 = y;
				if (y > by1) by1 = y;
			}
	look->ice_x0 = (int8_t)(bx0 - 1), look->ice_x1 = (int8_t)(bx1 + 1);
	look->ice_y0 = (int8_t)(by0 - 1), look->ice_y1 = (int8_t)(by1 + 1);
	if (look->aura)
		flames(look);

	/* The stroke: a cell round everything drawn, four ways, under it. */
	for (int y = DECK_PET_GRID_Y0; y < DECK_PET_GRID_Y0 + DECK_PET_GRID_H; y++)
		for (int x = DECK_PET_GRID_X0; x < DECK_PET_GRID_X0 + DECK_PET_GRID_W; x++) {
			if (deck_pet_at(look, x, y)) continue;
			if (deck_pet_at(look, x + 1, y) || deck_pet_at(look, x - 1, y) || deck_pet_at(look, x, y + 1) ||
			    deck_pet_at(look, x, y - 1))
				bit_set(look->stroke, x, y);
		}
	/* what it reaches */
	look->x0 = look->y0 = 127;
	look->x1 = look->y1 = -128;
	for (int y = DECK_PET_GRID_Y0; y < DECK_PET_GRID_Y0 + DECK_PET_GRID_H; y++)
		for (int x = DECK_PET_GRID_X0; x < DECK_PET_GRID_X0 + DECK_PET_GRID_W; x++)
			if (deck_pet_at(look, x, y) || bit_at(look->stroke, x, y) || bit_at(look->flame, x, y) ||
			    bit_at(look->star, x, y)) {
				if (x < look->x0) look->x0 = (int8_t)x;
				if (x > look->x1) look->x1 = (int8_t)x;
				if (y < look->y0) look->y0 = (int8_t)y;
				if (y > look->y1) look->y1 = (int8_t)y;
			}
	if (look->ice) { /* the cube and its frost */
		if (look->ice_x0 - 1 < look->x0) look->x0 = (int8_t)(look->ice_x0 - 1);
		if (look->ice_x1 + 1 > look->x1) look->x1 = (int8_t)(look->ice_x1 + 1);
		if (look->ice_y0 < look->y0) look->y0 = look->ice_y0;
		if (look->ice_y1 + 1 > look->y1) look->y1 = (int8_t)(look->ice_y1 + 1);
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
	case 'k':
	case 'K': return 0x0A0A0A;
	case 'g': return 0x55524B;
	case 'p': return 0xE8E4DC;
	case 'm': return 0xBDB8AD; /* light grey, so it reads on a dark tile */
	case 'c': return 0xF7F4EC;
	case 's': return look->spot_rgb;
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
} draw_t;

static void cells(const draw_t *d, int cx, int cy, int n, uint32_t rgb, uint8_t alpha)
{
	d->fill(d->user, d->x + cx * d->s, d->y + cy * d->s, n * d->s, d->s, rgb, alpha);
}

/* Runs of one colour along each row of the cells `pick` colours. */
typedef bool (*pick_fn)(const deck_pet_look_t *l, int x, int y, uint32_t *rgb);

static void runs(const deck_pet_look_t *l, const draw_t *d, pick_fn pick)
{
	for (int y = l->y0; y <= l->y1; y++) {
		int start = 0, n = 0;
		uint32_t run_rgb = 0;
		for (int x = l->x0; x <= l->x1 + 1; x++) {
			uint32_t rgb = 0;
			const bool in = x <= l->x1 && pick(l, x, y, &rgb);
			if (n && (!in || rgb != run_rgb)) {
				cells(d, start, y, n, run_rgb, 255);
				n = 0;
			}
			if (in) {
				if (!n) {
					start = x;
					run_rgb = rgb;
				}
				n++;
			}
		}
	}
}

static bool pick_flame(const deck_pet_look_t *l, int x, int y, uint32_t *rgb)
{
	*rgb = auras[l->aura].rgb;
	return bit_at(l->flame, x, y);
}

static bool pick_stroke(const deck_pet_look_t *l, int x, int y, uint32_t *rgb)
{
	*rgb = DECK_PET_STROKE_RGB;
	return bit_at(l->stroke, x, y);
}

static bool pick_cell(const deck_pet_look_t *l, int x, int y, uint32_t *rgb)
{
	const char k = deck_pet_at(l, x, y);
	*rgb = deck_pet_rgb(l, k);
	return k != 0;
}

static bool pick_star(const deck_pet_look_t *l, int x, int y, uint32_t *rgb)
{
	*rgb = STAR_RGB;
	return bit_at(l->star, x, y);
}

void deck_pet_draw(const deck_pet_look_t *l, int x, int y, int scale, deck_pet_fill_fn fill, void *user)
{
	if (!l || !fill || scale < 1 || l->x1 < l->x0)
		return;
	const draw_t d = {fill, user, x, y, scale};
	const int x0 = l->ice_x0, x1 = l->ice_x1, y0 = l->ice_y0, y1 = l->ice_y1;
	if (l->aura)
		runs(l, &d, pick_flame);
	if (l->ice)
		cells(&d, x0 - 1, y1 + 1, x1 - x0 + 3, ICE_EDGE, 115); /* the frost under it */
	runs(l, &d, pick_stroke);
	runs(l, &d, pick_cell);
	if (l->ice) {
		fill(user, x + x0 * scale, y + y0 * scale, (x1 - x0 + 1) * scale, (y1 - y0 + 1) * scale, ICE, 102);
		cells(&d, x0, y0, x1 - x0 + 1, ICE_EDGE, 230);
		cells(&d, x0, y1, x1 - x0 + 1, ICE_EDGE, 230);
		for (int cy = y0 + 1; cy < y1; cy++) {
			cells(&d, x0, cy, 1, ICE_EDGE, 230);
			cells(&d, x1, cy, 1, ICE_EDGE, 230);
		}
		for (int i = 0; i < 3; i++) /* the shine */
			cells(&d, x0 + 1 + i, y0 + 1 + i, 1, 0xFFFFFF, 204);
	}
	if (l->aura == DECK_PET_AURA_LEGEND)
		runs(l, &d, pick_star);
}

void deck_pet_bounds(const deck_pet_look_t *l, int scale, int *bx, int *by, int *bw, int *bh)
{
	*bx = l->x0 * scale;
	*by = l->y0 * scale;
	*bw = (l->x1 - l->x0 + 1) * scale;
	*bh = (l->y1 - l->y0 + 1) * scale;
}

void deck_pet_motion(const deck_pet_look_t *l, uint32_t ms, int scale, int *dx, int *dy)
{
	const int half = scale / 2 > 0 ? scale / 2 : 1;
	*dx = *dy = 0;
	if (l->ready) { /* rock, rock, then rest: 1.6 s */
		static const int8_t rock[8] = {0, 1, 0, -1, 0, 0, 0, 0};
		*dx = rock[ms % 1600 / 200] * half;
	}
}
