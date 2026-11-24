/* deck_pet: an agent's pet (Agent.pet, while Agent pets is on), drawn as
 * pixel art. Optional, like deck_tiles: a board that never calls it links
 * none of it.
 *
 * The art is the web deck's (backend/internal/web/static/pets.js): a 16 x 14
 * grid per look (egg, cracked egg, baby, kid, grown-up, weak grown-up), the
 * agent's hat on top (the critter's still hat, or a smaller cut for the kid),
 * an open mouth when it is hungry, eyes shut while it sleeps or its agent is
 * idle, an ice cube while it hibernates and a one-cell aura outline for the
 * strong, mighty and legend stages. Colours are 0xRRGGBB, with an alpha for
 * the see-through parts, so a board draws them at its own colour depth.
 *
 *   deck_pet_t pet;
 *   deck_pet_from_proto(&pet, a->has_pet, &a->pet);      // in on_agent
 *   ...
 *   deck_pet_look_t look;
 *   deck_pet_opts_t o = {.hat = hat, .hat_color = color, .idle = idle};
 *   deck_pet_look(&look, &pet, &o);
 *   deck_pet_draw(&look, x, y, scale, my_fill, my_ctx);  // cell (0, 0) at pixel (x, y)
 *
 * deck_pet_motion() gives the small moves (a working pet bobs, an egg
 * waiting to be seen wobbles) for boards that already animate their critter.
 */
#ifndef DECK_PET_H
#define DECK_PET_H

#include <stdbool.h>
#include <stdint.h>

#include "proto/link.pb.h"

#ifdef __cplusplus
extern "C" {
#endif

typedef enum {
	DECK_PET_EGG,
	DECK_PET_BABY,
	DECK_PET_KID,
	DECK_PET_GROWN,
	DECK_PET_STRONG,
	DECK_PET_MIGHTY,
	DECK_PET_LEGEND,
} deck_pet_stage_t;

typedef enum {
	DECK_PET_FED,
	DECK_PET_HUNGRY,
	DECK_PET_WEAK,
	DECK_PET_ASLEEP, /* hibernating: drawn in an ice cube */
} deck_pet_mood_t;

/* An egg cracks once it has eaten this many tokens (pets.js CRACK_AT). */
#define DECK_PET_CRACK_AT 30000
#define DECK_PET_NAME_SIZE 16 /* link.options: Pet.name max_size */

/* An agent's pet as a board keeps it: 26 bytes. */
typedef struct {
	bool has;     /* false: no pet (Agent pets is off); draw the critter */
	bool cracked; /* an egg that has eaten DECK_PET_CRACK_AT tokens */
	bool ready;   /* an egg waiting to be seen: it wobbles (Pet.ready, once the deck sends it) */
	uint8_t stage;      /* deck_pet_stage_t */
	uint8_t mood;       /* deck_pet_mood_t */
	uint8_t hearts;     /* 0..4 */
	uint16_t level;     /* 0 while an egg */
	uint16_t level_from; /* the level before its latest meal (Pet.level_from, once sent); 0 = unknown */
	char name[DECK_PET_NAME_SIZE]; /* "" until it hatches */
} deck_pet_t;

/* Fills `out` from a Deck Link Agent's pet; has_pet false (pets off) clears it. */
void deck_pet_from_proto(deck_pet_t *out, bool has_pet, const agentsdeck_link_v1_Pet *in);

/* "egg" .. "legend" and "fed" .. "asleep" as the deck sends them. An unknown
 * stage is drawn as a grown-up (pets.js does the same), an unknown mood as fed. */
deck_pet_stage_t deck_pet_stage_of(const char *stage);
deck_pet_mood_t deck_pet_mood_of(const char *mood);
/* For people: "Egg", "Baby", "Kid", "Grown-up", "Strong", "Mighty", "Legend". */
const char *deck_pet_stage_name(uint8_t stage);

/* ---- the look ------------------------------------------------------------ */

/* The grid a look is drawn on, in cells: x DECK_PET_GRID_X0 .. DECK_PET_GRID_X0 + W - 1,
 * likewise y. The art itself is cells (0, 0) .. (15, 13); a hat rises to
 * y -2 and a cap's brim reaches x -2. The aura and the ice reach two cells
 * further, so leave cells -4 .. 19 across and -4 .. 15 down. */
#define DECK_PET_GRID_X0 (-4)
#define DECK_PET_GRID_Y0 (-4)
#define DECK_PET_GRID_W 24
#define DECK_PET_GRID_H 20

/* Which picture. */
typedef enum {
	DECK_PET_ART_EGG,
	DECK_PET_ART_CRACK,
	DECK_PET_ART_BABY,
	DECK_PET_ART_KID,
	DECK_PET_ART_BUILDER, /* grown-up and later */
	DECK_PET_ART_WEAK,    /* a grown-up with no hearts left, lying down */
} deck_pet_art_t;

typedef enum { DECK_PET_AURA_NONE, DECK_PET_AURA_STRONG, DECK_PET_AURA_MIGHTY, DECK_PET_AURA_LEGEND } deck_pet_aura_t;

typedef struct {
	uint8_t hat;       /* 0 = none, else 1 + the shape's index in DECK_PET_HAT_SHAPES */
	uint8_t hat_color; /* index in DECK_PET_HAT_COLORS; out of range: teal */
	bool idle;         /* its agent is idle or exited: eyes shut */
	bool working;      /* its agent is running: it bobs (deck_pet_motion) */
	bool plate;        /* a dark square plate under it, for a background in its own orange */
} deck_pet_opts_t;

/* The deck's hats, in the protocol's order (docs/protocol.md). */
#define DECK_PET_HAT_SHAPES                                                                    \
	{"hard hat", "cap", "beanie", "helmet", "wizard", "beret", "chef", "crown", "headphones", \
	 "propeller", "top hat", "cowboy", "bandana"}
#define DECK_PET_HAT_COLORS {"teal", "blue", "yellow", "green", "purple", "pink", "sky", "ink"}
#define DECK_PET_HAT_SHAPE_COUNT 13
#define DECK_PET_HAT_COLOR_COUNT 8
/* 1 + the shape's index, 0 for none or an unknown shape; a colour's index, teal (0) if unknown. */
uint8_t deck_pet_hat_of(const char *shape);
uint8_t deck_pet_hat_color_of(const char *color);

/* A pet's picture. cell[y - DECK_PET_GRID_Y0][x - DECK_PET_GRID_X0] holds the pets.js
 * key of each cell, 0 where it is empty:
 *   b body · d body shade · e eye · w shell · v shell shade · s spot · k dark line
 *   g ground · p pencil · m open mouth · c highlight · h hat · H hat shade */
typedef struct {
	char cell[DECK_PET_GRID_H][DECK_PET_GRID_W];
	uint8_t art;  /* deck_pet_art_t */
	uint8_t aura; /* deck_pet_aura_t */
	bool ice;     /* asleep: drawn in an ice cube */
	bool weak;    /* the body is the weak brown */
	bool plate;
	int8_t plate_x0, plate_y0, plate_x1, plate_y1; /* the plate's cells, inclusive */
	bool ready;   /* an egg waiting to be seen */
	bool bob;     /* working and awake */
	int8_t x0, y0, x1, y1; /* the cells drawn, inclusive (the art and hat, not the aura or ice) */
	uint32_t hat_rgb, hat_shade_rgb;
	bool hat_toned; /* the hat's colour is close to the aura's: drawn in its shade */
	uint32_t hat_cells[DECK_PET_GRID_H]; /* bit x - DECK_PET_GRID_X0 of row y - DECK_PET_GRID_Y0: a hat cell */
} deck_pet_look_t;

/* Builds the look. The hat stays readable inside an aura: no aura cell
 * touches a hat cell (eight ways), and a hat whose colour is close to the
 * aura's (deck_pet_hat_blends) is drawn in its shade. */
void deck_pet_look(deck_pet_look_t *look, const deck_pet_t *pet, const deck_pet_opts_t *opts);

/* Whether a hat colour is close to an aura's: contrast under 1.6 and hue
 * within 40 degrees (yellow and ink in strong; teal, blue and sky in mighty;
 * yellow in legend). */
bool deck_pet_hat_blends(uint8_t aura, uint8_t hat_color);

/* Whether (x, y) is a cell of the hat. */
bool deck_pet_hat_at(const deck_pet_look_t *look, int x, int y);

/* The key at (x, y) in the art's cells; 0 outside the grid. */
char deck_pet_at(const deck_pet_look_t *look, int x, int y);

/* A key's colour (0xRRGGBB) in this look. */
uint32_t deck_pet_rgb(const deck_pet_look_t *look, char key);

/* ---- drawing --------------------------------------------------------------- */

/* Paints a w x h pixel rectangle in rgb (0xRRGGBB). alpha 255 is solid; less
 * is see-through (the ice, the aura): mix with what is there, or draw it
 * solid when alpha >= 128 and skip it below, on a board that can't mix. */
typedef void (*deck_pet_fill_fn)(void *user, int x, int y, int w, int h, uint32_t rgb, uint8_t alpha);

/* Draws the look with cell (0, 0) at pixel (x, y), each cell scale x scale
 * pixels: the plate, the aura, the art and its hat, then the ice. Runs of
 * one colour along a row are one rectangle. */
void deck_pet_draw(const deck_pet_look_t *look, int x, int y, int scale, deck_pet_fill_fn fill, void *user);

/* The pixels the drawing may touch, relative to (x, y) at this scale. */
void deck_pet_bounds(const deck_pet_look_t *look, int scale, int *bx, int *by, int *bw, int *bh);

/* Where to draw it at ms (any clock): a working pet bobs up half a cell every
 * other half second; an egg waiting to be seen rocks a cell from side to side.
 * Pixels, added to (x, y). Both 0 when it holds still. */
void deck_pet_motion(const deck_pet_look_t *look, uint32_t ms, int scale, int *dx, int *dy);

#ifdef __cplusplus
}
#endif

#endif
