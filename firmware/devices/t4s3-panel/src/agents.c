#include "agents.h"

#include <stdio.h>
#include <string.h>

#include "esp_heap_caps.h"
#include "esp_timer.h"
#include "freertos/FreeRTOS.h"
#include "freertos/semphr.h"

static agents_t state;
static SemaphoreHandle_t lock;
static bool seen[AGENTS_MAX]; // during a snapshot: sent again in it
static bool in_snapshot;
// The records live in PSRAM (about 3.5 KB each), allocated once.
static agentsdeck_link_v1_Agent *pool[AGENTS_MAX];
static bool used[AGENTS_MAX];

void agents_init(void)
{
	lock = xSemaphoreCreateMutex();
	for (int i = 0; i < AGENTS_MAX; i++)
		pool[i] = heap_caps_calloc(1, sizeof(agentsdeck_link_v1_Agent), MALLOC_CAP_SPIRAM);
}

agents_t *agents_lock(void)
{
	xSemaphoreTake(lock, portMAX_DELAY);
	return &state;
}

void agents_unlock(void) { xSemaphoreGive(lock); }

uint32_t now_ms(void) { return (uint32_t)(esp_timer_get_time() / 1000); }

static int index_of(const agents_t *s, const char *id)
{
	for (int i = 0; i < s->count; i++)
		if (strcmp(s->agents[i]->id, id) == 0)
			return i;
	return -1;
}

static void release(agentsdeck_link_v1_Agent *a)
{
	for (int i = 0; i < AGENTS_MAX; i++)
		if (pool[i] == a)
			used[i] = false;
}

static void remove_at(agents_t *s, int i)
{
	release(s->agents[i]);
	memmove(&s->agents[i], &s->agents[i + 1], (s->count - i - 1) * sizeof s->agents[0]);
	memmove(&seen[i], &seen[i + 1], (s->count - i - 1) * sizeof seen[0]);
	s->count--;
}

void agents_put(agents_t *s, const agentsdeck_link_v1_Agent *a)
{
	int i = index_of(s, a->id);
	agentsdeck_link_v1_Agent *rec = NULL;
	if (i >= 0) { // take it out: its slot may have changed
		rec = s->agents[i];
		memmove(&s->agents[i], &s->agents[i + 1], (s->count - i - 1) * sizeof s->agents[0]);
		memmove(&seen[i], &seen[i + 1], (s->count - i - 1) * sizeof seen[0]);
		s->count--;
	} else {
		for (int k = 0; k < AGENTS_MAX && !rec; k++)
			if (!used[k] && pool[k]) {
				used[k] = true;
				rec = pool[k];
			}
		if (!rec)
			return; // more agents than the panel keeps
	}
	*rec = *a;
	int at = 0;
	while (at < s->count && s->agents[at]->slot <= a->slot)
		at++;
	memmove(&s->agents[at + 1], &s->agents[at], (s->count - at) * sizeof s->agents[0]);
	memmove(&seen[at + 1], &seen[at], (s->count - at) * sizeof seen[0]);
	s->agents[at] = rec;
	seen[at] = true;
	s->count++;
	s->version++;
}

void agents_remove(agents_t *s, const char *id)
{
	int i = index_of(s, id);
	if (i >= 0) {
		remove_at(s, i);
		s->version++;
	}
}

void agents_snapshot_begin(agents_t *s)
{
	in_snapshot = true;
	for (int i = 0; i < s->count; i++)
		seen[i] = false;
}

// Agents the snapshot didn't mention are gone.
void agents_snapshot_end(agents_t *s)
{
	if (!in_snapshot)
		return;
	in_snapshot = false;
	for (int i = s->count - 1; i >= 0; i--)
		if (!seen[i])
			remove_at(s, i);
	s->version++;
}

void agents_say(agents_t *s, const char *text, uint32_t ms)
{
	snprintf(s->message, sizeof s->message, "%s", text);
	s->message_until_ms = now_ms() + ms;
	s->version++;
}

const agentsdeck_link_v1_Agent *agents_find(const agents_t *s, const char *id)
{
	int i = index_of(s, id);
	return i >= 0 ? s->agents[i] : NULL;
}

int agents_count_status(const agents_t *s, agentsdeck_link_v1_Status status)
{
	int n = 0;
	for (int i = 0; i < s->count; i++)
		if (s->agents[i]->status == status)
			n++;
	return n;
}
