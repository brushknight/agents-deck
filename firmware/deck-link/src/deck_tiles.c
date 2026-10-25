#include "deck_tiles.h"

#include <string.h>

static void copy_str(char *dst, size_t size, const char *src)
{
	size_t n = strlen(src);
	if (n > size - 1) {
		n = size - 1;
		while (n > 0 && ((uint8_t)src[n] & 0xC0) == 0x80)
			n--;
	}
	memcpy(dst, src, n);
	dst[n] = '\0';
}

void deck_tiles_init(deck_tiles_t *t)
{
	memset(t, 0, sizeof *t);
}

void deck_tiles_clear(deck_tiles_t *t)
{
	t->count = 0;
	t->in_snapshot = false;
	t->version++;
}

int deck_tiles_index(const deck_tiles_t *t, const char *id)
{
	for (int i = 0; i < t->count; i++)
		if (strcmp(t->tiles[i].id, id) == 0)
			return i;
	return -1;
}

const deck_tile_t *deck_tiles_find(const deck_tiles_t *t, const char *id)
{
	int i = deck_tiles_index(t, id);
	return i < 0 ? NULL : &t->tiles[i];
}

static void remove_at(deck_tiles_t *t, int i)
{
	memmove(&t->tiles[i], &t->tiles[i + 1], (size_t)(t->count - i - 1) * sizeof t->tiles[0]);
	t->count--;
}

static void fill(deck_tile_t *d, const agentsdeck_link_v1_Agent *a)
{
	memset(d, 0, sizeof *d);
	copy_str(d->id, sizeof d->id, a->id);
	d->slot = a->slot;
	copy_str(d->title, sizeof d->title, a->title);
	d->status = a->status;
	d->tool = a->tool;
	copy_str(d->folder, sizeof d->folder, a->folder);
	if (a->has_hat) {
		copy_str(d->hat_shape, sizeof d->hat_shape, a->hat.shape);
		copy_str(d->hat_color, sizeof d->hat_color, a->hat.color);
	}
	if (a->has_context && a->context.window > 0 && a->context.used > 0) {
		int64_t pm = a->context.used * 1000 / a->context.window;
		d->context_permille = (uint16_t)(pm > 1000 ? 1000 : pm);
	}
	d->unseen = a->unseen;
	d->focused = a->focused;
	d->lost = a->lost;
	if (a->has_waiting) {
		const agentsdeck_link_v1_Prompt *p = &a->waiting;
		d->has_prompt = true;
		copy_str(d->prompt_id, sizeof d->prompt_id, p->id);
		copy_str(d->prompt_title, sizeof d->prompt_title, p->title);
		d->prompt_kind = p->kind;
		for (pb_size_t i = 0; i < p->options_count && i < DECK_TILE_OPTIONS; i++) {
			deck_tile_option_t *o = &d->options[d->option_count++];
			copy_str(o->key, sizeof o->key, p->options[i].key);
			copy_str(o->label, sizeof o->label, p->options[i].label);
			o->primary = p->options[i].primary;
			o->selected = p->options[i].selected;
		}
	}
}

void deck_tiles_put(deck_tiles_t *t, const agentsdeck_link_v1_Agent *a)
{
	int i = deck_tiles_index(t, a->id);
	if (i >= 0)
		remove_at(t, i); /* re-inserted below: its slot may have changed */
	int at = 0;
	while (at < t->count && t->tiles[at].slot <= a->slot)
		at++;
	if (t->count == DECK_TILES_MAX) {
		if (at == DECK_TILES_MAX) { /* beyond the last slot we keep */
			t->dropped++;
			t->version++;
			return;
		}
		t->count--; /* drop the highest slot */
		t->dropped++;
	}
	memmove(&t->tiles[at + 1], &t->tiles[at], (size_t)(t->count - at) * sizeof t->tiles[0]);
	fill(&t->tiles[at], a);
	t->count++;
	t->version++;
}

void deck_tiles_remove(deck_tiles_t *t, const char *id)
{
	int i = deck_tiles_index(t, id);
	if (i >= 0) {
		remove_at(t, i);
		t->version++;
	}
}

void deck_tiles_snapshot_begin(deck_tiles_t *t)
{
	for (int i = 0; i < t->count; i++)
		t->tiles[i].stale = true;
	t->in_snapshot = true;
}

void deck_tiles_snapshot_end(deck_tiles_t *t)
{
	for (int i = t->count - 1; i >= 0; i--)
		if (t->tiles[i].stale)
			remove_at(t, i);
	t->in_snapshot = false;
	t->version++;
}

void deck_tiles_handle(deck_tiles_t *t, const agentsdeck_link_v1_Frame *f)
{
	switch (f->which_body) {
	case agentsdeck_link_v1_Frame_snapshot_begin_tag:
		deck_tiles_snapshot_begin(t);
		break;
	case agentsdeck_link_v1_Frame_agent_changed_tag:
		if (f->body.agent_changed.has_agent)
			deck_tiles_put(t, &f->body.agent_changed.agent);
		break;
	case agentsdeck_link_v1_Frame_agent_removed_tag:
		deck_tiles_remove(t, f->body.agent_removed.id);
		break;
	case agentsdeck_link_v1_Frame_snapshot_end_tag:
		deck_tiles_snapshot_end(t);
		break;
	default:
		break;
	}
}

int deck_tiles_count_status(const deck_tiles_t *t, agentsdeck_link_v1_Status status)
{
	int n = 0;
	for (int i = 0; i < t->count; i++)
		if (t->tiles[i].status == status)
			n++;
	return n;
}

const deck_tile_t *deck_tiles_first_waiting(const deck_tiles_t *t)
{
	const deck_tile_t *any = NULL;
	for (int i = 0; i < t->count; i++) {
		const deck_tile_t *x = &t->tiles[i];
		if (x->status != agentsdeck_link_v1_Status_STATUS_WAITING)
			continue;
		if (x->has_prompt)
			return x;
		if (!any)
			any = x;
	}
	return any;
}

int deck_tile_primary(const deck_tile_t *tile)
{
	for (int i = 0; i < tile->option_count; i++)
		if (tile->options[i].primary)
			return i;
	return -1;
}

const char *deck_status_name(agentsdeck_link_v1_Status status)
{
	switch (status) {
	case agentsdeck_link_v1_Status_STATUS_STARTING: return "starting";
	case agentsdeck_link_v1_Status_STATUS_RUNNING: return "running";
	case agentsdeck_link_v1_Status_STATUS_WAITING: return "waiting";
	case agentsdeck_link_v1_Status_STATUS_IDLE: return "idle";
	case agentsdeck_link_v1_Status_STATUS_ERROR: return "error";
	case agentsdeck_link_v1_Status_STATUS_EXITED: return "exited";
	default: return "unknown";
	}
}
