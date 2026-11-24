/* Host tests for deck_pet: the looks match the web deck's pets.js cell for
 * cell (pet_golden.h, from tools/pet_golden.mjs), the state from Deck Link,
 * and the drawing: colours, scale, the aura, the dark edge, the ice, the
 * bounds and the moves. Build with make. */
#include <math.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>

#include "deck_pet.h"
#include "pet_golden.h"

static int failures, checks;

#define CHECK(cond)                                                              \
	do {                                                                     \
		checks++;                                                        \
		if (!(cond)) {                                                   \
			failures++;                                              \
			fprintf(stderr, "%s:%d: CHECK failed: %s\n", __FILE__, __LINE__, #cond); \
		}                                                                \
	} while (0)

static uint32_t fnv(uint32_t h, const char *s)
{
	for (; *s; s++) {
		h ^= (uint8_t)*s;
		h *= 0x01000193u;
	}
	return h;
}

static uint32_t look_hash(const deck_pet_look_t *l)
{
	uint32_t h = 0x811c9dc5u;
	char buf[32];
	for (int y = DECK_PET_GRID_Y0; y < DECK_PET_GRID_Y0 + DECK_PET_GRID_H; y++)
		for (int x = DECK_PET_GRID_X0; x < DECK_PET_GRID_X0 + DECK_PET_GRID_W; x++) {
			const char k = deck_pet_at(l, x, y);
			if (!k)
				continue;
			snprintf(buf, sizeof buf, "%d,%d,%c;", x, y, k);
			h = fnv(h, buf);
		}
	return h;
}

static deck_pet_t pet_of(int stage, int mood, bool cracked)
{
	deck_pet_t p;
	memset(&p, 0, sizeof p);
	p.has = true;
	p.stage = (uint8_t)stage;
	p.mood = (uint8_t)mood;
	p.cracked = cracked;
	return p;
}

static void test_golden(void)
{
	for (size_t i = 0; i < sizeof pet_golden / sizeof pet_golden[0]; i++) {
		const deck_pet_t p = pet_of(pet_golden[i].stage, pet_golden[i].mood, pet_golden[i].cracked);
		deck_pet_opts_t o = {0};
		o.hat = (uint8_t)pet_golden[i].hat;
		o.hat_color = (uint8_t)pet_golden[i].color;
		o.idle = pet_golden[i].idle;
		deck_pet_look_t l;
		deck_pet_look(&l, &p, &o);
		const uint32_t h = look_hash(&l);
		if (h != pet_golden[i].hash)
			fprintf(stderr, "golden %zu (stage %d mood %d hat %d): %08x, want %08x\n", i, pet_golden[i].stage,
			        pet_golden[i].mood, pet_golden[i].hat, h, pet_golden[i].hash);
		CHECK(h == pet_golden[i].hash);
	}
}

static void test_state(void)
{
	agentsdeck_link_v1_Pet in = agentsdeck_link_v1_Pet_init_zero;
	deck_pet_t p;
	memset(&p, 0x55, sizeof p);
	deck_pet_from_proto(&p, false, &in); /* pets off */
	CHECK(!p.has && p.name[0] == 0 && p.level == 0);

	strcpy(in.name, "Pip");
	strcpy(in.stage, "mighty");
	strcpy(in.mood, "hungry");
	in.level = 31;
	in.ate = 5000000;
	in.hearts = 9;
	deck_pet_from_proto(&p, true, &in);
	CHECK(p.has && strcmp(p.name, "Pip") == 0);
	CHECK(p.stage == DECK_PET_MIGHTY && p.mood == DECK_PET_HUNGRY && p.level == 31 && p.hearts == 4);
	CHECK(p.cracked);
	CHECK(strcmp(deck_pet_stage_name(p.stage), "Mighty") == 0);

	strcpy(in.stage, "egg");
	in.ate = DECK_PET_CRACK_AT - 1;
	in.level = 70000;
	deck_pet_from_proto(&p, true, &in);
	CHECK(p.stage == DECK_PET_EGG && !p.cracked && p.level == 0xFFFF);
	in.ate = DECK_PET_CRACK_AT;
	deck_pet_from_proto(&p, true, &in);
	CHECK(p.cracked);

	/* Control bytes become spaces: a name is one line. */
	strcpy(in.name, "Ab\tcd\n");
	deck_pet_from_proto(&p, true, &in);
	CHECK(strcmp(p.name, "Ab cd ") == 0);

	CHECK(deck_pet_stage_of("") == DECK_PET_EGG);
	CHECK(deck_pet_stage_of("grown") == DECK_PET_GROWN);
	CHECK(deck_pet_stage_of("mythic") == DECK_PET_GROWN); /* a stage this firmware doesn't know */
	CHECK(deck_pet_mood_of("asleep") == DECK_PET_ASLEEP);
	CHECK(deck_pet_mood_of("bored") == DECK_PET_FED);
	CHECK(strcmp(deck_pet_stage_name(DECK_PET_GROWN), "Grown-up") == 0);
	CHECK(strcmp(deck_pet_stage_name(99), "") == 0);
	CHECK(deck_pet_hat_of("top hat") == 11 && deck_pet_hat_of("tiara") == 0 && deck_pet_hat_of(NULL) == 0);
	CHECK(deck_pet_hat_color_of("ink") == 7 && deck_pet_hat_color_of("mauve") == 0);
}

/* ---- a canvas that mixes like a board would ---------------------------------- */

#define CW 160
#define CH 160
#define BG 0x101010u
static uint32_t px[CH][CW];
static int fills, minx, miny, maxx, maxy;

static uint32_t mix(uint32_t bg, uint32_t fg, uint8_t a)
{
	uint32_t out = 0;
	for (int sh = 0; sh < 24; sh += 8) {
		const uint32_t b = (bg >> sh) & 0xFF, f = (fg >> sh) & 0xFF;
		out |= ((b * (255u - a) + f * a + 127) / 255) << sh;
	}
	return out;
}

static void fill(void *user, int x, int y, int w, int h, uint32_t rgb, uint8_t alpha)
{
	(void)user;
	fills++;
	CHECK(w > 0 && h > 0);
	if (x < minx) minx = x;
	if (y < miny) miny = y;
	if (x + w - 1 > maxx) maxx = x + w - 1;
	if (y + h - 1 > maxy) maxy = y + h - 1;
	for (int yy = y; yy < y + h; yy++)
		for (int xx = x; xx < x + w; xx++)
			if (xx >= 0 && yy >= 0 && xx < CW && yy < CH)
				px[yy][xx] = mix(px[yy][xx], rgb, alpha);
}

static void clear(void)
{
	for (int y = 0; y < CH; y++)
		for (int x = 0; x < CW; x++)
			px[y][x] = BG;
	fills = 0;
	minx = miny = 1 << 20;
	maxx = maxy = -(1 << 20);
}

static int ring_cells(const deck_pet_look_t *l, int x0, int y0, int s, uint32_t want)
{
	int n = 0;
	for (int y = DECK_PET_GRID_Y0; y < DECK_PET_GRID_Y0 + DECK_PET_GRID_H; y++)
		for (int x = DECK_PET_GRID_X0; x < DECK_PET_GRID_X0 + DECK_PET_GRID_W; x++)
			if (!deck_pet_at(l, x, y) && px[y0 + y * s][x0 + x * s] == want)
				n++;
	return n;
}

static void draw(const deck_pet_look_t *l, int x, int y, int s)
{
	clear();
	deck_pet_draw(l, x, y, s, fill, NULL);
	int bx, by, bw, bh;
	deck_pet_bounds(l, s, &bx, &by, &bw, &bh);
	CHECK(minx >= x + bx && miny >= y + by && maxx < x + bx + bw && maxy < y + by + bh);
}

static void test_draw(void)
{
	deck_pet_look_t l;
	deck_pet_opts_t o = {0};
	o.hat = deck_pet_hat_of("crown");
	o.hat_color = deck_pet_hat_color_of("blue");
	deck_pet_t p = pet_of(DECK_PET_GROWN, DECK_PET_FED, false);
	deck_pet_look(&l, &p, &o);
	CHECK(l.art == DECK_PET_ART_BUILDER && l.aura == DECK_PET_AURA_NONE && !l.ice);
	CHECK(l.x0 >= DECK_PET_GRID_X0 + 2 && l.y0 >= DECK_PET_GRID_Y0 + 2);
	CHECK(l.x1 <= DECK_PET_GRID_X0 + DECK_PET_GRID_W - 3 && l.y1 <= DECK_PET_GRID_Y0 + DECK_PET_GRID_H - 3);

	/* Every cell is scale x scale pixels of its key's colour; nothing round it. */
	for (int s = 1; s <= 4; s++) {
		const int x0 = 20, y0 = 30;
		draw(&l, x0, y0, s);
		int cellsN = 0;
		for (int y = DECK_PET_GRID_Y0; y < DECK_PET_GRID_Y0 + DECK_PET_GRID_H; y++)
			for (int x = DECK_PET_GRID_X0; x < DECK_PET_GRID_X0 + DECK_PET_GRID_W; x++) {
				const char k = deck_pet_at(&l, x, y);
				const uint32_t want = k ? deck_pet_rgb(&l, k) : BG;
				cellsN += k != 0;
				CHECK(px[y0 + y * s][x0 + x * s] == want);
				CHECK(px[y0 + y * s + s - 1][x0 + x * s + s - 1] == want);
			}
		CHECK(fills < cellsN / 2); /* runs, not cells */
	}
	CHECK(deck_pet_rgb(&l, 'b') == 0xFF4D00);
	CHECK(deck_pet_rgb(&l, 'h') == 0x6F8FD8 && deck_pet_rgb(&l, 'H') == 0x4A63A3);
	CHECK(deck_pet_at(&l, 9, 1) == 'h'); /* the crown sits on the body's top row */
	CHECK(deck_pet_at(&l, 100, 0) == 0 && deck_pet_at(&l, 0, -100) == 0);

	/* No hat: the egg's spots are teal. */
	deck_pet_opts_t bare = {0};
	p = pet_of(DECK_PET_EGG, DECK_PET_FED, false);
	deck_pet_look(&l, &p, &bare);
	CHECK(deck_pet_at(&l, 8, 2) == 's' && deck_pet_rgb(&l, 's') == 0x4FB3A6);
	deck_pet_look(&l, NULL, NULL); /* no pet at all: an egg */
	CHECK(l.art == DECK_PET_ART_EGG);

	/* Weak: brown body, no aura even at legend. */
	p = pet_of(DECK_PET_LEGEND, DECK_PET_WEAK, false);
	deck_pet_look(&l, &p, &o);
	CHECK(l.art == DECK_PET_ART_WEAK && l.weak && l.aura == DECK_PET_AURA_NONE);
	CHECK(deck_pet_rgb(&l, 'd') == 0xB0673F);
	p = pet_of(DECK_PET_KID, DECK_PET_WEAK, false);
	deck_pet_look(&l, &p, &o);
	CHECK(l.art == DECK_PET_ART_KID && deck_pet_rgb(&l, 'b') == 0xB0673F);

	/* The aura: one ring of cells round the art in the stage's colour. */
	const uint32_t aura_rgb[] = {0xFFE08A, 0x3FA9F5, 0xE8A317};
	const uint8_t aura_a[] = {191, 179, 217};
	for (int st = DECK_PET_STRONG; st <= DECK_PET_LEGEND; st++) {
		p = pet_of(st, DECK_PET_FED, false);
		deck_pet_look(&l, &p, &o);
		CHECK(l.aura == DECK_PET_AURA_STRONG + (st - DECK_PET_STRONG));
		draw(&l, 40, 40, 2);
		const uint32_t ring = mix(BG, aura_rgb[st - DECK_PET_STRONG], aura_a[st - DECK_PET_STRONG]);
		CHECK(ring_cells(&l, 40, 40, 2, ring) > 30);
		CHECK(px[40 + (l.y1 + 1) * 2][40 + 4 * 2] == ring);  /* right under a foot */
		CHECK(px[40 + (l.y1 + 2) * 2][40 + 4 * 2] == BG);    /* and only one cell deep */
		/* With a dark edge the aura moves out a cell. */
		o.outline = true;
		deck_pet_look(&l, &p, &o);
		draw(&l, 40, 40, 2);
		CHECK(px[40 + (l.y1 + 1) * 2][40 + 4 * 2] == 0x0A0A0A);
		CHECK(px[40 + (l.y1 + 2) * 2][40 + 4 * 2] == ring);
		o.outline = false;
	}
	p = pet_of(DECK_PET_GROWN, DECK_PET_FED, false);
	deck_pet_look(&l, &p, &o);
	draw(&l, 40, 40, 2);
	CHECK(px[40 + (l.y1 + 1) * 2][40 + 4 * 2] == BG); /* a grown-up has none */

	/* Asleep: the ice cube over it, no aura, frost under it. */
	p = pet_of(DECK_PET_LEGEND, DECK_PET_ASLEEP, false);
	deck_pet_look(&l, &p, &o);
	CHECK(l.ice && l.aura == DECK_PET_AURA_NONE);
	draw(&l, 40, 40, 3);
	const int bx = 40 + (l.x0 - 1) * 3, by = 40 + (l.y0 - 1) * 3;
	CHECK(px[by][bx] == mix(mix(BG, 0x8FD0F2, 102), 0xDFF4FF, 230)); /* the cube's corner */
	const int cx = 40 + 6 * 3, cy = 40 + 7 * 3;                    /* a body cell, seen through the ice */
	CHECK(deck_pet_at(&l, 6, 7) == 'b');
	CHECK(px[cy][cx] == mix(0xFF4D00, 0x8FD0F2, 102));
	CHECK(px[40 + (l.y1 + 2) * 3][40 + 8 * 3] == mix(BG, 0xDFF4FF, 115)); /* the frost */
	o.outline = true;
	deck_pet_look(&l, &p, &o);
	draw(&l, 40, 40, 3);
	CHECK(px[40 + (l.y0 - 2) * 3][40 + 8 * 3] == 0x0A0A0A); /* the dark frame above the cube */
	o.outline = false;

	/* Hungry: the open mouth; idle: eyes shut (the mouth wins). */
	p = pet_of(DECK_PET_GROWN, DECK_PET_HUNGRY, false);
	o.idle = true;
	deck_pet_look(&l, &p, &o);
	CHECK(deck_pet_at(&l, 7, 7) == 'm' && deck_pet_at(&l, 8, 8) == 'm' && deck_pet_at(&l, 4, 5) == 'e');
	p = pet_of(DECK_PET_GROWN, DECK_PET_FED, false);
	deck_pet_look(&l, &p, &o);
	CHECK(deck_pet_at(&l, 4, 5) == 'b' && deck_pet_at(&l, 3, 6) == 'e' && deck_pet_at(&l, 11, 6) == 'e');
	o.idle = false;

	/* Off the canvas: drawn clipped, no harm. */
	draw(&l, -60, -60, 4);
	draw(&l, CW - 10, CH - 10, 4);
}

/* WCAG contrast and hue, to check the kit's table of hat colours close to an aura's. */
static double lum(uint32_t c)
{
	double v[3];
	for (int i = 0; i < 3; i++) {
		const double x = ((c >> (16 - 8 * i)) & 0xFF) / 255.0;
		v[i] = x <= 0.04045 ? x / 12.92 : pow((x + 0.055) / 1.055, 2.4);
	}
	return 0.2126 * v[0] + 0.7152 * v[1] + 0.0722 * v[2];
}

static double hue(uint32_t c)
{
	const double r = ((c >> 16) & 0xFF) / 255.0, g = ((c >> 8) & 0xFF) / 255.0, b = (c & 0xFF) / 255.0;
	const double mx = fmax(r, fmax(g, b)), mn = fmin(r, fmin(g, b)), d = mx - mn;
	if (d == 0)
		return 0;
	double h = mx == r ? fmod((g - b) / d, 6) : mx == g ? (b - r) / d + 2 : (r - g) / d + 4;
	h *= 60;
	return h < 0 ? h + 360 : h;
}

static void test_hat_in_aura(void)
{
	const uint32_t aura_rgb[] = {0, 0xFFE08A, 0x3FA9F5, 0xE8A317};
	const uint32_t hat_main[] = {0x4FB3A6, 0x6F8FD8, 0xE8B94A, 0x6DBE6A, 0xA68BD8, 0xE58BB0, 0x5EC4D6, 0xE8E4DC};
	const uint32_t hat_shade[] = {0x2F7F75, 0x4A63A3, 0xB08429, 0x468A44, 0x7660A6, 0xB05E80, 0x3A8E9C, 0xA8A39A};
	int close = 0;
	for (int au = DECK_PET_AURA_STRONG; au <= DECK_PET_AURA_LEGEND; au++)
		for (int col = 0; col < DECK_PET_HAT_COLOR_COUNT; col++) {
			const double l1 = lum(aura_rgb[au]), l2 = lum(hat_main[col]);
			const double contrast = (fmax(l1, l2) + 0.05) / (fmin(l1, l2) + 0.05);
			double dh = fabs(hue(aura_rgb[au]) - hue(hat_main[col]));
			if (dh > 180)
				dh = 360 - dh;
			const bool want = contrast < 1.6 && dh <= 40;
			CHECK(deck_pet_hat_blends((uint8_t)au, (uint8_t)col) == want);
			close += want;
		}
	CHECK(close == 6); /* yellow, ink in strong; teal, blue, sky in mighty; yellow in legend */
	CHECK(!deck_pet_hat_blends(DECK_PET_AURA_NONE, 2) && !deck_pet_hat_blends(9, 2) && !deck_pet_hat_blends(1, 99));

	for (int st = DECK_PET_KID; st <= DECK_PET_LEGEND; st++)
		for (int hat = 1; hat <= DECK_PET_HAT_SHAPE_COUNT; hat++)
			for (int col = 0; col < DECK_PET_HAT_COLOR_COUNT; col++)
				for (int outline = 0; outline < 2; outline++) {
					deck_pet_t p = pet_of(st, DECK_PET_FED, false);
					deck_pet_opts_t o = {0};
					o.hat = (uint8_t)hat;
					o.hat_color = (uint8_t)col;
					o.outline = outline;
					deck_pet_look_t l;
					deck_pet_look(&l, &p, &o);
					/* In its shade when close to the aura, else its own colour. */
					const bool toned = deck_pet_hat_blends(l.aura, (uint8_t)col);
					CHECK(l.hat_toned == toned);
					CHECK(deck_pet_rgb(&l, 'h') == (toned ? hat_shade[col] : hat_main[col]));
					CHECK(deck_pet_rgb(&l, 'H') == hat_shade[col]);
					/* Every empty cell next to the hat stays background (or the dark edge). */
					draw(&l, 40, 40, 1);
					int hat_cells = 0;
					for (int y = DECK_PET_GRID_Y0; y < DECK_PET_GRID_Y0 + DECK_PET_GRID_H; y++)
						for (int x = DECK_PET_GRID_X0; x < DECK_PET_GRID_X0 + DECK_PET_GRID_W; x++) {
							if (!deck_pet_hat_at(&l, x, y))
								continue;
							hat_cells++;
							CHECK(deck_pet_at(&l, x, y) != 0);
							for (int d = 0; d < 9; d++) {
								const int nx = x + d % 3 - 1, ny = y + d / 3 - 1;
								if (deck_pet_at(&l, nx, ny))
									continue;
								const uint32_t v = px[40 + ny][40 + nx];
								CHECK(v == BG || (outline && v == 0x0A0A0A));
							}
						}
					CHECK(hat_cells > 0);
					/* The aura is still there round the body. */
					if (l.aura) {
						const uint32_t ring = mix(BG, aura_rgb[l.aura], l.aura == 1 ? 191 : l.aura == 2 ? 179 : 217);
						CHECK(ring_cells(&l, 40, 40, 1, ring) > 20);
					}
				}
}

static void test_motion(void)
{
	deck_pet_look_t l;
	deck_pet_opts_t o = {0};
	int dx, dy;
	deck_pet_t p = pet_of(DECK_PET_KID, DECK_PET_FED, false);
	deck_pet_look(&l, &p, &o);
	deck_pet_motion(&l, 700, 4, &dx, &dy);
	CHECK(dx == 0 && dy == 0); /* not working: still */
	o.working = true;
	deck_pet_look(&l, &p, &o);
	deck_pet_motion(&l, 200, 4, &dx, &dy);
	CHECK(dx == 0 && dy == 0);
	deck_pet_motion(&l, 700, 4, &dx, &dy);
	CHECK(dx == 0 && dy == -2);
	deck_pet_motion(&l, 1700, 1, &dx, &dy);
	CHECK(dy == -1);
	p.mood = DECK_PET_ASLEEP; /* in the ice it holds still */
	deck_pet_look(&l, &p, &o);
	deck_pet_motion(&l, 700, 4, &dx, &dy);
	CHECK(dx == 0 && dy == 0);

	/* An egg waiting to be seen rocks. */
	p = pet_of(DECK_PET_EGG, DECK_PET_FED, false);
	p.ready = true;
	deck_pet_look(&l, &p, &o);
	CHECK(l.ready);
	int left = 0, right = 0;
	for (uint32_t ms = 0; ms < 1600; ms += 50) {
		deck_pet_motion(&l, ms, 2, &dx, &dy);
		CHECK(dy == 0 && dx >= -1 && dx <= 1);
		left += dx < 0;
		right += dx > 0;
	}
	CHECK(left > 0 && right > 0);
	p.ready = false;
	deck_pet_look(&l, &p, &(deck_pet_opts_t){0});
	deck_pet_motion(&l, 250, 2, &dx, &dy);
	CHECK(dx == 0 && dy == 0);
}

int main(void)
{
	test_golden();
	test_state();
	test_draw();
	test_hat_in_aura();
	test_motion();
	if (failures) {
		fprintf(stderr, "test_pet: %d of %d checks failed\n", failures, checks);
		return 1;
	}
	printf("test_pet: %d checks passed\n", checks);
	return 0;
}
