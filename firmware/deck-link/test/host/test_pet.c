/* Host tests for deck_pet: the looks match the web deck's pets.js and
 * pet-poses.js cell for cell in every pose over time (pet_golden.h, from
 * tools/pet_golden.mjs), the state from Deck Link, the stroke, the flames,
 * the drawing (colours, layers, scale, the ice), the reach and the bounds.
 * Build with make. */
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

#define FOR_GRID(x, y)                                                                     \
	for (int y = DECK_PET_GRID_Y0; y < DECK_PET_GRID_Y0 + DECK_PET_GRID_H; y++)        \
		for (int x = DECK_PET_GRID_X0; x < DECK_PET_GRID_X0 + DECK_PET_GRID_W; x++)

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
	FOR_GRID(x, y) {
		const char k = deck_pet_at(l, x, y);
		if (!k)
			continue;
		snprintf(buf, sizeof buf, "%d,%d,%c;", x, y, k);
		h = fnv(h, buf);
	}
	return h;
}

static deck_pet_t pet_of(int stage, int mood, int hat)
{
	deck_pet_t p;
	memset(&p, 0, sizeof p);
	p.has = true;
	p.stage = (uint8_t)stage;
	p.mood = (uint8_t)mood;
	p.hat = (uint8_t)hat;
	p.hat_color = 1;
	return p;
}

static deck_pet_opts_t posed(int pose, uint32_t ms)
{
	deck_pet_opts_t o;
	memset(&o, 0, sizeof o);
	o.pose = (uint8_t)pose;
	o.ms = ms;
	o.bubble = true;
	return o;
}

static deck_pet_look_t L; /* about 4 KB: kept off the stack */

static void test_golden(void)
{
	int bad = 0;
	for (size_t i = 0; i < sizeof pet_golden / sizeof pet_golden[0]; i++) {
		deck_pet_t p = pet_of(pet_golden[i].stage, pet_golden[i].mood, pet_golden[i].hat);
		p.cracked = pet_golden[i].cracked;
		p.ready = pet_golden[i].ready;
		for (size_t t = 0; t < sizeof pet_golden_ms / sizeof pet_golden_ms[0]; t++) {
			const deck_pet_opts_t o = posed(pet_golden[i].pose, pet_golden_ms[t]);
			deck_pet_look(&L, &p, &o);
			const uint32_t h = look_hash(&L);
			if (h != pet_golden[i].hash[t] && bad++ < 12)
				fprintf(stderr, "golden %zu (stage %d mood %d hat %d pose %d) at %u ms: %08x, want %08x\n", i,
				        pet_golden[i].stage, pet_golden[i].mood, pet_golden[i].hat, pet_golden[i].pose,
				        pet_golden_ms[t], h, pet_golden[i].hash[t]);
			CHECK(h == pet_golden[i].hash[t]);
		}
	}
}

static void test_state(void)
{
	agentsdeck_link_v1_Pet in = agentsdeck_link_v1_Pet_init_zero;
	deck_pet_t p;
	memset(&p, 0x55, sizeof p);
	deck_pet_from_proto(&p, false, &in); /* pets off */
	CHECK(!p.has && p.name[0] == 0 && p.level == 0 && p.hat == 0);

	strcpy(in.name, "Pip");
	strcpy(in.stage, "mighty");
	strcpy(in.mood, "hungry");
	in.level = 31;
	in.ate = 900;
	in.next_at = 1000;
	in.hearts = 9;
	deck_pet_from_proto(&p, true, &in);
	CHECK(p.has && strcmp(p.name, "Pip") == 0);
	CHECK(p.stage == DECK_PET_MIGHTY && p.mood == DECK_PET_HUNGRY && p.level == 31 && p.hearts == 4);
	CHECK(p.progress == 127); /* this level from 800 (the default step) to 1000 */
	CHECK(strcmp(deck_pet_stage_name(p.stage), "Mighty") == 0);

	strcpy(in.stage, "egg");
	in.level = 0;
	in.ate = DECK_PET_CRACK_AT - 1;
	in.next_at = 4 * (DECK_PET_CRACK_AT - 1);
	deck_pet_from_proto(&p, true, &in);
	CHECK(p.stage == DECK_PET_EGG && !p.cracked && p.progress == 63);
	in.ate = DECK_PET_CRACK_AT;
	deck_pet_from_proto(&p, true, &in);
	CHECK(p.cracked);
	in.level = 70000;
	deck_pet_from_proto(&p, true, &in);
	CHECK(p.level == 0xFFFF);

	strcpy(in.name, "Ab\tcd\n");
	deck_pet_from_proto(&p, true, &in);
	CHECK(strcmp(p.name, "Ab cd ") == 0);

	CHECK(deck_pet_stage_of("") == DECK_PET_EGG);
	CHECK(deck_pet_stage_of("mythic") == DECK_PET_GROWN);
	CHECK(deck_pet_mood_of("bored") == DECK_PET_FED);
	CHECK(strcmp(deck_pet_stage_name(DECK_PET_GROWN), "Grown-up") == 0 && strcmp(deck_pet_stage_name(99), "") == 0);
	CHECK(deck_pet_hat_of("top hat") == 11 && deck_pet_hat_of("devil horns") == 14 && deck_pet_hat_of("tiara") == 0);
	CHECK(deck_pet_hat_color_of("ink") == 7 && deck_pet_hat_color_of("mauve") == 0);
}

/* Every pet, every pose, many moments. */
#define EACH_LOOK(body)                                                                              \
	for (int st = 0; st < 7; st++)                                                               \
		for (int mood = 0; mood < 4; mood++)                                                 \
			for (int hat = 0; hat <= DECK_PET_HAT_SHAPE_COUNT; hat += hat < 9 ? 2 : 1)   \
				for (int pose = 0; pose <= DECK_PET_POSE_HUNGRY; pose++)             \
					for (uint32_t ms = 0; ms < 2400; ms += 150) {                \
						deck_pet_t p = pet_of(st, mood, hat);        \
						p.ready = st == 0 && hat % 2;                \
						p.cracked = p.ready;                         \
						const deck_pet_opts_t o = posed(pose, ms);   \
						deck_pet_look(&L, &p, &o);                   \
						body                                         \
					}

static void test_stroke_and_reach(void)
{
	int rx0 = 99, ry0 = 99, rx1 = -99, ry1 = -99;
	EACH_LOOK({
		FOR_GRID(x, y) {
			const bool cell = deck_pet_at(&L, x, y) != 0;
			const bool near = deck_pet_at(&L, x + 1, y) || deck_pet_at(&L, x - 1, y) || deck_pet_at(&L, x, y + 1) ||
			                  deck_pet_at(&L, x, y - 1);
			/* the stroke: exactly the empty cells next to a drawn one (four ways) */
			if (deck_pet_stroke_at(&L, x, y) != (!cell && near)) {
				fprintf(stderr, "stroke at %d,%d: st %d mood %d hat %d pose %d ms %u\n", x, y, st, mood, hat, pose, ms);
				CHECK(0);
				goto out;
			}
		}
		if (L.x0 < rx0) rx0 = L.x0;
		if (L.y0 < ry0) ry0 = L.y0;
		if (L.x1 > rx1) rx1 = L.x1;
		if (L.y1 > ry1) ry1 = L.y1;
		/* nothing reaches the grid's edge */
		CHECK(L.x0 > DECK_PET_GRID_X0 - 1 && L.y0 > DECK_PET_GRID_Y0 - 1);
		CHECK(L.x1 < DECK_PET_GRID_X0 + DECK_PET_GRID_W && L.y1 < DECK_PET_GRID_Y0 + DECK_PET_GRID_H);
	})
out:
	printf("test_pet: the looks reach x %d..%d, y %d..%d\n", rx0, rx1, ry0, ry1);
	CHECK(rx0 >= DECK_PET_REACH_X0 && ry0 >= DECK_PET_REACH_Y0 && rx1 <= DECK_PET_REACH_X1 && ry1 <= DECK_PET_REACH_Y1);
}

static void test_flames(void)
{
	for (int st = 0; st < 7; st++)
		for (int mood = 0; mood < 4; mood++)
			for (int pose = 0; pose <= DECK_PET_POSE_HUNGRY; pose++) {
				deck_pet_t p = pet_of(st, mood, 11);
				const deck_pet_opts_t o = posed(pose, 0);
				deck_pet_look(&L, &p, &o);
				int flames = 0, stars = 0;
				FOR_GRID(x, y) {
					flames += deck_pet_flame_at(&L, x, y);
					stars += (L.star[y - DECK_PET_GRID_Y0] >> (x - DECK_PET_GRID_X0)) & 1u;
				}
				const bool glow = st >= DECK_PET_STRONG && mood != DECK_PET_WEAK && mood != DECK_PET_ASLEEP &&
				                  pose != DECK_PET_POSE_EXIT;
				CHECK((L.aura != 0) == glow);
				CHECK((flames > 20) == glow);
				CHECK((stars > 0) == (glow && st == DECK_PET_LEGEND));
			}
}

/* ---- a canvas that mixes like a board would ---------------------------------- */

#define CW 220
#define CH 200
#define BG 0x101010u
static uint32_t px[CH][CW];
static int minx, miny, maxx, maxy;

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

static void draw(const deck_pet_look_t *l, int x, int y, int s)
{
	for (int yy = 0; yy < CH; yy++)
		for (int xx = 0; xx < CW; xx++) px[yy][xx] = BG;
	minx = miny = 1 << 20;
	maxx = maxy = -(1 << 20);
	deck_pet_draw(l, x, y, s, fill, NULL);
	int bx, by, bw, bh;
	deck_pet_bounds(l, s, &bx, &by, &bw, &bh);
	CHECK(minx >= x + bx && miny >= y + by && maxx < x + bx + bw && maxy < y + by + bh);
}

static void test_draw(void)
{
	/* A strong grown-up with a top hat, running: every cell in its colour,
	 * the stroke black, the flames under it all, the props white. */
	deck_pet_t p = pet_of(DECK_PET_STRONG, DECK_PET_FED, 11);
	for (int s = 1; s <= 3; s++)
		for (uint32_t ms = 0; ms < 1200; ms += 200) {
			const deck_pet_opts_t o = posed(DECK_PET_POSE_RUN, ms);
			deck_pet_look(&L, &p, &o);
			const int ox = 6 * 3 + 20, oy = 13 * 3 + 20;
			draw(&L, ox, oy, s);
			int props = 0;
			FOR_GRID(x, y) {
				const char k = deck_pet_at(&L, x, y);
				const bool spark = (L.spark[y - DECK_PET_GRID_Y0] >> (x - DECK_PET_GRID_X0)) & 1u;
				const uint32_t want = spark ? 0xFFFFFFu   /* the sparks rise in front */
				                      : k ? deck_pet_rgb(&L, k)
				                      : deck_pet_stroke_at(&L, x, y) ? 0x000000u
				                      : deck_pet_flame_at(&L, x, y) ? 0xFFE08Au : BG;
				CHECK(px[oy + y * s][ox + x * s] == want);
				CHECK(px[oy + y * s + s - 1][ox + x * s + s - 1] == want);
				props += k == 'P';
			}
			CHECK(L.aura == DECK_PET_AURA_STRONG);
			CHECK(props == 0 || deck_pet_rgb(&L, 'P') == 0xFFFFFFu);
		}
	CHECK(deck_pet_rgb(&L, 'h') == 0x6F8FD8 && deck_pet_rgb(&L, 'H') == 0x4A63A3);

	/* Its own hat; the agent's hat only colours an egg's spots. */
	deck_pet_opts_t o = posed(DECK_PET_POSE_NONE, 0);
	o.hat = 3;
	o.hat_color = 2; /* yellow */
	p = pet_of(DECK_PET_EGG, DECK_PET_FED, 0);
	deck_pet_look(&L, &p, &o);
	CHECK(deck_pet_at(&L, 8, 2) == 's' && deck_pet_rgb(&L, 's') == 0xE8B94A);
	o.hat = 0;
	deck_pet_look(&L, &p, &o);
	CHECK(deck_pet_rgb(&L, 's') == 0x4FB3A6); /* no hats: teal */
	p = pet_of(DECK_PET_GROWN, DECK_PET_FED, 0);
	deck_pet_look(&L, &p, &o);
	int hat = 0;
	FOR_GRID(x, y) hat += deck_pet_hat_at(&L, x, y);
	CHECK(hat == 0);
	p.hat = 11;
	deck_pet_look(&L, &p, &o);
	FOR_GRID(x, y) hat += deck_pet_hat_at(&L, x, y);
	CHECK(hat > 20 && deck_pet_hat_at(&L, 5, -1));
	deck_pet_look(&L, NULL, NULL); /* no pet at all: an egg */
	CHECK(L.art == DECK_PET_ART_EGG);

	/* Without its bubble: no props for a waiting pet. */
	p = pet_of(DECK_PET_KID, DECK_PET_FED, 0);
	o = posed(DECK_PET_POSE_WAIT, 0);
	deck_pet_look(&L, &p, &o);
	int props = 0;
	FOR_GRID(x, y) props += deck_pet_at(&L, x, y) == 'P';
	CHECK(props == 6);
	o.bubble = false;
	deck_pet_look(&L, &p, &o);
	props = 0;
	FOR_GRID(x, y) props += deck_pet_at(&L, x, y) == 'P';
	CHECK(props == 0);

	/* Asleep: the ice cube over it, frost under it; it still asks. */
	p = pet_of(DECK_PET_LEGEND, DECK_PET_ASLEEP, 0);
	o = posed(DECK_PET_POSE_WAIT, 0);
	deck_pet_look(&L, &p, &o);
	CHECK(L.ice && L.aura == DECK_PET_AURA_NONE);
	draw(&L, 60, 80, 2);
	CHECK(deck_pet_at(&L, 6, 7) == 'b');
	CHECK(px[80 + 7 * 2][60 + 6 * 2] == mix(0xFF4D00, 0x8FD0F2, 102)); /* the body, seen through the ice */
	CHECK(px[80 + (L.ice_y1 + 1) * 2][60 + 8 * 2] == mix(BG, 0xDFF4FF, 115)); /* the frost */
	props = 0;
	FOR_GRID(x, y) props += deck_pet_at(&L, x, y) == 'P';
	CHECK(props == 6); /* "?" beside the cube */
	o = posed(DECK_PET_POSE_RUN, 300);
	deck_pet_look(&L, &p, &o);
	props = 0;
	FOR_GRID(x, y) props += deck_pet_at(&L, x, y) == 'P';
	CHECK(props == 0); /* frozen: no pose */

	/* Exited: hollow, no flames, the hat in its shade. */
	p = pet_of(DECK_PET_LEGEND, DECK_PET_FED, 2);
	o = posed(DECK_PET_POSE_EXIT, 0);
	deck_pet_look(&L, &p, &o);
	CHECK(!L.aura && deck_pet_at(&L, 7, 7) == 0 && deck_pet_rgb(&L, 'h') == 0x4A63A3);

	/* Off the canvas: drawn clipped, no harm. */
	draw(&L, -90, -90, 4);
	draw(&L, CW - 10, CH - 10, 4);
}

/* The face stays on top: in every frame of every pose (but the exited one's
 * hollow outline) a hatched pet shows as many eye cells as in any other
 * frame of that pose; none go under the body (the compacting squeeze once
 * hid them). The weak grown-up's eyes are its dark cells, but not its
 * mouth (the rows where columns 6 to 8 are all dark) or the flag's squares
 * (right of column 12). */
static int eye_cells(const deck_pet_look_t *l)
{
	int n = 0;
	FOR_GRID(x, y) {
		const char k = deck_pet_at(l, x, y);
		if (l->art != DECK_PET_ART_WEAK) {
			n += k == 'e';
			continue;
		}
		const bool mouth = deck_pet_at(l, 6, y) == 'k' && deck_pet_at(l, 7, y) == 'k' && deck_pet_at(l, 8, y) == 'k';
		n += k == 'k' && x <= 12 && !(mouth && x >= 6 && x <= 8);
	}
	return n;
}

static void test_face(void)
{
	for (int st = DECK_PET_BABY; st <= DECK_PET_LEGEND; st++)
		for (int mood = 0; mood < 4; mood++)
			for (int hat = 0; hat <= DECK_PET_HAT_SHAPE_COUNT; hat += 3)
				for (int pose = DECK_PET_POSE_THINK; pose <= DECK_PET_POSE_HUNGRY; pose++) {
					if (pose == DECK_PET_POSE_EXIT)
						continue;
					deck_pet_t p = pet_of(st, mood, hat);
					int lo = 999, hi = -1;
					for (uint32_t ms = 0; ms < 2400; ms += 50) {
						const deck_pet_opts_t o = posed(pose, ms);
						deck_pet_look(&L, &p, &o);
						const int n = eye_cells(&L);
						if (n < lo) lo = n;
						if (n > hi) hi = n;
					}
					if (lo != hi || lo == 0)
						fprintf(stderr, "face: stage %d mood %d hat %d pose %d: eye cells %d..%d\n", st, mood, hat, pose, lo, hi);
					CHECK(lo == hi && lo > 0);
				}
}

/* The feet stay on the body: in every frame of every pose (but the exited
 * one's hollow outline) a hatched pet's body (body, shade, eyes, mouth,
 * ground; not its hat, props or the mini pet in tow) is one piece, four
 * ways: a jog lifts the feet with it. A piece with no body colour in it
 * (the flag's checker) doesn't count. */
static bool body_piece(char k) { return k == 'b' || k == 'd' || k == 'm' || k == 'g' || k == 'e' || k == 'k'; }

static int pieces(const deck_pet_look_t *l, int pose)
{
	static uint8_t seen[DECK_PET_GRID_H][DECK_PET_GRID_W];
	memset(seen, 0, sizeof seen);
	static const int tow[] = {16, 16, 14, 14, 16, 17}; /* the mini pet's column, per art */
	const int tow_x = pose == DECK_PET_POSE_DELEGATE ? tow[l->art] : 999;
	int n = 0;
	FOR_GRID(x0, y0) {
		const char k = deck_pet_at(l, x0, y0);
		if (!body_piece(k) || x0 >= tow_x || seen[y0 - DECK_PET_GRID_Y0][x0 - DECK_PET_GRID_X0])
			continue;
		int stack[DECK_PET_GRID_W * DECK_PET_GRID_H][2], top = 0;
		bool coloured = false;
		stack[top][0] = x0, stack[top][1] = y0, top++;
		seen[y0 - DECK_PET_GRID_Y0][x0 - DECK_PET_GRID_X0] = 1;
		while (top) {
			top--;
			const int x = stack[top][0], y = stack[top][1];
			const char c = deck_pet_at(l, x, y);
			coloured |= c == 'b' || c == 'd' || c == 'g';
			static const int8_t d[4][2] = {{1, 0}, {-1, 0}, {0, 1}, {0, -1}};
			for (int i = 0; i < 4; i++) {
				const int nx = x + d[i][0], ny = y + d[i][1];
				const char m = deck_pet_at(l, nx, ny);
				if (!body_piece(m) || nx >= tow_x || seen[ny - DECK_PET_GRID_Y0][nx - DECK_PET_GRID_X0])
					continue;
				seen[ny - DECK_PET_GRID_Y0][nx - DECK_PET_GRID_X0] = 1;
				stack[top][0] = nx, stack[top][1] = ny, top++;
			}
		}
		n += coloured;
	}
	return n;
}

static void test_feet(void)
{
	for (int st = DECK_PET_BABY; st <= DECK_PET_LEGEND; st++)
		for (int mood = 0; mood < 3; mood++)   /* (asleep: frozen, no pose) */
			for (int hat = 0; hat <= DECK_PET_HAT_SHAPE_COUNT; hat += 3)
				for (int pose = DECK_PET_POSE_THINK; pose <= DECK_PET_POSE_HUNGRY; pose++) {
					if (pose == DECK_PET_POSE_EXIT)
						continue;
					deck_pet_t p = pet_of(st, mood, hat);
					for (uint32_t ms = 0; ms < 2400; ms += 50) {
						const deck_pet_opts_t o = posed(pose, ms);
						deck_pet_look(&L, &p, &o);
						const int n = pieces(&L, pose);
						if (n != 1)
							fprintf(stderr, "feet: stage %d mood %d hat %d pose %d at %u ms: %d pieces\n", st, mood, hat, pose, ms, n);
						CHECK(n == 1);
					}
				}
}

static void test_motion(void)
{
	int dx, dy;
	deck_pet_t p = pet_of(DECK_PET_EGG, DECK_PET_FED, 0);
	p.ready = p.cracked = true;
	deck_pet_opts_t o = posed(DECK_PET_POSE_NONE, 0);
	deck_pet_look(&L, &p, &o);
	CHECK(L.ready && L.art == DECK_PET_ART_CRACK);
	int left = 0, right = 0;
	for (uint32_t ms = 0; ms < 1600; ms += 40) {   /* whole cells: still until 60 %, then -1, +1, -1 */
		deck_pet_motion(&L, ms, 2, &dx, &dy);
		CHECK(dy == 0 && (dx == 0 || dx == -2 || dx == 2));
		CHECK((ms < 960 || ms >= 1440) == (dx == 0));
		left += dx < 0;
		right += dx > 0;
	}
	CHECK(left == 8 && right == 4);
	p.ready = false;
	deck_pet_look(&L, &p, &o);
	deck_pet_motion(&L, 250, 2, &dx, &dy);
	CHECK(dx == 0 && dy == 0);
}

int main(void)
{
	test_golden();
	test_state();
	test_stroke_and_reach();
	test_flames();
	test_face();
	test_feet();
	test_draw();
	test_motion();
	if (failures) {
		fprintf(stderr, "test_pet: %d of %d checks failed\n", failures, checks);
		return 1;
	}
	printf("test_pet: %d checks passed\n", checks);
	return 0;
}
