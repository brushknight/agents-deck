// The deck's pixel critter, drawn with LVGL: a port of spriteSVG() in
// backend/internal/web/static/deck-core.js (and critter.dart), poses, hats and
// animations included. The sprite is 18 x 13 cells (x 0..18, y -4..9);
// Claude's flat block or Codex's onigiri.
#pragma once

#include <stdbool.h>
#include <stdint.h>

#include "deck_pet.h"
#include "lvgl.h"

typedef enum {
	POSE_IDLE, // asleep
	POSE_START,
	POSE_RUN,
	POSE_THINK,
	POSE_READ,
	POSE_WRITE,
	POSE_BASH,
	POSE_WEB,
	POSE_PLAN,
	POSE_DELEGATE,
	POSE_COMPACT,
	POSE_WAIT,
	POSE_ERR,
	POSE_EXIT,
	POSE_HUNGRY,
	POSE_CELEBRATE,
} pose_t;

typedef struct {
	int8_t x, y;
	char key;
} hat_extra_t;

typedef struct {
	int8_t x, top;
	uint8_t nrows;
	const char *const *rows; // a = hat colour, b = shade, c = highlight, d = near-black
	uint8_t nextras;
	const hat_extra_t *extras;
} hat_frame_t;

typedef struct {
	const char *name;
	uint16_t ms; // per frame while working
	hat_frame_t critter_still;
	uint8_t ncritter;
	const hat_frame_t *critter;
	hat_frame_t onigiri_still;
	uint8_t nonigiri;
	const hat_frame_t *onigiri;
} hat_art_t;

extern const hat_art_t hat_art[]; // hats.c, generated
extern const int hat_art_count;

typedef struct {
	pose_t pose;
	bool onigiri;         // Codex
	const char *hat;      // shape name, NULL or "" for none
	uint32_t hat_color;   // its main colour; the shade is looked up
	uint32_t body, cut;   // the critter's colour and the background behind it
	bool bubble;          // the thought bubble beside it
	deck_pet_t pet;       // the agent's pet while Agent pets is on (pet.has): drawn in its place
	deck_pet_opts_t pet_opts;
} critter_t;

// The hat colours (the deck's names): main and shade.
bool critter_hat_color(const char *name, uint32_t *main, uint32_t *shade);

// Draws the critter with its cell (0, -4) at pixel (x, y), cell size `cell`
// pixels, at time `ms` (animations). With a pet, the pet instead, at the
// nearest whole cell size: it reaches 6 cells round that box (aura, hat, ice).
void critter_draw(lv_layer_t *layer, const critter_t *c, int32_t x, int32_t y, float cell, uint32_t ms);
#define CRITTER_PET_REACH 13 // cells beyond the 18 x 13 box a pet may draw (sparks and flames over a hat, bubbles, the flag)
