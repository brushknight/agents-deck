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

/* The deck's hats (deck-core.js HAT_ART: the critter's frames; sprites.h
 * kHats), the still one and the ones a working hat cycles through, ms each.
 * a = hat colour, b = shade, c = highlight, d = near-black. */
typedef struct {
	int8_t x, top;
	const char *rows[5];
	struct { int8_t x, y; char key; } extras[5];
} hat_frame_t;
typedef struct {
	uint16_t ms;
	uint8_t frames;
	hat_frame_t work[4];
} pet_hat_t;
static const pet_hat_t hats[13] = {
	{400, 2, {{0, -4, {"....aaaaa.....", "...acaaaaa....", "..aaaaccaaa...", ".bbbbbbbbbbbb."}, {{0, 0, 0}}},
	   {0, -4, {"....aaaaa.....", "...acaaaaa....", "..aaaabbaaa...", ".bbbbbbbbbbbb."}, {{0, 0, 0}}}}}, /* hard hat */
	{300, 4, {{-3, -3, {"........aaaaaa", ".......aaaaacaa", "bbbbbbbbbbbbbbb"}, {{0, 0, 0}}},
	   {-3, -3, {"........aaaaaa", ".......aaaaacaa", "bbbbbbbbbbbbbbb"}, {{0, 0, 0}}},
	   {-3, -3, {"........aaaaaa", ".......aaaaacaa", "...bbbbbbbbbbbb"}, {{-3, 0, 'b'}, {-2, 0, 'b'}}},
	   {-3, -3, {"........aaaaaa", ".......aaaaacaa", "bbbbbbbbbbbbbbb"}, {{0, 0, 0}}}}}, /* cap */
	{350, 2, {{0, -4, {".....cc.......", "....aaaaaa....", "...aaaaaaaa...", "..babababab..."}, {{0, 0, 0}}},
	   {0, -3, {"....aaaaaa....", "...aaaaaaaa...", "..babababab..."}, {{5, -5, 'c'}, {6, -5, 'c'}}}}}, /* beanie */
	{280, 4, {{0, -4, {"....aaaaaa....", "...aaaaaaaa...", "..acaaaaaaaa..", "..bbbbbbbbbb.."}, {{0, 0, 0}}},
	   {0, -4, {"....aaaaaa....", "...aaaaaaaa...", "..aaaaacaaaa..", "..bbbbbbbbbb.."}, {{0, 0, 0}}},
	   {0, -4, {"....aaaaaa....", "...aaaaaaaa...", "..aaaaaaaaca..", "..bbbbbbbbbb.."}, {{0, 0, 0}}},
	   {0, -4, {"....aaaaaa....", "...aaaaaaaa...", "..aaaaaaaaaa..", "..bbbbbbbbbb.."}, {{0, 0, 0}}}}}, /* helmet */
	{300, 4, {{0, -5, {"........a.....", ".......aa.....", "......aaca....", ".....aaaaaa...", "..bbbbbbbbbb.."}, {{0, 0, 0}}},
	   {0, -5, {".........a....", ".......aa.....", "......aaca....", ".....aaaaaa...", "..bbbbbbbbbb.."}, {{11, -6, 'c'}}},
	   {0, -5, {"........a.....", ".......aa.....", "......aaca....", ".....aaaaaa...", "..bbbbbbbbbb.."}, {{0, 0, 0}}},
	   {0, -5, {".......a......", ".......aa.....", "......aaca....", ".....aaaaaa...", "..bbbbbbbbbb.."}, {{0, 0, 0}}}}}, /* wizard */
	{300, 4, {{0, -4, {"......b.......", "...aaaaaaaa...", "..aaaaaaaaaaa.", "...bbbbbbbb..."}, {{0, 0, 0}}},
	   {0, -4, {".......b......", "...aaaaaaaa...", "..aaaaaaaaaaa.", "...bbbbbbbb..."}, {{0, 0, 0}}},
	   {0, -4, {"......b.......", "...aaaaaaaa...", "..aaaaaaaaaaa.", "...bbbbbbbb..."}, {{0, 0, 0}}},
	   {0, -4, {".....b........", "...aaaaaaaa...", "..aaaaaaaaaaa.", "...bbbbbbbb..."}, {{0, 0, 0}}}}}, /* beret */
	{450, 2, {{0, -4, {"...cc.cc.cc...", "..cccccccccc..", "...cccccccc...", "...aaaaaaaa..."}, {{0, 0, 0}}},
	   {0, -5, {"....cccccc....", "..cccccccccc..", "..cccccccccc..", "...cccccccc...", "...aaaaaaaa..."}, {{0, 0, 0}}}}}, /* chef */
	{280, 4, {{0, -4, {"...c..aa..a...", "...aa.aa.aa...", "...aaaaaaaa...", "...acaacaaca.."}, {{0, 0, 0}}},
	   {0, -4, {"...a..ca..a...", "...aa.aa.aa...", "...aaaaaaaa...", "...acaacaaca.."}, {{0, 0, 0}}},
	   {0, -4, {"...a..ac..a...", "...aa.aa.aa...", "...aaaaaaaa...", "...acaacaaca.."}, {{0, 0, 0}}},
	   {0, -4, {"...a..aa..a...", "...aa.aa.aa...", "...aaaaaaaa...", "...acaacaaca.."}, {{0, 0, 0}}}}}, /* crown */
	{320, 4, {{0, -3, {"....aaaaaa....", "...a......a...", "..a........a.."}, {{-2, -1, 'c'}, {-1, -2, 'c'}}},
	   {0, -3, {"....aaaaaa....", "...a......a...", "..a........a.."}, {{-2, -2, 'c'}, {-1, -3, 'c'}}},
	   {0, -3, {"....aaaaaa....", "...a......a...", "..a........a.."}, {{-3, -3, 'c'}, {-2, -4, 'c'}, {-1, -1, 'c'}}},
	   {0, -3, {"....aaaaaa....", "...a......a...", "..a........a.."}, {{-2, -2, 'c'}}}}}, /* headphones */
	{120, 4, {{0, -4, {"..cccc.aaaa...", ".......d......", "....ababab....", "...aaaaaaaa..."}, {{0, 0, 0}}},
	   {0, -4, {"....cc.aa.....", ".......d......", "....ababab....", "...aaaaaaaa..."}, {{0, 0, 0}}},
	   {0, -4, {"..aaaa.cccc...", ".......d......", "....ababab....", "...aaaaaaaa..."}, {{0, 0, 0}}},
	   {0, -4, {"....aa.cc.....", ".......d......", "....ababab....", "...aaaaaaaa..."}, {{0, 0, 0}}}}}, /* propeller */
	{450, 4, {{0, -5, {"...aaaaaaaa...", "...aaaaaaaa...", "...aaaaaaaa...", "...bbbbbbbb...", ".aaaaaaaaaaaa."}, {{0, 0, 0}}},
	   {0, -5, {"...aaaaaaaa...", "...aaaaaaaa...", "...aaaaaaaa...", "...bbbbbbbb...", ".aaaaaaaaaaaa."}, {{0, 0, 0}}},
	   {0, -6, {"...aaaaaaaa...", "...aaaaaaaa...", "...aaaaaaaa...", "...bbbbbbbb...", ".aaaaaaaaaaaa."}, {{0, 0, 0}}},
	   {0, -5, {"...aaaaaaaa...", "...aaaaaaaa...", "...aaaaaaaa...", "...bbbbbbbb...", ".aaaaaaaaaaaa."}, {{0, 0, 0}}}}}, /* top hat */
	{350, 4, {{0, -4, {".....aaaa.....", "....aabbaa....", "b...aaaaaa...b", "bbbbbbbbbbbbbb"}, {{0, 0, 0}}},
	   {0, -4, {".....aaaa.....", "b...aabbaa....", "b...aaaaaa....", ".bbbbbbbbbbbbb"}, {{0, 0, 0}}},
	   {0, -4, {".....aaaa.....", "....aabbaa....", "b...aaaaaa...b", "bbbbbbbbbbbbbb"}, {{0, 0, 0}}},
	   {0, -4, {".....aaaa.....", "....aabbaa...b", "....aaaaaa...b", "bbbbbbbbbbbbb."}, {{0, 0, 0}}}}}, /* cowboy */
	{200, 3, {{0, -1, {"..aaaaaaaaaa..", "..acacacacac.."}, {{1, -1, 'b'}, {0, -2, 'b'}, {0, 0, 'b'}, {-1, 0, 'b'}}},
	   {0, -1, {"..aaaaaaaaaa..", "..acacacacac.."}, {{1, -1, 'b'}, {0, -1, 'b'}, {-1, -2, 'b'}, {0, 0, 'b'}, {-1, 1, 'b'}}},
	   {0, -1, {"..aaaaaaaaaa..", "..acacacacac.."}, {{1, -1, 'b'}, {0, -2, 'b'}, {-1, -2, 'b'}, {0, 0, 'b'}, {-1, 0, 'b'}}}}}, /* bandana */
};
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

/* ---- the rig (docs/pet-poses.md, pet-poses.js RIG) ------------------------- */

typedef struct {
	bool eyes_on;
	int8_t eyes[2][4];   /* x, y, w, h: left, right */
	int8_t mouth[2];
	bool arms_on;
	int8_t arms[3];      /* the top row of an arm down, mid, up */
	uint8_t nfeet;       /* feet rows */
	int8_t feet[2];
	int8_t feet_x[4], feet_step[4];
	int8_t bubble[2], tow[2];
} rig_t;
static const rig_t rigs[] = {
	{false, {{0}}, {0, 0}, false, {0}, 0, {0}, {0}, {0}, {14, -4}, {16, 11}},  /* egg */
	{false, {{0}}, {0, 0}, false, {0}, 0, {0}, {0}, {0}, {14, -4}, {16, 11}},  /* crack */
	{true, {{5, 7, 1, 2}, {10, 7, 1, 2}}, {7, 9}, true, {9, 7, 3}, 1, {12, 0}, {4, 6, 9, 11}, {5, 7, 9, 11}, {13, -1}, {14, 10}},
	{true, {{5, 7, 1, 2}, {10, 7, 1, 2}}, {7, 9}, true, {9, 7, 4}, 2, {11, 12}, {4, 6, 9, 11}, {3, 5, 10, 12}, {14, 0}, {14, 10}},
	{true, {{4, 5, 1, 2}, {10, 5, 1, 2}}, {7, 7}, true, {7, 5, 2}, 2, {10, 11}, {2, 4, 11, 13}, {3, 5, 10, 12}, {16, -2}, {16, 9}},
	{true, {{3, 7, 2, 1}, {9, 7, 2, 1}}, {6, 10}, true, {9, 7, 4}, 1, {11, 0}, {2, 4, 11, 13}, {3, 5, 10, 12}, {15, 0}, {17, 9}},
};

/* ---- motion (the critter's timing classes, in whole cells) ----------------- */

enum {
	M_NONE, M_LA, M_LB, M_LA_FAST, M_LB_FAST, M_LA_SLOW, M_LB_SLOW, M_JOG, M_JOG_FAST, M_HOP, M_ROCK,
	M_ROCK2, M_GLANCE, M_SCAN, M_T2, M_T3, M_CURSOR, M_TICK0, M_TICK1, M_TICK2, M_DOTS, M_PRESS2,
	M_PRESS1, M_BOBC, M_BLINK, M_ZZ, M_ZZ2, M_PSQ, M_PSQ1,
};

/* Where a class puts its cells at ms; false while it is hidden (or faint). */
static bool motion(int m, uint32_t ms, int *dx, int *dy)
{
	*dx = *dy = 0;
	switch (m) {
	case M_LA: return ms % 500 < 250;
	case M_LB: return ms % 500 >= 250;
	case M_LA_FAST: return ms % 200 < 100;
	case M_LB_FAST: return ms % 200 >= 100;
	case M_LA_SLOW: return ms % 800 < 400;
	case M_LB_SLOW: return ms % 800 >= 400;
	case M_JOG: *dy = ms % 500 >= 250 ? -1 : 0; return true;      /* the whole pet lifts a cell */
	case M_JOG_FAST: *dy = ms % 400 >= 200 ? -1 : 0; return true; /* bash and write's beat */
	case M_HOP: *dy = ms % 400 >= 200 ? -2 : 0; return true;
	case M_ROCK:
	case M_ROCK2:
	case M_SCAN: {
		static const int8_t q[4] = {0, -1, 0, 1};
		*dx = q[ms % (m == M_ROCK2 ? 600u : 1200u) * 4 / (m == M_ROCK2 ? 600u : 1200u)];
		return true;
	}
	case M_GLANCE: *dx = ms % 1200 < 600 ? -1 : 1; return true;
	case M_T2: return ms % 1200 >= 300;
	case M_T3: return ms % 1200 >= 600;
	case M_CURSOR: return ms % 1000 < 500;
	case M_TICK0: return ms % 2000 < 500;
	case M_TICK1: return ms % 2000 < 1000;
	case M_TICK2: return ms % 2000 < 1500;
	case M_DOTS: return ms % 1200 >= 400 && ms % 1200 < 800;
	case M_PRESS2: return ms % 1200 < 400;
	case M_PRESS1: return ms % 1200 < 800;
	case M_BOBC: *dy = ms % 600 >= 300 ? 1 : 0; return true;
	case M_BLINK: return ms % 1000 < 600;
	case M_ZZ: *dy = ms % 2400 >= 1200 ? -1 : 0; return true;
	case M_ZZ2: *dy = (ms + 1200) % 2400 >= 1200 ? -1 : 0; return true;
	case M_PSQ: *dy = (int)(ms % 1200 / 400); return true;
	case M_PSQ1: *dy = ms % 1200 >= 400 ? 1 : 0; return true;
	default: return true;
	}
}

/* ---- the poses (pet-poses.js POSES) ----------------------------------------- */

enum { E_KEEP, E_UP, E_GLANCE, E_SCAN, E_SHUT, E_HAPPY, E_X, E_STRAIN, E_DOT };
enum { A_NONE, A_DOWN, A_MID, A_UP, A_SWING };
enum { W_NONE, W_STEP, W_FAST };
enum { BB_NONE, BB_THINK, BB_READ, BB_BASH, BB_WEB, BB_PLAN, BB_DOTS, BB_PRESS, BB_COOKIE, BB_ASK, BB_BANG, BB_ZZ, BB_SPARKS };
typedef struct {
	uint8_t eyes, arm_l, arm_r, walk, grp, bubble;
	bool tow, squash, flag, chomp, hollow;
} pose_t;
static const pose_t poses[] = {
	{E_KEEP, A_NONE, A_NONE, W_NONE, M_NONE, BB_NONE, false, false, false, false, false},     /* none */
	{E_GLANCE, A_DOWN, A_MID, W_NONE, M_NONE, BB_THINK, false, false, false, false, false},   /* think */
	{E_SCAN, A_NONE, A_NONE, W_NONE, M_NONE, BB_READ, false, false, false, false, false},     /* read */
	{E_KEEP, A_NONE, A_NONE, W_FAST, M_JOG_FAST, BB_BASH, false, false, false, false, false}, /* write */
	{E_KEEP, A_NONE, A_NONE, W_FAST, M_JOG_FAST, BB_BASH, false, false, false, false, false}, /* bash */
	{E_UP, A_NONE, A_NONE, W_NONE, M_NONE, BB_WEB, false, false, false, false, false},        /* web */
	{E_KEEP, A_NONE, A_NONE, W_NONE, M_NONE, BB_PLAN, false, false, false, false, false},     /* plan */
	{E_KEEP, A_SWING, A_SWING, W_STEP, M_JOG, BB_NONE, true, false, false, false, false},     /* delegate */
	{E_STRAIN, A_NONE, A_NONE, W_NONE, M_NONE, BB_PRESS, false, true, false, false, false},   /* compact */
	{E_KEEP, A_SWING, A_SWING, W_STEP, M_JOG, BB_DOTS, false, false, false, false, false},    /* run */
	{E_UP, A_NONE, A_NONE, W_NONE, M_NONE, BB_ASK, false, false, false, false, false},        /* wait */
	{E_X, A_UP, A_UP, W_NONE, M_NONE, BB_BANG, false, false, false, false, false},            /* err */
	{E_SHUT, A_NONE, A_NONE, W_NONE, M_NONE, BB_ZZ, false, false, false, false, false},       /* idle */
	{E_KEEP, A_NONE, A_NONE, W_NONE, M_NONE, BB_DOTS, false, false, false, false, false},     /* start */
	{E_KEEP, A_NONE, A_NONE, W_NONE, M_NONE, BB_NONE, false, false, false, false, true},      /* exit */
	{E_HAPPY, A_UP, A_UP, W_NONE, M_HOP, BB_SPARKS, false, false, true, false, false},        /* celebrate */
	{E_UP, A_NONE, A_NONE, W_NONE, M_NONE, BB_COOKIE, false, false, false, true, false},      /* hungry */
};

/* Working poses: an egg rocks through them, a working hat cycles its frames
 * (compact too, but the egg squeezes then). */
static bool working(uint8_t pose)
{
	return pose >= DECK_PET_POSE_THINK && pose <= DECK_PET_POSE_RUN;
}

/* The bubbles (pet-poses.js BUBBLES, deck-core.js GLYPH): rows, offset in the
 * bubble box, motion, key. */
typedef struct { const char *const *rows; uint8_t n; int8_t x, y; uint8_t m; char key; } glyph_t;
static const char *const G_DOT[] = {"#"};
static const char *const G_SQ[] = {"##", "##"};
static const char *const G_MAG[] = {".##..", "#..#.", "#..#.", ".##..", "....#"};
static const char *const G_PROMPT[] = {"#..", ".#.", "#.."};
static const char *const G_CURSOR[] = {"##"};
static const char *const G_GLOBE[] = {".###.", "#.#.#", "#####", "#.#.#", ".###."};
static const char *const G_GLOBE2[] = {".###.", "##.##", "#####", "##.##", ".###."};
static const char *const G_LIST[] = {"#.###"};
static const char *const G_DOTS[] = {"#.#.#"};
static const char *const G_BAR[] = {"####"};
static const char *const G_COOKIE[] = {".###.", "##.##", "#####", "#.###", ".###."};
static const char *const G_ASK[] = {"###", "..#", ".#.", "...", ".#."};
static const char *const G_BANG[] = {"#", "#", "#", ".", "#"};
static const char *const G_Z4[] = {"####", "..#.", ".#..", "####"};
static const char *const G_Z3[] = {"###", ".#.", "###"};
static const char *const G_PLUS[] = {".#.", "###", ".#."};
#define GL(rows) rows, (uint8_t)(sizeof rows / sizeof rows[0])
static const glyph_t bubble_think[] = {{GL(G_DOT), 0, 4, M_NONE, 'P'}, {GL(G_DOT), 1, 2, M_T2, 'P'}, {GL(G_SQ), 3, 0, M_T3, 'P'}};
static const glyph_t bubble_read[] = {{GL(G_MAG), 0, 0, M_NONE, 'P'}};
static const glyph_t bubble_bash[] = {{GL(G_PROMPT), 0, 1, M_NONE, 'P'}, {GL(G_CURSOR), 3, 3, M_CURSOR, 'P'}};
static const glyph_t bubble_web[] = {{GL(G_GLOBE), 0, 0, M_LA_SLOW, 'P'}, {GL(G_GLOBE2), 0, 0, M_LB_SLOW, 'P'}};
static const glyph_t bubble_plan[] = {
	{GL(G_LIST), 0, 0, M_NONE, 'P'}, {GL(G_DOT), 0, 0, M_TICK0, 'B'}, {GL(G_LIST), 0, 2, M_NONE, 'P'},
	{GL(G_DOT), 0, 2, M_TICK1, 'B'}, {GL(G_LIST), 0, 4, M_NONE, 'P'}, {GL(G_DOT), 0, 4, M_TICK2, 'B'},
};
static const glyph_t bubble_dots[] = {{GL(G_DOTS), 0, 4, M_DOTS, 'P'}};
static const glyph_t bubble_press[] = {{GL(G_BAR), 0, 0, M_PRESS2, 'P'}, {GL(G_BAR), 0, 2, M_PRESS1, 'P'}, {GL(G_BAR), 0, 4, M_NONE, 'P'}};
static const glyph_t bubble_cookie[] = {{GL(G_COOKIE), 0, 0, M_BOBC, 'P'}};
static const glyph_t bubble_ask[] = {{GL(G_ASK), 1, 0, M_BLINK, 'P'}};
static const glyph_t bubble_bang[] = {{GL(G_BANG), 2, 0, M_NONE, 'P'}};
static const glyph_t bubble_zz[] = {{GL(G_Z4), 0, 1, M_ZZ, 'P'}, {GL(G_Z3), 4, -2, M_ZZ2, 'P'}};
static const glyph_t bubble_sparks[] = {{GL(G_PLUS), 0, 2, M_NONE, 'P'}, {GL(G_PLUS), 2, 0, M_NONE, 'P'}};
static const glyph_t bubble_flag_sparks[] = {{GL(G_PLUS), 5, -4, M_NONE, 'P'}, {GL(G_PLUS), 3, -8, M_NONE, 'P'}};
#define BL(list) list, (uint8_t)(sizeof list / sizeof list[0])
static const struct { const glyph_t *g; uint8_t n; } bubbles[] = {
	{NULL, 0}, {BL(bubble_think)}, {BL(bubble_read)}, {BL(bubble_bash)}, {BL(bubble_web)}, {BL(bubble_plan)},
	{BL(bubble_dots)}, {BL(bubble_press)}, {BL(bubble_cookie)}, {BL(bubble_ask)}, {BL(bubble_bang)},
	{BL(bubble_zz)}, {BL(bubble_sparks)},
};

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

/* ---- posing ---------------------------------------------------------------------- */

/* Scratch for one look: the still pet (with its hat), and the posed body. */
static grid_t base_g, pix_g;
static hgrid_t frame_g[4];

typedef struct {
	deck_pet_look_t *l;
	uint32_t ms;
	int gdx, gdy;   /* the body's motion */
	int split;      /* squeezed: rows above it drop with sq */
	bool squash;
	int sq;         /* M_PSQ (two rows), or M_PSQ1 (the kid's one) */
} pen_t;

#define NO_SPLIT (-1000)

/* A cell of the body group: the body's motion, the squeeze above the split, its own. */
static void body_cell(const pen_t *p, int x, int y, char key, int m, bool top)
{
	int dx, dy, sx = 0, sy = 0;
	if (!motion(m, p->ms, &dx, &dy))
		return;
	if (top)
		motion(p->sq, p->ms, &sx, &sy);
	set(p->l->cell, x + dx + p->gdx + sx, y + dy + p->gdy + sy, key);
}

/* A cell outside the body group (stepping feet, bubbles, the mini pet). */
static void outer_cell(const pen_t *p, int x, int y, char key, int m)
{
	int dx, dy;
	if (motion(m, p->ms, &dx, &dy))
		set(p->l->cell, x + dx, y + dy, key);
}

static void glyph(const pen_t *p, const glyph_t *g, int bx, int by)
{
	for (int r = 0; r < g->n; r++)
		for (int c = 0; g->rows[r][c]; c++)
			if (g->rows[r][c] == '#')
				outer_cell(p, bx + g->x + c, by + g->y + r, g->key, g->m);
}

static bool body_key(char k)
{
	return k == 'b' || k == 'd' || k == 'e' || k == 'k' || k == 'w' || k == 'v' || k == 's' || k == 'm';
}

/* The arm beside the body whose top row is y: one left of the body's left
 * edge on rows y and y+1 (side 0), or one right of the right edge. */
static int arm_x(int y, int side)
{
	int l = 999, r = -999;
	for (int yy = y; yy <= y + 1; yy++)
		for (int x = DECK_PET_GRID_X0; x < DECK_PET_GRID_X0 + DECK_PET_GRID_W; x++)
			if (body_key(get(base_g, x, yy))) {
				if (x < l) l = x;
				if (x > r) r = x;
			}
	return side ? r + 1 : l - 1;
}

/* Cells of one eye [x, y, w, h] drawn as kind (pet-poses.js eyeCells); side 0 is the left. */
static int eye_cells(const int8_t *e, int kind, int side, int8_t out[][2])
{
	const int x = e[0], y = e[1], w = e[2], h = e[3], t = h >= 2 ? y : y - 1;
	int n = 0;
#define C(cx, cy) (out[n][0] = (int8_t)(cx), out[n][1] = (int8_t)(cy), n++)
	switch (kind) {
	case E_UP:
	case E_GLANCE:
		for (int j = 0; j < h; j++)
			for (int i = 0; i < w; i++) C(x + i, y + j - 1);
		break;
	case E_SHUT:
		for (int i = 0; i <= w; i++) C((side ? x : x - 1) + i, y + h - 1);
		break;
	case E_HAPPY:
		C(x - 1, t + 1);
		for (int i = 0; i < w; i++) C(x + i, t);
		C(x + w, t + 1);
		break;
	case E_X:
		C(x - 1, t), C(x + w, t);
		for (int i = 0; i < w; i++) C(x + i, t + 1);
		C(x - 1, t + 2), C(x + w, t + 2);
		break;
	case E_STRAIN: {
		const int ex = side ? x + w : x - 1;
		C(ex, t);
		for (int i = 0; i < w; i++) C(x + i, t + 1);
		C(ex, t + 2);
		break;
	}
	case E_DOT: C(side ? x : x + w - 1, y + h - 1); break;
	default:
		for (int j = 0; j < h; j++)
			for (int i = 0; i < w; i++) C(x + i, y + j);
	}
#undef C
	return n;
}

/* The flames round a strong, mighty or legend pet (pets.js auraSVG), from
 * the still pet in base_g, moved with the pet's group (dx, dy) at ms: the
 * still layer, the second layer for the second half of 1.8 s, the sparks
 * rising a cell a step and the legend's stars twinkling. */
static void flames(deck_pet_look_t *l, uint32_t ms, int dx, int dy)
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
	for (int f = 0; f < 2; f++) {
		if (f && ms % 1800 < 900) /* the second layer: off, then on */
			break;
		for (int y = ys0; y <= ys1; y++) {
			const int iy = y - DECK_PET_GRID_Y0;
			if (sr[iy] < sl[iy]) continue;
			const int w = 2 * y <= mid2 ? 2 : 1;
			for (int k = 2; k < 2 + w; k++)
				if ((y + f) % 4 != 0 || k == 2) {
					bit_set(fl, sl[iy] - k, y);
					bit_set(fl, sr[iy] + k, y);
				}
		}
		int i = 0;
		for (int x = xs0 - 2; x <= xs1 + 2; x++, i++) {
			const int h = a->h[(i + f * 3) % a->n];
			const int tx = in_grid(x, 0) ? top[x - DECK_PET_GRID_X0] : 999;
			const int tl = in_grid(x - 1, 0) ? top[x - 1 - DECK_PET_GRID_X0] : 999;
			const int tr = in_grid(x + 1, 0) ? top[x + 1 - DECK_PET_GRID_X0] : 999;
			const int t = (tx != 999 ? tx : tl != 999 ? tl : tr != 999 ? tr : ys0) - 2;
			for (int k = 0; k < h; k++) bit_set(fl, x, t - k);
		}
	}
	for (int y = DECK_PET_GRID_Y0; y < DECK_PET_GRID_Y0 + DECK_PET_GRID_H; y++)
		for (int x = DECK_PET_GRID_X0; x < DECK_PET_GRID_X0 + DECK_PET_GRID_W; x++)
			if (bit_at(fl, x, y) && !get(base_g, x, y) && !bit_at(gap, x, y))
				bit_set(l->flame, x + dx, y + dy);
	/* sparks: each rises a cell a step, 9 steps in 2.4 s, shown on steps 1 to 7 */
	const int cx = (xs0 + xs1) >> 1;
	const int sx[10] = {xs0 - 2, xs1 + 2, xs0 + 2, xs1 - 2, cx, xs0 - 1, xs1 + 1, xs0 + 4, xs1 - 4, cx + 2};
	static const uint8_t nsparks[] = {0, 5, 7, 10};
	for (int s = 0; s < nsparks[l->aura]; s++) {
		const int step = (int)((ms + 530u * (uint32_t)s) % 2400u * 9u / 2400u);
		if (step >= 1 && step <= 7)
			bit_set(l->spark, sx[s % 10] + dx, ys1 - s % 3 - step + dy);
	}
	if (l->aura == DECK_PET_AURA_LEGEND) { /* a dot, then the dot and its four arms, in 1.1 s */
		const int st[5][2] = {{-2, ys0 - 2}, {17, ys0}, {-3, 7}, {18, 8}, {8, ys0 - 7}};
		for (int s = 0; s < 5; s++) {
			const int x = st[s][0] + dx, y = st[s][1] + dy;
			bit_set(l->star, x, y);
			if ((ms + 270u * (uint32_t)s) % 1100u >= 550u) {
				bit_set(l->star, x - 1, y);
				bit_set(l->star, x + 1, y);
				bit_set(l->star, x, y - 1);
				bit_set(l->star, x, y + 1);
			}
		}
	}
}

/* The darker shade of an exited pet's hat (pets.js darker, 0.72). */
static uint32_t darker(uint32_t rgb)
{
	uint32_t out = 0;
	for (int sh = 0; sh < 24; sh += 8) {
		const uint32_t v = (rgb >> sh) & 0xFF;
		out |= ((v * 72 + 50) / 100) << sh;
	}
	return out;
}

void deck_pet_look(deck_pet_look_t *look, const deck_pet_t *pet, const deck_pet_opts_t *opts)
{
	static const deck_pet_opts_t none = {0, 0, false, 0, 0, false};
	if (!opts)
		opts = &none;
	memset(look, 0, sizeof *look);
	memset(base_g, 0, sizeof base_g);
	memset(pix_g, 0, sizeof pix_g);
	const uint8_t stage = pet ? pet->stage : (uint8_t)DECK_PET_EGG;
	const uint8_t mood = pet ? pet->mood : (uint8_t)DECK_PET_FED;
	const uint8_t pose_id = opts->pose < sizeof poses / sizeof poses[0] ? opts->pose : (uint8_t)DECK_PET_POSE_NONE;
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
	if (!weak && !asleep && stage >= DECK_PET_STRONG && stage <= DECK_PET_LEGEND && pose_id != DECK_PET_POSE_EXIT)
		look->aura = (uint8_t)(DECK_PET_AURA_STRONG + (stage - DECK_PET_STRONG));
	const uint8_t own = pet && pet->hat ? pet->hat_color % DECK_PET_HAT_COLOR_COUNT : 0;
	look->hat_rgb = hat_rgb[own][0];
	look->hat_shade_rgb = hat_rgb[own][1];
	look->spot_rgb = opts->hat && opts->hat_color < DECK_PET_HAT_COLOR_COUNT ? hat_rgb[opts->hat_color][0] : hat_rgb[own][0];

	/* The still pet (petLook). */
	for (int y = 0; y < ROWS; y++)
		for (int x = 0; x < 16; x++)
			if (art_rows[art][y][x] != '.')
				set(base_g, x, y, art_rows[art][y][x]);
	if (!egg && art != DECK_PET_ART_WEAK) {
		if (hungry) {
			const int mx = rigs[art].mouth[0], my = rigs[art].mouth[1];
			for (int i = 0; i < 4; i++) set(base_g, mx + i % 2, my + i / 2, 'm');
		} else if (asleep || (pose_id == DECK_PET_POSE_NONE && (weak || opts->idle))) {
			shut_eyes(base_g);
		}
	}
	memset(frame_g, 0, sizeof frame_g); /* [0]: the still hat on its own; or the working frames */
	if (!egg && art != DECK_PET_ART_BABY && pet)
		put_hat(frame_g[0], art, pet->hat);
	memcpy(pix_g, base_g, sizeof pix_g); /* the body without the hat, for posing */
	for (int y = DECK_PET_GRID_Y0; y < DECK_PET_GRID_Y0 + DECK_PET_GRID_H; y++)
		for (int x = DECK_PET_GRID_X0; x < DECK_PET_GRID_X0 + DECK_PET_GRID_W; x++)
			if (hget(frame_g[0], x, y)) {
				set(base_g, x, y, hget(frame_g[0], x, y));
				bit_set(look->hat_cells, x, y);
			}

	/* the ice cube round the still pet, and the flames */
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

	/* The pose (pet-poses.js posePet). */
	const pose_t *sp = &poses[pose_id];
	const rig_t *rig = &rigs[art];
	const bool bubble_only = asleep && (pose_id == DECK_PET_POSE_WAIT || pose_id == DECK_PET_POSE_ERR);
	const bool posing = pose_id != DECK_PET_POSE_NONE && (!asleep || bubble_only);
	const bool awake = pose_id == DECK_PET_POSE_IDLE && hungry; /* hungry for food: no "z z" */
	const char body = art == DECK_PET_ART_WEAK ? 'd' : 'b';
	const char eye = art == DECK_PET_ART_WEAK ? 'k' : 'e';
	pen_t p = {look, opts->ms, 0, 0, NO_SPLIT, false, M_PSQ};
	int hat_frames = 0, hat_ms = 0;
	const int eye_kind = awake ? (int)E_KEEP : (int)sp->eyes;
	bool hollow = false;

	if (posing && !bubble_only && !egg) {
		/* eyes that move into place */
		if (eye_kind != E_KEEP && rig->eyes_on) {
			int8_t c[8][2];
			for (int side = 0; side < 2; side++) {
				const int n = eye_cells(rig->eyes[side], E_KEEP, side, c);
				for (int i = 0; i < n; i++) set(pix_g, c[i][0], c[i][1], body);
			}
			if (eye_kind != E_GLANCE && eye_kind != E_SCAN)
				for (int side = 0; side < 2; side++) {
					const int n = eye_cells(rig->eyes[side], eye_kind, side, c);
					for (int i = 0; i < n; i++) set(pix_g, c[i][0], c[i][1], eye);
				}
		}
		/* feet that step leave the body */
		if (sp->walk)
			for (int r = 0; r < rig->nfeet; r++)
				for (int i = 0; i < 4; i++) set(pix_g, rig->feet_x[i], rig->feet[r], 0);
		if (sp->chomp && art != DECK_PET_ART_WEAK)
			for (int i = 0; i < 4; i++) set(pix_g, rig->mouth[0] + i % 2, rig->mouth[1] + i / 2, body);
		if (sp->hollow) { /* only the outline stays (the hat is drawn on its own) */
			for (int y = 0; y < ROWS; y++)
				for (int x = 0; x < 16; x++) {
					if (!get(pix_g, x, y)) continue;
					bool inside = true;
					static const int8_t n4[4][2] = {{1, 0}, {-1, 0}, {0, 1}, {0, -1}};
					for (int k = 0; k < 4 && inside; k++) {
						const int nx = x + n4[k][0], ny = y + n4[k][1];
						inside = get(base_g, nx, ny) && !bit_at(look->hat_cells, nx, ny);
					}
					if (inside) set(pix_g, x, y, 0);
				}
			int8_t c[8][2];
			for (int side = 0; side < 2 && rig->eyes_on; side++) {
				const int n = eye_cells(rig->eyes[side], E_DOT, side, c);
				for (int i = 0; i < n; i++) set(pix_g, c[i][0], c[i][1], body);
			}
			hollow = true;
		}
	} else if (posing && !bubble_only && egg && sp->hollow) {
		for (int y = 0; y < ROWS; y++)
			for (int x = 0; x < 16; x++) {
				bool inside = get(base_g, x, y) != 0;
				static const int8_t n4[4][2] = {{1, 0}, {-1, 0}, {0, 1}, {0, -1}};
				for (int k = 0; k < 4 && inside; k++) inside = get(base_g, x + n4[k][0], y + n4[k][1]) != 0;
				if (inside) set(pix_g, x, y, 0);
			}
		hollow = true;
	}
	if (hollow) { /* the hat takes its shade */
		look->hat_rgb = look->hat_shade_rgb;
		look->hat_shade_rgb = darker(look->hat_shade_rgb);
	}
	if (posing && !bubble_only) {
		int grp = sp->grp;
		if (egg) grp = working(pose_id) && pose_id != DECK_PET_POSE_COMPACT ? (sp->walk == W_FAST ? M_ROCK2 : M_ROCK)
		             : pose_id == DECK_PET_POSE_CELEBRATE ? M_HOP : M_NONE;
		motion(grp, p.ms, &p.gdx, &p.gdy);
		if (sp->squash) {
			int feet_top = rig->nfeet ? rig->feet[0] : 0;
			if (!rig->nfeet)
				for (int y = 0; y < ROWS; y++)
					for (int x = 0; x < 16; x++)
						if (get(pix_g, x, y) && y + 1 > feet_top) feet_top = y + 1;
			/* the kid's face reaches row 9: it drops one row, the others two */
			const int squeeze = art == DECK_PET_ART_KID ? 1 : 2;
			p.split = feet_top - squeeze;
			p.squash = true;
			p.sq = squeeze == 1 ? M_PSQ1 : M_PSQ;
		}
		/* a working hat cycles through its frames */
		if (working(pose_id) && !asleep && pet && pet->hat >= 1 && pet->hat - 1 < 13 && has_hat_spot(art) &&
		    !hat_at[art].small) {
			const pet_hat_t *h = &hats[pet->hat - 1];
			hat_frames = h->frames;
			hat_ms = h->ms;
			for (int i = 0; i < h->frames; i++) {
				memset(frame_g[i], 0, sizeof frame_g[i]);
				put_frame(frame_g[i], art, pet->hat - 1, &h->work[i]);
			}
			memset(look->hat_cells, 0, sizeof look->hat_cells);
		}
	}

	if (look->aura) /* the flames, sparks and stars move with the pet */
		flames(look, opts->ms, p.gdx, p.gdy);

	/* Draw the posed body: squeezed, the rows it drops over first, then the
	 * top rows, the hat and the inner layers over them (they drop together,
	 * the face stays on top); then the outer layers. */
	const int frame = hat_frames ? (int)(p.ms / (uint32_t)hat_ms % (uint32_t)hat_frames) : 0;
	const bool top = p.squash;
	if (p.squash)
		for (int y = DECK_PET_GRID_Y0; y < DECK_PET_GRID_Y0 + DECK_PET_GRID_H; y++)
			for (int x = DECK_PET_GRID_X0; x < DECK_PET_GRID_X0 + DECK_PET_GRID_W; x++) {
				const char k = get(pix_g, x, y);
				if (k && y >= p.split)
					body_cell(&p, x, y, k, M_NONE, false);
			}
	for (int y = DECK_PET_GRID_Y0; y < DECK_PET_GRID_Y0 + DECK_PET_GRID_H; y++)
		for (int x = DECK_PET_GRID_X0; x < DECK_PET_GRID_X0 + DECK_PET_GRID_W; x++) {
			const char k = get(pix_g, x, y);
			if (k && (!p.squash || y < p.split))
				body_cell(&p, x, y, k, M_NONE, top);
		}
	memset(look->hat_cells, 0, sizeof look->hat_cells);
	{
		int sx = 0, sy = 0;
		if (top)
			motion(p.sq, p.ms, &sx, &sy);
		/* a working hat: the cells all its frames share now, the others after the inner layers */
		for (int y = DECK_PET_GRID_Y0; y < DECK_PET_GRID_Y0 + DECK_PET_GRID_H; y++)
			for (int x = DECK_PET_GRID_X0; x < DECK_PET_GRID_X0 + DECK_PET_GRID_W; x++) {
				const char k = hget(frame_g[frame], x, y);
				if (!k)
					continue;
				bit_set(look->hat_cells, x + p.gdx + sx, y + p.gdy + sy);
				bool shared = true;
				for (int i = 0; i < hat_frames && shared; i++) shared = hget(frame_g[i], x, y) == k;
				if (shared)
					body_cell(&p, x, y, k, M_NONE, top);
			}
	}
	if (posing && !bubble_only && !egg) {
		int8_t c[8][2];
		if ((eye_kind == E_GLANCE || eye_kind == E_SCAN) && rig->eyes_on)
			for (int side = 0; side < 2; side++) {
				const int n = eye_cells(rig->eyes[side], eye_kind, side, c);
				for (int i = 0; i < n; i++)
					body_cell(&p, c[i][0], c[i][1], eye, eye_kind == E_GLANCE ? M_GLANCE : M_SCAN, top);
			}
		static const uint8_t arm_row[] = {0, 0, 1, 2};
		if (rig->arms_on && sp->arm_l == A_SWING) {
			static const struct { uint8_t pos, side, m; } sw[4] = {{0, 0, M_LA}, {1, 1, M_LA}, {1, 0, M_LB}, {0, 1, M_LB}};
			for (int i = 0; i < 4; i++) {
				const int y = rig->arms[sw[i].pos], x = arm_x(y, sw[i].side);
				body_cell(&p, x, y, 'b', sw[i].m, top);
				body_cell(&p, x, y + 1, 'b', sw[i].m, top);
			}
		} else if (rig->arms_on && sp->arm_l) {
			for (int side = 0; side < 2; side++) {
				const int y = rig->arms[arm_row[side ? sp->arm_r : sp->arm_l]], x = arm_x(y, side);
				body_cell(&p, x, y, 'b', M_NONE, top);
				body_cell(&p, x, y + 1, 'b', M_NONE, top);
			}
		}
		if (sp->walk) { /* stepping feet, with the body: they never come apart from it */
			const int fa = sp->walk == W_FAST ? M_LA_FAST : M_LA, fb = sp->walk == W_FAST ? M_LB_FAST : M_LB;
			for (int r = 0; r < rig->nfeet; r++)
				for (int i = 0; i < 4; i++) {
					body_cell(&p, rig->feet_x[i], rig->feet[r], body, fa, top);
					body_cell(&p, rig->feet_step[i], rig->feet[r], body, fb, top);
				}
		}
		if (sp->chomp) {
			const int mx = rig->mouth[0], my = rig->mouth[1];
			if (art == DECK_PET_ART_WEAK) {
				for (int i = 0; i < 3; i++) body_cell(&p, mx + i, my, 'k', M_LA_SLOW, top);
			} else {
				for (int i = 0; i < 4; i++) body_cell(&p, mx + i % 2, my + i / 2, 'm', M_LA_SLOW, top);
				for (int i = 0; i < 2; i++) body_cell(&p, mx + i, my + 1, 'm', M_LB_SLOW, top);
			}
		}
		if (sp->flag) { /* the finish flag in the raised right hand */
			const int up = rig->arms[2], px = arm_x(up, 1) + 1;
			for (int i = 0; i < 6; i++) body_cell(&p, px, up - 4 + i, 'P', M_NONE, top);
			for (int ph = 0; ph < 2; ph++)
				for (int cx = 0; cx < 3; cx++)
					for (int cy = 0; cy < 2; cy++)
						body_cell(&p, px + 1 + cx, up - 4 + cy, (cx + cy + ph) % 2 ? 'k' : 'w', ph ? M_LB : M_LA, top);
		}
	}
	for (int y = DECK_PET_GRID_Y0; y < DECK_PET_GRID_Y0 + DECK_PET_GRID_H && hat_frames; y++)
		for (int x = DECK_PET_GRID_X0; x < DECK_PET_GRID_X0 + DECK_PET_GRID_W; x++) {
			const char k = hget(frame_g[frame], x, y);
			bool shared = true;
			for (int i = 0; i < hat_frames && shared; i++) shared = hget(frame_g[i], x, y) == k;
			if (k && !shared)
				body_cell(&p, x, y, k, M_NONE, top);
		}
	if (posing && !bubble_only && sp->tow) { /* the mini pet in tow (it doesn't bob: its feet would come apart) */
		static const char *const mini[] = {"#####", "#.#.#"};
		const int tx = rig->tow[0], ty = rig->tow[1];
		for (int r = 0; r < 2; r++)
			for (int c = 0; c < 5; c++)
				if (mini[r][c] == '#') outer_cell(&p, tx + c, ty + r, body, M_NONE);
		for (int c = 0; c < 5; c++) {
			if (c == 0 || c == 4) outer_cell(&p, tx + c, ty + 2, body, M_LA);
			if (c == 1 || c == 3) outer_cell(&p, tx + c, ty + 2, body, M_LB);
		}
	}
	if (posing && opts->bubble && !awake && sp->bubble) { /* the bubble, up and to the right */
		int bx = rig->bubble[0], by = rig->bubble[1];
		const glyph_t *g = bubbles[sp->bubble].g;
		int n = bubbles[sp->bubble].n;
		if (sp->bubble == BB_SPARKS && sp->flag && !egg && !bubble_only) { /* beside the flag */
			bx = arm_x(rig->arms[2], 1) + 1;
			by = rig->arms[2];
			g = bubble_flag_sparks;
			n = 2;
		}
		if (g == bubble_flag_sparks) { /* the sparks by the flag hop with it */
			for (int i = 0; i < n; i++)
				for (int r = 0; r < g[i].n; r++)
					for (int k = 0; g[i].rows[r][k]; k++)
						if (g[i].rows[r][k] == '#') body_cell(&p, bx + g[i].x + k, by + g[i].y + r, g[i].key, g[i].m, false);
		} else {
			for (int i = 0; i < n; i++) glyph(&p, &g[i], bx, by);
		}
	}

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
			    bit_at(look->star, x, y) || bit_at(look->spark, x, y)) {
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
	case 'b':
	case 'B': return look->weak ? WEAK_BODY : 0xFF4D00;
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
	case 'P': return DECK_PET_PROP_RGB;
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

static bool pick_spark(const deck_pet_look_t *l, int x, int y, uint32_t *rgb)
{
	*rgb = l->aura == DECK_PET_AURA_LEGEND ? STAR_RGB : 0xFFFFFF;
	return bit_at(l->spark, x, y);
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
	if (l->aura) /* in front: the sparks and the legend's stars */
		runs(l, &d, pick_spark);
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
	*dx = *dy = 0;
	if (l->ready) { /* still until 60 % of 1.6 s, then -1, +1, -1 cell, a tenth each */
		static const int8_t wobble[10] = {0, 0, 0, 0, 0, 0, -1, 1, -1, 0};
		*dx = wobble[ms % 1600 / 160] * scale;
	}
}
