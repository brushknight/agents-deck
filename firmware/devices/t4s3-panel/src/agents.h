// What the panel knows: the deck's agents (full records, sorted by slot) and
// the link's state. The link task writes it, the UI task reads it; both hold
// the lock while they do. `version` changes with every write, so the UI
// redraws only when something changed.
#pragma once

#include <stdbool.h>
#include <stdint.h>

#include "deck_link.h"

#define AGENTS_MAX 16

typedef enum {
	LINK_WAITING,      // no Welcome yet (deck not running, or not added)
	LINK_NOT_APPROVED, // the deck knows the panel but hasn't approved it
	LINK_ONLINE,
	LINK_OFFLINE, // it was online, then went quiet
} link_status_t;

typedef struct {
	uint32_t version;
	link_status_t link;
	char device_name[40]; // from Welcome: what `agentctl devices approve` takes
	int64_t clock_offset_ms; // deck time - local time, for "3m ago"
	char message[96];     // a short note for the person (answered, refused …)
	uint32_t message_until_ms;
	int count;
	agentsdeck_link_v1_Agent *agents[AGENTS_MAX]; // sorted by slot
} agents_t;

void agents_init(void);
agents_t *agents_lock(void);
void agents_unlock(void);

// Writers (the link task). Each bumps version; the caller holds the lock.
void agents_put(agents_t *s, const agentsdeck_link_v1_Agent *a);
void agents_remove(agents_t *s, const char *id);
void agents_snapshot_begin(agents_t *s);
void agents_snapshot_end(agents_t *s);
void agents_say(agents_t *s, const char *text, uint32_t ms);

// Readers.
const agentsdeck_link_v1_Agent *agents_find(const agents_t *s, const char *id);
int agents_count_status(const agents_t *s, agentsdeck_link_v1_Status status);

// Milliseconds since boot.
uint32_t now_ms(void);
