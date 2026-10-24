// A port of spriteSVG() (backend/internal/web/static/app.js). Shapes are in
// sprite cells; a pen maps them to pixels through the group transforms the
// SVG uses (the bob, the hop, the squash, the mini critter). The CSS
// animations (app.css) become functions of the time.
#include "critter.h"

#include <math.h>
#include <string.h>

typedef struct {
	lv_layer_t *layer;
	float px, py, cell; // pixel position of cell (0, 0), pixels per cell
	float sx, sy, tx, ty; // the group transform, in cells
	lv_opa_t opa;
} pen_t;

// Colours a shape can take: the body, the background cut out of it, the
// flag's light squares, the error's accent.
typedef enum { K_BODY, K_CUT, K_FLAG, K_ACCENT } kind_t;

typedef struct {
	const critter_t *c;
	uint32_t ms;
} ctx_t;

static lv_color_t kind_color(const critter_t *c, kind_t k)
{
	switch (k) {
	case K_CUT: return lv_color_hex(c->cut);
	case K_FLAG: return lv_color_hex(0xE8E4DC);
	case K_ACCENT: return lv_color_hex(0xFF4D00);
	default: return lv_color_hex(c->body);
	}
}

static void fill(pen_t *p, float x, float y, float w, float h, lv_color_t col)
{
	if (p->opa == 0)
		return;
	float x1 = p->tx + p->sx * x, y1 = p->ty + p->sy * y;
	float x2 = p->tx + p->sx * (x + w), y2 = p->ty + p->sy * (y + h);
	lv_area_t a = {
		.x1 = (int32_t)lroundf(p->px + x1 * p->cell),
		.y1 = (int32_t)lroundf(p->py + y1 * p->cell),
		.x2 = (int32_t)lroundf(p->px + x2 * p->cell) - 1,
		.y2 = (int32_t)lroundf(p->py + y2 * p->cell) - 1,
	};
	if (a.x2 < a.x1)
		a.x2 = a.x1;
	if (a.y2 < a.y1)
		a.y2 = a.y1;
	lv_draw_rect_dsc_t d;
	lv_draw_rect_dsc_init(&d);
	d.bg_color = col;
	d.bg_opa = p->opa;
	d.radius = 0;
	lv_draw_rect(p->layer, &d, &a);
}

static void rect(pen_t *p, const ctx_t *x, float rx, float ry, float w, float h, kind_t k)
{
	fill(p, rx, ry, w, h, kind_color(x->c, k));
}

// A stroked polyline (the "x", "^" and "><" eyes), in the cut colour.
static void stroke(pen_t *p, const ctx_t *x, const float *pts, int n, float width)
{
	lv_draw_line_dsc_t d;
	lv_draw_line_dsc_init(&d);
	d.color = kind_color(x->c, K_CUT);
	d.width = width * p->cell * fabsf(p->sx);
	d.opa = p->opa;
	d.round_start = d.round_end = 0;
	for (int i = 0; i + 1 < n; i++) {
		d.p1.x = p->px + (p->tx + p->sx * pts[2 * i]) * p->cell;
		d.p1.y = p->py + (p->ty + p->sy * pts[2 * i + 1]) * p->cell;
		d.p2.x = p->px + (p->tx + p->sx * pts[2 * i + 2]) * p->cell;
		d.p2.y = p->py + (p->ty + p->sy * pts[2 * i + 3]) * p->cell;
		lv_draw_line(p->layer, &d);
	}
}

// Shifts the group by (dx, dy) cells.
static pen_t moved(pen_t p, float dx, float dy)
{
	p.tx += p.sx * dx;
	p.ty += p.sy * dy;
	return p;
}

// ---- timing (app.css) -----------------------------------------------------

// Where we are in a cycle of `period` ms, 0..1.
static float phase(const ctx_t *x, uint32_t period, uint32_t delay)
{
	return (float)((x->ms + delay) % period) / (float)period;
}

static bool la_on(const ctx_t *x, uint32_t period) { return phase(x, period, 0) < 0.5f; }

// ---- the shapes -----------------------------------------------------------

static const float LEGS_A[4] = {2, 4, 9, 11};
static const float LEGS_B[4] = {3, 5, 8, 10};

static void critter_legs(pen_t *p, const ctx_t *x, const float *xs)
{
	for (int i = 0; i < 4; i++)
		rect(p, x, xs[i], 6, 1, 1.6f, K_BODY);
}

static void bot_feet(pen_t *p, const ctx_t *x, bool b)
{
	rect(p, x, 3.5f, b ? 6.4f : 7, 2, 1.4f, K_BODY);
	rect(p, x, 8.5f, 7, 2, 1.4f, K_BODY);
}

// Legs or feet, still (which = 0: A) or stepping with period ms.
static void feet(pen_t *p, const ctx_t *x, uint32_t period)
{
	bool a = period == 0 || la_on(x, period);
	if (x->c->onigiri)
		bot_feet(p, x, !a);
	else
		critter_legs(p, x, a ? LEGS_A : LEGS_B);
}

// The onigiri: rows of the rice triangle (y from, y to, x from, x to).
static const float BOT_ROWS[][4] = {{-3, -2, 6, 8}, {-2, -1, 5, 9}, {-1, 0, 4, 10}, {0, 1, 3, 11},
				    {1, 3, 2, 12}, {3, 5, 1, 13},  {5, 6, 0, 14},  {6, 7, 1, 13}};
static const float BOT_HOLLOW[][4] = {{-1, 0, 6, 8}, {0, 1, 5, 9}, {1, 3, 4, 10}, {3, 5, 3, 11}, {5, 6, 2, 12}};

static void body(pen_t *p, const ctx_t *x, bool exit)
{
	if (!x->c->onigiri) {
		rect(p, x, 1, 0, 12, 6, K_BODY);
		if (exit)
			rect(p, x, 2, 1, 10, 4, K_CUT);
		return;
	}
	for (size_t i = 0; i < sizeof BOT_ROWS / sizeof BOT_ROWS[0]; i++)
		rect(p, x, BOT_ROWS[i][2], BOT_ROWS[i][0], BOT_ROWS[i][3] - BOT_ROWS[i][2], BOT_ROWS[i][1] - BOT_ROWS[i][0], K_BODY);
	if (exit) {
		for (size_t i = 0; i < sizeof BOT_HOLLOW / sizeof BOT_HOLLOW[0]; i++)
			rect(p, x, BOT_HOLLOW[i][2], BOT_HOLLOW[i][0], BOT_HOLLOW[i][3] - BOT_HOLLOW[i][2],
			     BOT_HOLLOW[i][1] - BOT_HOLLOW[i][0], K_CUT);
	} else {
		rect(p, x, 4, 5.4f, 6, 1.6f, K_CUT); // the nori
	}
}

// Arms beside the body, at their top row l (left) and r (right).
static void arms(pen_t *p, const ctx_t *x, float l, float r)
{
	if (x->c->onigiri) {
		rect(p, x, 1, l - 1, 1, 2, K_BODY);
		rect(p, x, 12, r - 1, 1, 2, K_BODY);
	} else {
		rect(p, x, 0, l, 1, 2, K_BODY);
		rect(p, x, 13, r, 1, 2, K_BODY);
	}
}

static void eyes_open_at(pen_t *p, const ctx_t *x, float y, float h)
{
	rect(p, x, 4, y, 1, h, K_CUT);
	rect(p, x, 9, y, 1, h, K_CUT);
}

// Bitmap rows ('#' cells) of cell size w x h from (x0, y0).
static void pixels(pen_t *p, const ctx_t *x, const char *const *rows, int n, float x0, float w, float h, float y0,
		   kind_t k)
{
	for (int r = 0; r < n; r++)
		for (int c = 0; rows[r][c]; c++)
			if (rows[r][c] == '#')
				rect(p, x, x0 + c * w, y0 + r * h, w, h, k);
}

static void dots(pen_t *p, const ctx_t *x)
{
	static const lv_opa_t steps[3] = {51, 153, 255}; // .2 → 1, in three steps
	pen_t q = *p;
	q.opa = (lv_opa_t)(p->opa * steps[(int)(phase(x, 1200, 0) * 3)] / 255);
	rect(&q, x, 14.6f, -2.6f, 0.9f, 0.9f, K_BODY);
	rect(&q, x, 15.9f, -2.6f, 0.9f, 0.9f, K_BODY);
	rect(&q, x, 17.2f, -2.6f, 0.8f, 0.9f, K_BODY);
}

// The little critter a delegating agent shows (MINI in app.js).
static void mini(pen_t *p, const ctx_t *x, float ox, float oy, float s)
{
	pen_t q = *p;
	q.tx += q.sx * ox;
	q.ty += q.sy * oy;
	q.sx *= s;
	q.sy *= s;
	critter_t plain = *x->c;
	plain.onigiri = false;
	ctx_t mx = {&plain, x->ms};
	pen_t b = la_on(&mx, 500) ? q : moved(q, 0, -0.5f);
	rect(&b, &mx, 1, 0, 12, 6, K_BODY);
	rect(&b, &mx, 0, 2, 1, 2, K_BODY);
	rect(&b, &mx, 13, 2, 1, 2, K_BODY);
	rect(&b, &mx, 4, 1.5f, 1, 2, K_CUT);
	rect(&b, &mx, 9, 1.5f, 1, 2, K_CUT);
	critter_legs(&q, &mx, la_on(&mx, 500) ? LEGS_A : LEGS_B);
}

static void draw_z(pen_t *p, const ctx_t *x, float zx, float zy, const lv_font_t *font)
{
	lv_draw_label_dsc_t d;
	lv_draw_label_dsc_init(&d);
	d.color = kind_color(x->c, K_BODY);
	d.font = font;
	d.text = "z";
	d.opa = p->opa;
	int32_t px = (int32_t)(p->px + (p->tx + p->sx * zx) * p->cell);
	int32_t py = (int32_t)(p->py + (p->ty + p->sy * zy) * p->cell) - lv_font_get_line_height(font) * 3 / 4;
	lv_area_t a = {px, py, px + 30, py + lv_font_get_line_height(font)};
	lv_draw_label(p->layer, &d, &a);
}

// ---- hats -----------------------------------------------------------------

static const struct {
	const char *name;
	uint32_t main, shade;
} HAT_COLORS[] = {{"teal", 0x4FB3A6, 0x2F7F75}, {"blue", 0x6F8FD8, 0x4A63A3}, {"yellow", 0xE8B94A, 0xB08429},
		  {"green", 0x6DBE6A, 0x468A44}, {"purple", 0xA68BD8, 0x7660A6}, {"pink", 0xE58BB0, 0xB05E80},
		  {"sky", 0x5EC4D6, 0x3A8E9C},   {"ink", 0xE8E4DC, 0xA8A39A}};

bool critter_hat_color(const char *name, uint32_t *main, uint32_t *shade)
{
	for (size_t i = 0; i < sizeof HAT_COLORS / sizeof HAT_COLORS[0]; i++)
		if (name && strcmp(name, HAT_COLORS[i].name) == 0) {
			*main = HAT_COLORS[i].main;
			*shade = HAT_COLORS[i].shade;
			return true;
		}
	*main = HAT_COLORS[7].main;
	*shade = HAT_COLORS[7].shade;
	return false;
}

static uint32_t shade_of(uint32_t main)
{
	for (size_t i = 0; i < sizeof HAT_COLORS / sizeof HAT_COLORS[0]; i++)
		if (HAT_COLORS[i].main == main)
			return HAT_COLORS[i].shade;
	return main;
}

static void hat_frame(pen_t *p, const hat_frame_t *f, uint32_t main, uint32_t shade)
{
	for (int r = 0; r < f->nrows; r++)
		for (int c = 0; f->rows[r][c]; c++) {
			char k = f->rows[r][c];
			uint32_t col = k == 'a' ? main : k == 'b' ? shade : k == 'c' ? 0xF7F4EC : k == 'd' ? 0x0A0A0A : 0;
			if (k == 'a' || k == 'b' || k == 'c' || k == 'd')
				fill(p, f->x + c, f->top + r, 1, 1, lv_color_hex(col));
		}
	for (int i = 0; i < f->nextras; i++) {
		char k = f->extras[i].key;
		uint32_t col = k == 'a' ? main : k == 'b' ? shade : k == 'c' ? 0xF7F4EC : 0x0A0A0A;
		fill(p, f->extras[i].x, f->extras[i].y, 1, 1, lv_color_hex(col));
	}
}

static void hat(pen_t *p, const ctx_t *x, bool faded, bool animate)
{
	const critter_t *c = x->c;
	if (!c->hat || !c->hat[0])
		return;
	const hat_art_t *art = NULL;
	for (int i = 0; i < hat_art_count; i++)
		if (strcmp(hat_art[i].name, c->hat) == 0)
			art = &hat_art[i];
	if (!art)
		return;
	uint32_t main = c->hat_color, shade = shade_of(main);
	pen_t q = *p;
	if (faded)
		q.opa = (lv_opa_t)(p->opa * 45 / 100);
	const hat_frame_t *f;
	if (animate) {
		int n = c->onigiri ? art->nonigiri : art->ncritter;
		int i = (int)((x->ms / art->ms) % n);
		f = c->onigiri ? &art->onigiri[i] : &art->critter[i];
	} else {
		f = c->onigiri ? &art->onigiri_still : &art->critter_still;
	}
	hat_frame(&q, f, main, shade);
	if (strcmp(art->name, "headphones") == 0) {
		lv_color_t s = lv_color_hex(shade);
		if (c->onigiri) {
			fill(&q, 2, -0.5f, 1, 2, s);
			fill(&q, 11, -0.5f, 1, 2, s);
		} else {
			fill(&q, 0, 0, 1, 2, s);
			fill(&q, 13, 0, 1, 2, s);
		}
	}
}

// ---- the sprite -----------------------------------------------------------

static bool working(pose_t pose)
{
	switch (pose) {
	case POSE_RUN:
	case POSE_THINK:
	case POSE_READ:
	case POSE_WRITE:
	case POSE_BASH:
	case POSE_WEB:
	case POSE_PLAN:
	case POSE_DELEGATE:
	case POSE_COMPACT: return true;
	default: return false;
	}
}

void critter_draw(lv_layer_t *layer, const critter_t *c, int32_t x0, int32_t y0, float cell, uint32_t ms)
{
	ctx_t x = {c, ms};
	pen_t root = {layer, (float)x0, (float)y0 + 4 * cell, cell, 1, 1, 0, 0, LV_OPA_COVER};
	pose_t pose = c->pose == POSE_WRITE ? POSE_BASH : c->pose; // writing looks like bash for now
	bool bot = c->onigiri;

	pen_t g = root; // the body group: bob, hop, squash move it
	uint32_t step = 0;  // legs stepping period (0: still)
	enum { F_OPEN2, F_OPEN1, F_GLANCE, F_SCAN, F_WEB, F_HUNGRY, F_CELEBRATE, F_ERR, F_COMPACT, F_EXIT, F_SHUT } face;
	float al = 2, ar = 2;  // arm rows
	bool arms_step = false; // run, delegate: the arms swing with the legs
	bool no_arms = false, no_feet = false;

	switch (pose) {
	case POSE_THINK: face = F_GLANCE, ar = 1; break;
	case POSE_READ: face = F_SCAN; break;
	case POSE_BASH:
		face = F_OPEN2, step = 200;
		if (!la_on(&x, 200))
			g = moved(g, 0, -0.5f);
		break;
	case POSE_WEB: face = F_WEB; break;
	case POSE_PLAN: face = F_OPEN2; break;
	case POSE_DELEGATE:
	case POSE_RUN:
		face = F_OPEN2, step = 500, arms_step = true;
		if (!la_on(&x, 500))
			g = moved(g, 0, -0.5f);
		break;
	case POSE_HUNGRY: face = F_HUNGRY; break;
	case POSE_CELEBRATE:
		face = F_CELEBRATE, al = 0, ar = 0;
		if (!la_on(&x, 400))
			g = moved(g, 0, -1.5f);
		break;
	case POSE_WAIT: face = F_OPEN1; break;
	case POSE_ERR: face = F_ERR, al = 0, ar = 0; break;
	case POSE_START: face = F_OPEN2; break;
	case POSE_COMPACT: {
		face = F_COMPACT;
		int q = (int)(phase(&x, 1200, 0) * 4);
		float sx = q == 0 ? 1 : q == 2 ? 1.2f : 1.1f, sy = q == 0 ? 1 : q == 2 ? 0.62f : 0.8f;
		// scale around cell (7, 7)
		g.tx += g.sx * 7 * (1 - sx);
		g.ty += g.sy * 7 * (1 - sy);
		g.sx *= sx;
		g.sy *= sy;
		break;
	}
	case POSE_EXIT: face = F_EXIT, no_arms = true, no_feet = !bot; break;
	default: face = F_SHUT; break;
	}

	// The body group.
	body(&g, &x, pose == POSE_EXIT);
	if (!no_arms) {
		if (arms_step) {
			if (la_on(&x, 500))
				arms(&g, &x, 2, 1);
			else
				arms(&g, &x, 1, 2);
		} else {
			arms(&g, &x, al, ar);
		}
	}
	pen_t f = bot ? g : moved(g, 0, -0.5f); // the block's face sits half a row higher
	switch (face) {
	case F_OPEN2: eyes_open_at(&f, &x, 2, 2); break;
	case F_OPEN1: eyes_open_at(&f, &x, 1, 2); break;
	case F_GLANCE: {
		pen_t e = moved(f, la_on(&x, 1200) ? -1 : 1, 0);
		eyes_open_at(&e, &x, 1, 1.5f);
		break;
	}
	case F_SCAN: {
		static const float dx[4] = {0, -1, 0, 1};
		pen_t e = moved(f, dx[(int)(phase(&x, 1200, 0) * 4)], 0);
		eyes_open_at(&e, &x, 2, 2);
		break;
	}
	case F_WEB:
		rect(&f, &x, 4.4f, 1, 1, 2, K_CUT);
		rect(&f, &x, 9.4f, 1, 1, 2, K_CUT);
		break;
	case F_HUNGRY: {
		rect(&f, &x, 4.6f, 1, 1, 2, K_CUT);
		rect(&f, &x, 9.6f, 1, 1, 2, K_CUT);
		bool open = la_on(&x, 800);
		if (bot)
			rect(&f, &x, 6.2f, open ? 3.4f : 4, 1.6f, open ? 1.4f : 0.5f, K_CUT);
		else
			rect(&f, &x, 6.2f, open ? 4.2f : 4.8f, 2.2f, open ? 1.6f : 0.5f, K_CUT);
		break;
	}
	case F_CELEBRATE:
		for (int i = 0; i < 2; i++) {
			float ex = i ? 9.5f : 4.5f;
			float pts[] = {ex - 1, 3.3f, ex, 2.2f, ex + 1, 3.3f};
			stroke(&f, &x, pts, 3, 0.55f);
		}
		break;
	case F_ERR:
		for (int i = 0; i < 2; i++) {
			float ex = i ? 8.8f : 3.8f;
			float a[] = {ex - 0.4f, 1.8f, ex + 1.4f, 4.0f};
			float b[] = {ex + 1.4f, 1.8f, ex - 0.4f, 4.0f};
			stroke(&f, &x, a, 2, 0.55f);
			stroke(&f, &x, b, 2, 0.55f);
		}
		break;
	case F_COMPACT: {
		float l[] = {3.6f, 1.9f, 4.8f, 2.7f, 3.6f, 3.5f};
		float r[] = {10.4f, 1.9f, 9.2f, 2.7f, 10.4f, 3.5f};
		stroke(&f, &x, l, 3, 0.55f);
		stroke(&f, &x, r, 3, 0.55f);
		break;
	}
	case F_EXIT:
		rect(&f, &x, 4, 3, 1, 1, K_BODY);
		rect(&f, &x, 9, 3, 1, 1, K_BODY);
		break;
	case F_SHUT:
		rect(&f, &x, 3.5f, 3.2f, 2, 0.55f, K_CUT);
		rect(&f, &x, 8.5f, 3.2f, 2, 0.55f, K_CUT);
		break;
	}
	hat(&g, &x, pose == POSE_EXIT, working(c->pose));
	if (pose == POSE_CELEBRATE) { // the finish flag in the raised hand, waving
		rect(&g, &x, 14, -4, 1, 6, K_FLAG);
		int ph = la_on(&x, 500) ? 0 : 1;
		for (int cx = 0; cx < 3; cx++)
			for (int cy = 0; cy < 2; cy++)
				rect(&g, &x, 15 + cx, -4 + cy, 1, 1, (cx + cy + ph) % 2 ? K_CUT : K_FLAG);
	}

	// Feet, outside the body group (the hop moves them with it).
	if (!no_feet) {
		pen_t ft = pose == POSE_CELEBRATE ? g : root;
		feet(&ft, &x, step);
	}

	// The bubble beside it.
	if (!c->bubble)
		return;
	pen_t b = root;
	switch (pose) {
	case POSE_THINK: {
		float pp = phase(&x, 1200, 0);
		rect(&b, &x, 14.3f, -0.9f, 0.6f, 0.6f, K_BODY);
		if (pp >= 0.25f)
			rect(&b, &x, 15.3f, -2.1f, 0.9f, 0.9f, K_BODY);
		if (pp >= 0.5f)
			rect(&b, &x, 16.5f, -3.8f, 1.3f, 1.3f, K_BODY);
		break;
	}
	case POSE_READ: {
		static const char *const r[] = {".##..", "#..#.", "#..#.", ".##..", "....#"};
		pixels(&b, &x, r, 5, 14.6f, 0.75f, 0.75f, -4, K_BODY);
		break;
	}
	case POSE_BASH: {
		static const char *const r[] = {"#..", ".#.", "#.."};
		pixels(&b, &x, r, 3, 14.5f, 0.7f, 0.8f, -3.7f, K_BODY);
		if (la_on(&x, 1000))
			rect(&b, &x, 16.5f, -1.7f, 1.2f, 0.5f, K_BODY);
		break;
	}
	case POSE_WEB: {
		static const char *const a[] = {".###.", "#.#.#", "#####", "#.#.#", ".###."};
		static const char *const z[] = {".###.", "##.##", "#####", "##.##", ".###."};
		pixels(&b, &x, la_on(&x, 800) ? a : z, 5, 14.4f, 0.72f, 0.72f, -4.2f, K_BODY);
		break;
	}
	case POSE_PLAN: {
		float pp = phase(&x, 2000, 0);
		for (int i = 0; i < 3; i++) {
			float y = -3.9f + i * 1.25f;
			rect(&b, &x, 14.4f, y, 0.9f, 0.9f, K_BODY);
			if (pp < (i + 1) * 0.25f)
				rect(&b, &x, 14.6f, y + 0.2f, 0.5f, 0.5f, K_CUT);
			rect(&b, &x, 15.6f, y + 0.25f, 2.2f, 0.45f, K_BODY);
		}
		break;
	}
	case POSE_DELEGATE: mini(&b, &x, 14.4f, 3.2f, 0.26f); break;
	case POSE_HUNGRY: {
		static const char *const r[] = {".###.", "##.##", "#####", "#.###", ".###."};
		pen_t cb = la_on(&x, 600) ? b : moved(b, 0, 0.6f);
		pixels(&cb, &x, r, 5, 14.3f, 0.75f, 0.75f, -4.2f, K_BODY);
		break;
	}
	case POSE_CELEBRATE:
		for (int i = 0; i < 2; i++) {
			pen_t s = b;
			if (phase(&x, 600, i ? 400 : 0) >= 0.34f)
				s.opa = 115;
			float sx = i ? 18.2f : 16.4f, sy = i ? -4.4f : -6.2f;
			rect(&s, &x, sx - 0.5f, sy, 1.5f, 0.5f, K_BODY);
			rect(&s, &x, sx, sy - 0.5f, 0.5f, 1.5f, K_BODY);
		}
		break;
	case POSE_RUN:
	case POSE_START: dots(&b, &x); break;
	case POSE_WAIT: {
		static const char *const r[] = {"###", "..#", ".#.", "...", ".#."};
		pen_t q = b;
		if (phase(&x, 1000, 0) >= 0.6f)
			q.opa = 38;
		pixels(&q, &x, r, 5, 14.5f, 1.1f, 0.9f, -4, K_BODY);
		break;
	}
	case POSE_ERR: {
		static const char *const r[] = {"#", "#", "#", ".", "#"};
		pixels(&b, &x, r, 5, 15.6f, 1.2f, 0.9f, -4, K_ACCENT);
		break;
	}
	case POSE_COMPACT: {
		int q = (int)(phase(&x, 1200, 0) * 4);
		float p1 = q == 0 ? 0 : q == 2 ? 1.1f : 0.4f, p2 = q == 0 ? 0 : q == 2 ? 2.2f : 0.8f;
		rect(&b, &x, 14.6f, -3.8f + p2, 3.2f, 0.6f, K_BODY);
		rect(&b, &x, 14.6f, -2.7f + p1, 3.2f, 0.6f, K_BODY);
		rect(&b, &x, 14.6f, -1.6f, 3.2f, 0.6f, K_BODY);
		break;
	}
	case POSE_EXIT: break;
	default: { // asleep: two z's drifting up
		static const float dy[4] = {0, -0.3f, -0.6f, -0.3f};
		static const lv_opa_t op[4] = {230, 166, 102, 166};
		int k = (int)(phase(&x, 2400, 0) * 4);
		pen_t z = moved(b, 0, dy[k]);
		z.opa = op[k];
		draw_z(&z, &x, 14.8f, -0.6f, &lv_font_montserrat_20);
		draw_z(&z, &x, 16.6f, -2.4f, &lv_font_montserrat_16);
	}
	}
}
