/* deck_tiles: an optional helper that keeps what a simple device shows per
 * agent in a fixed table, sorted by slot. Feed it every frame from the
 * on_frame callback:
 *
 *   static deck_tiles_t tiles;
 *   static void on_frame(deck_link_t *ctx, const deck_frame_t *f) { deck_tiles_handle(&tiles, f); }
 *
 * then read tiles.tiles[0 .. tiles.count) and redraw when tiles.version
 * changes. Text longer than the tile's buffers is cut on a UTF-8 boundary.
 */
#ifndef DECK_TILES_H
#define DECK_TILES_H

#include <stdbool.h>
#include <stdint.h>

#include "deck_link.h"

#ifdef __cplusplus
extern "C" {
#endif

#ifndef DECK_TILES_MAX
#define DECK_TILES_MAX 16
#endif
#ifndef DECK_TILE_TITLE_SIZE
#define DECK_TILE_TITLE_SIZE 48 /* bytes, NUL included */
#endif
#ifndef DECK_TILE_FOLDER_SIZE
#define DECK_TILE_FOLDER_SIZE 32
#endif
#ifndef DECK_TILE_PROMPT_SIZE
#define DECK_TILE_PROMPT_SIZE 64
#endif
#ifndef DECK_TILE_LABEL_SIZE
#define DECK_TILE_LABEL_SIZE 32
#endif
#define DECK_TILE_OPTIONS 6

typedef struct {
	char key[8];
	char label[DECK_TILE_LABEL_SIZE];
	bool primary;
	bool selected; /* ticked: an answer picked so far on a pick-any question */
} deck_tile_option_t;

typedef struct {
	char id[12];
	uint32_t slot;
	char title[DECK_TILE_TITLE_SIZE];
	agentsdeck_link_v1_Status status;
	agentsdeck_link_v1_Tool tool;
	char folder[DECK_TILE_FOLDER_SIZE];
	char hat_shape[16];
	char hat_color[12];
	uint16_t context_permille; /* context used / window x 1000; 0 if unknown */
	bool unseen;
	bool focused;
	bool lost;
	/* The prompt (needs FIELDS_PROMPT in the subscription). */
	bool has_prompt;
	char prompt_id[16];
	char prompt_title[DECK_TILE_PROMPT_SIZE];
	agentsdeck_link_v1_PromptKind prompt_kind;
	uint8_t option_count;
	deck_tile_option_t options[DECK_TILE_OPTIONS];
	bool stale; /* internal: not seen in the current snapshot yet */
} deck_tile_t;

typedef struct {
	deck_tile_t tiles[DECK_TILES_MAX]; /* sorted by slot */
	uint8_t count;
	uint32_t version;  /* bumped on every change */
	uint32_t dropped;  /* agents that didn't fit (higher slots are dropped) */
	bool in_snapshot;
} deck_tiles_t;

void deck_tiles_init(deck_tiles_t *t);
/* Handles SnapshotBegin, AgentChanged, AgentRemoved and SnapshotEnd. */
void deck_tiles_handle(deck_tiles_t *t, const agentsdeck_link_v1_Frame *f);

/* The same, one event at a time. */
void deck_tiles_snapshot_begin(deck_tiles_t *t);
void deck_tiles_put(deck_tiles_t *t, const agentsdeck_link_v1_Agent *a);
void deck_tiles_remove(deck_tiles_t *t, const char *id);
void deck_tiles_snapshot_end(deck_tiles_t *t);
void deck_tiles_clear(deck_tiles_t *t);

/* Lookups; NULL or -1 when there's none. */
const deck_tile_t *deck_tiles_find(const deck_tiles_t *t, const char *id);
int deck_tiles_index(const deck_tiles_t *t, const char *id);
int deck_tiles_count_status(const deck_tiles_t *t, agentsdeck_link_v1_Status status);
/* The lowest-slot agent with status WAITING (one with a prompt first). */
const deck_tile_t *deck_tiles_first_waiting(const deck_tiles_t *t);
/* The index of the tile's primary option, or -1. */
int deck_tile_primary(const deck_tile_t *tile);

/* "running", "waiting", ... for logs and small screens. */
const char *deck_status_name(agentsdeck_link_v1_Status status);

#ifdef __cplusplus
}
#endif

#endif
