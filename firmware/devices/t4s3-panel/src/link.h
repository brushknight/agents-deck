// Deck Link on the T4-S3's USB port (the ESP32-S3's own USB serial), in its
// own FreeRTOS task. The UI asks for actions with link_send(); answers come
// back as a short message in the agents state.
#pragma once

#include <stdbool.h>

typedef enum {
	ACTION_ANSWER,
	ACTION_FOCUS,
	ACTION_INTERRUPT,
} action_kind_t;

typedef struct {
	action_kind_t kind;
	char agent_id[12];
	char prompt_id[16];
	char key[8];
} action_t;

void link_start(void);

// Queues an action for the link task; false if the queue is full.
bool link_send(const action_t *a);
