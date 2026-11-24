/* deck_pet: an agent's pet (Agent.pet, while Agent pets is on), drawn as
 * pixel art. Optional, like deck_tiles: a board that never calls it links
 * none of it.
 *
 * The art is the web deck's (backend/internal/web/static/pets.js): a 16 x 14
 * grid per look (egg, cracked egg, baby, kid, grown-up, weak grown-up), the
 * pet's own hat, an open mouth when it is hungry, an ice cube while it
 * hibernates and flames round strong, mighty and legend pets. It does the
 * critter's poses (docs/pet-poses.md: eyes, arms, feet, bubbles, the flag,
 * the mini pet, the squeeze), and every part has a black stroke one pet
 * pixel wide. Colours are 0xRRGGBB, with an alpha for the see-through ice,
 * so a board draws them at its own colour depth.
 *
 *   deck_pet_t pet;
 *   deck_pet_from_proto(&pet, a->has_pet, &a->pet);      // in on_agent
 *   ...
 *   deck_pet_look_t look;                                // build it, draw it, drop it
 *   deck_pet_opts_t o = {0};
 *   o.pose = DECK_PET_POSE_RUN;                          // what its agent does
 *   o.ms = millis();                                     // where its motion is
 *   o.bubble = true;
 *   deck_pet_look(&look, &pet, &o);
 *   deck_pet_draw(&look, x, y, scale, my_fill, my_ctx);  // cell (0, 0) at pixel (x, y)
 *
 * deck_pet_look() is not reentrant: draw pets from one task.
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
#define DECK_PET_HEARTS 4

/* An agent's pet as a board keeps it: 28 bytes. */
typedef struct {
	bool has;     /* false: no pet (Agent pets is off); draw the critter */
	bool cracked; /* an egg that has eaten DECK_PET_CRACK_AT tokens */
	bool ready;   /* an egg waiting to be seen hatching (Pet.ready): it wobbles; HatchPet hatches it */
	uint8_t stage;      /* deck_pet_stage_t */
	uint8_t mood;       /* deck_pet_mood_t */
	uint8_t hearts;     /* 0..4 */
	uint16_t level;     /* 0 while an egg */
	uint8_t progress;   /* how far it is to the next level, 0..255 */
	uint8_t hat;        /* its own hat (Pet.hat, from a kid on): 1 + DECK_PET_HAT_SHAPES index, 0 none */
	uint8_t hat_color;  /* index in DECK_PET_HAT_COLORS */
	char name[DECK_PET_NAME_SIZE]; /* "" until it hatches */
} deck_pet_t;

/* Fills `out` from a Deck Link Agent's pet; has_pet false (pets off) clears
 * it. Pet.ready, Pet.level_from and Pet.hat are read when the generated code
 * has them (nanopb names each field's tag). */
void deck_pet_from_proto(deck_pet_t *out, bool has_pet, const agentsdeck_link_v1_Pet *in);

/* "egg" .. "legend" and "fed" .. "asleep" as the deck sends them. An unknown
 * stage is drawn as a grown-up (pets.js does the same), an unknown mood as fed. */
deck_pet_stage_t deck_pet_stage_of(const char *stage);
deck_pet_mood_t deck_pet_mood_of(const char *mood);
/* For people: "Egg", "Baby", "Kid", "Grown-up", "Strong", "Mighty", "Legend". */
const char *deck_pet_stage_name(uint8_t stage);

/* ---- the look ------------------------------------------------------------ */

/* The grid a look is drawn on, in cells: x DECK_PET_GRID_X0 ..
 * DECK_PET_GRID_X0 + W - 1, likewise y. The art itself is cells (0, 0) ..
 * (15, 13); a hat rises to y -2, bubbles and the flag reach right and up,
 * flames above the hat, the stroke a cell round all of it. */
#define DECK_PET_GRID_X0 (-6)
#define DECK_PET_GRID_Y0 (-14)
#define DECK_PET_GRID_W 32
#define DECK_PET_GRID_H 31

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

/* The critter's poses, in the order of panel-core's Pose, after NONE (a pet
 * that holds still: previews, lists). docs/pet-poses.md says what each does. */
typedef enum {
	DECK_PET_POSE_NONE,
	DECK_PET_POSE_THINK,
	DECK_PET_POSE_READ,
	DECK_PET_POSE_WRITE,
	DECK_PET_POSE_BASH,
	DECK_PET_POSE_WEB,
	DECK_PET_POSE_PLAN,
	DECK_PET_POSE_DELEGATE,
	DECK_PET_POSE_COMPACT,
	DECK_PET_POSE_RUN,
	DECK_PET_POSE_WAIT,
	DECK_PET_POSE_ERR,
	DECK_PET_POSE_IDLE,
	DECK_PET_POSE_START,
	DECK_PET_POSE_EXIT,
	DECK_PET_POSE_CELEBRATE,
	DECK_PET_POSE_HUNGRY,
} deck_pet_pose_t;

typedef struct {
	uint8_t pose;      /* deck_pet_pose_t */
	uint32_t ms;       /* any clock: where the pose's motion is */
	bool bubble;       /* the pose's bubble (what it does); false where there is no room */
	uint8_t hat;       /* the agent's hat (1 + shape, 0 none): only an egg's spots take its colour */
	uint8_t hat_color; /* the agent's hat colour */
	bool idle;         /* without a pose: eyes shut */
} deck_pet_opts_t;

/* The deck's hats, in the protocol's order (docs/protocol.md); pets also
 * find devil horns. */
#define DECK_PET_HAT_SHAPES                                                                    \
	{"hard hat", "cap", "beanie", "helmet", "wizard", "beret", "chef", "crown", "headphones", \
	 "propeller", "top hat", "cowboy", "bandana", "devil horns"}
#define DECK_PET_HAT_COLORS {"teal", "blue", "yellow", "green", "purple", "pink", "sky", "ink"}
#define DECK_PET_HAT_SHAPE_COUNT 14
#define DECK_PET_HAT_COLOR_COUNT 8
/* 1 + the shape's index, 0 for none or an unknown shape; a colour's index, teal (0) if unknown. */
uint8_t deck_pet_hat_of(const char *shape);
uint8_t deck_pet_hat_color_of(const char *color);

/* A pet's picture at one moment. cell[y - Y0][x - X0] holds the key of
 * each cell, 0 where it is empty:
 *   b body · d body shade · e eye · w shell, flag light · v shell shade · s spot
 *   k dark line · g ground · p pencil · m mouth · c highlight · h hat · H hat shade
 *   K a hat's near-black · P props and bubbles · B a plan's tick
 * The stroke, the flames and the legend's stars are bits per row (bit x - X0). */
typedef struct {
	char cell[DECK_PET_GRID_H][DECK_PET_GRID_W];
	uint32_t stroke[DECK_PET_GRID_H];
	uint32_t flame[DECK_PET_GRID_H];
	uint32_t star[DECK_PET_GRID_H];
	uint32_t spark[DECK_PET_GRID_H];
	uint32_t hat_cells[DECK_PET_GRID_H];
	uint8_t art;  /* deck_pet_art_t */
	uint8_t aura; /* deck_pet_aura_t */
	bool ice;     /* asleep: drawn in an ice cube */
	bool weak;    /* the body is the weak brown */
	bool ready;   /* an egg waiting to be seen */
	int8_t x0, y0, x1, y1;             /* every cell drawn this moment, inclusive (stroke and flames too) */
	int8_t ice_x0, ice_y0, ice_x1, ice_y1; /* the ice cube */
	uint32_t hat_rgb, hat_shade_rgb, spot_rgb;
} deck_pet_look_t;

void deck_pet_look(deck_pet_look_t *look, const deck_pet_t *pet, const deck_pet_opts_t *opts);

/* The key at (x, y); 0 outside the grid. */
char deck_pet_at(const deck_pet_look_t *look, int x, int y);
/* Whether (x, y) is a cell of the hat, the stroke, the flames. */
bool deck_pet_hat_at(const deck_pet_look_t *look, int x, int y);
bool deck_pet_stroke_at(const deck_pet_look_t *look, int x, int y);
bool deck_pet_flame_at(const deck_pet_look_t *look, int x, int y);

/* A key's colour (0xRRGGBB) in this look. */
uint32_t deck_pet_rgb(const deck_pet_look_t *look, char key);
#define DECK_PET_STROKE_RGB 0x000000u
#define DECK_PET_PROP_RGB 0xFFFFFFu

/* ---- drawing --------------------------------------------------------------- */

/* Paints a w x h pixel rectangle in rgb (0xRRGGBB). alpha 255 is solid; less
 * is see-through (the ice): mix with what is there, or skip it below 128 on a
 * board that can't mix. */
typedef void (*deck_pet_fill_fn)(void *user, int x, int y, int w, int h, uint32_t rgb, uint8_t alpha);

/* Draws the look with cell (0, 0) at pixel (x, y), each cell scale x scale
 * pixels, in layers: the flames, the stroke, the ice's frost, the pet with
 * its props and bubbles, the ice, the stars. Runs of one colour along a row
 * are one rectangle. */
void deck_pet_draw(const deck_pet_look_t *look, int x, int y, int scale, deck_pet_fill_fn fill, void *user);

/* The pixels this look touches, relative to (x, y) at this scale. */
void deck_pet_bounds(const deck_pet_look_t *look, int scale, int *bx, int *by, int *bw, int *bh);

/* The furthest any look reaches, in cells (inclusive): for a board's fixed box round a pet. */
#define DECK_PET_REACH_X0 (-5)
#define DECK_PET_REACH_Y0 (-12)
#define DECK_PET_REACH_X1 24
#define DECK_PET_REACH_Y1 15

/* An egg waiting to be seen wobbles a whole cell from side to side (pixels, added to x). */
void deck_pet_motion(const deck_pet_look_t *look, uint32_t ms, int scale, int *dx, int *dy);

#ifdef __cplusplus
}
#endif

#endif
