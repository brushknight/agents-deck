# Pet poses

An agent's pet is the critter's new shape: it does every pose the critter does (see the header of
`clients/flutter/agents_ui/lib/src/critter.dart`), with the critter's bubbles, props and timings, drawn on
the pet's own pixel grid. This page is the spec for the browser (`backend/internal/web/static/pet-poses.js`
and `pets.js`), so the panel (Flutter) and the boards (firmware) can draw the same thing.

## The grid

- One cell is one pet pixel. x grows to the right, y grows down. The art's rows are 0 to 13.
- The browser draws the pet in a `viewBox` of x -2 to 18, y -3 to 14 (20 x 17 cells). Auras, bubbles and
  props may reach outside it; nothing clips them on a tile.
- Every shape is a whole cell, fully opaque (only the ice is see-through). Motion moves whole cells.
- Arts: `egg` (fewer than 30,000 tokens eaten, not ready), `crack` (30,000 or more, or ready), `baby`,
  `kid`, `builder` (grown, strong, mighty, legend), `weak` (a weak grown-up or higher). A weak baby or kid
  keeps its art, in the weak colour.
- Colours used by poses: body `b` #FF4D00 (weak: #B0673F), eyes `e` #141412 (the weak art's eyes and mouth
  `k` #0A0A0A), hungry mouth `m` #BDB8AD, flag light `w` #F4F0E6, flag dark `k` #0A0A0A, and `P`, the prop
  colour, #FFFFFF. Around all of it runs the stroke (see "Stroke").

## The rig

Where each pose anchors, per art. Eyes are `[x, y, w, h]`, left then right.

| art | eyes | mouth | arm rows (down, mid, up) | feet rows | feet x (still) | feet x (mid-step) | bubble box | mini in tow |
|---|---|---|---|---|---|---|---|---|
| egg, crack | none | none | none | none | none | none | 14, -4 | 16, 11 |
| baby | [5,7,1,2] [10,7,1,2] | 7, 9 | 9, 7, 3 | 12 | 4 6 9 11 | 5 7 9 11 | 13, -1 | 14, 10 |
| kid | [5,7,1,2] [10,7,1,2] | 7, 9 | 9, 7, 4 | 11, 12 | 4 6 9 11 | 3 5 10 12 | 14, 0 | 14, 10 |
| builder | [4,5,1,2] [10,5,1,2] | 7, 7 | 7, 5, 2 | 10, 11 | 2 4 11 13 | 3 5 10 12 | 16, -2 | 16, 9 |
| weak | [3,7,2,1] [9,7,2,1] | 6, 10 | 9, 7, 4 | 11 | 2 4 11 13 | 3 5 10 12 | 15, 0 | 17, 9 |

The bubble box and the mini give the top-left cell.

### Arms

Pets have no arms in their still art. An arm comes out only in poses that move it: think, run, delegate,
celebrate, error. An arm is 1 x 2 cells in the body colour. For a top row y, the left arm is one cell left
of the body's leftmost cell on rows y and y+1, the right arm one cell right of the rightmost (the pencil and
the ground don't count). That gives:

| art | down (L / R) | mid (L / R) | up (L / R) |
|---|---|---|---|
| baby | (2,9-10) / (13,9-10) | (2,7-8) / (13,7-8) | (4,3-4) / (11,3-4) |
| kid | (2,9-10) / (13,9-10) | (1,7-8) / (14,7-8) | (2,4-5) / (13,4-5) |
| builder | (-1,7-8) / (16,7-8) | (-1,5-6) / (16,5-6) | (0,2-3) / (15,2-3) |
| weak | (-1,9-10) / (16,9-10) | (-1,7-8) / (16,7-8) | (0,4-5) / (14,4-5) |

### Eyes

A pose moves the art's eyes (the cells under them become body colour) or draws a mark in their place. With
t = y for eyes 2 tall and t = y - 1 for the weak art's flat eyes, for an eye `[x, y, w, h]`:

- open: as in the art. up: one row higher. glance: up, and the whole pair moves -1 / +1 in x.
  scan: as in the art, the pair moves 0, -1, 0, +1 in x.
- shut: one row at y+h-1, from x-1 to x+w-1 (left eye) or x to x+w (right eye).
- happy "^": (x-1, t+1), x to x+w-1 on row t, (x+w, t+1).
- X: (x-1, t), (x+w, t), x to x+w-1 on row t+1, (x-1, t+2), (x+w, t+2).
- strain "> <": left (x-1, t), x to x+w-1 on row t+1, (x-1, t+2); right (x+w, t), x to x+w-1 on row t+1,
  (x+w, t+2).
- dot (exited): one cell at (x+w-1, y+h-1) left, (x, y+h-1) right, in the body colour.

For example the builder's X eyes are (3,5) (5,5) (4,6) (3,7) (5,7) and (9,5) (11,5) (10,6) (9,7) (11,7).

### The bubble

Glyphs sit in the bubble box at an offset (dx, dy) from its top-left; `#` is a cell in `P`.

| bubble | glyph rows | offset | motion |
|---|---|---|---|
| think | `#` at (0,4); `#` at (1,2); `##` `##` at (3,0) | | t1, t2, t3 |
| read | `.##..` `#..#.` `#..#.` `.##..` `....#` | 0, 0 | still |
| bash, write | `#..` `.#.` `#..` | 0, 1 | still |
| | cursor `##` | 3, 3 | cursor |
| web | `.###.` `#.#.#` `#####` `#.#.#` `.###.` / `.###.` `##.##` `#####` `##.##` `.###.` | 0, 0 | la slow / lb slow |
| plan | `#.###` on rows 0, 2, 4; a tick cell in the body colour over each `#` at x 0 | 0, 0 | tick0, tick1, tick2 |
| "…" (run, starting) | `#.#.#` | 0, 4 | dots |
| compact | `####` at rows 0, 2, 4 | 0, 0 | press2, press1, still |
| cookie (hungry) | `.###.` `##.##` `#####` `#.###` `.###.` | 0, 0 | bobc |
| "?" (waiting) | `###` `..#` `.#.` `...` `.#.` | 1, 0 | blink |
| "!" (error) | `#` `#` `#` `.` `#` | 2, 0 | still |
| "z z" (idle) | `####` `..#.` `.#..` `####` at (0,1); `###` `.#.` `###` at (4,-2) | | zz z0, zz z1 (half a cycle later) |
| sparks (an egg's celebration) | plus `.#.` `###` `.#.` at (0,2) and (2,0) | | spark s0, spark s1 |

The bubble is left out where the pet is drawn without one (the sidebar's corner), and where it would come
within a cell of the mystery hat's "?" (the egg's, in the pet's view).

### Props

- Finish flag (celebrate, hatched pets): the right arm is up; the pole is 6 cells in `P` at x = that arm's
  x + 1, rows up-4 to up+1 (builder: x 16, rows -2 to 3). The checker is 3 x 2 at x pole+1 to pole+3, rows
  up-4 to up-3: cell (cx, cy) is dark when (cx + cy + phase) is odd, light otherwise; phase 0 is `la`, 1 is
  `lb`. Two sparks (the plus above) sit at (pole+5, up-4) and (pole+3, up-8), top-left, as `spark s0` and
  `spark s1`; they hop with the pet and its flag.
- Mini pet in tow (delegate): 5 x 3 in the body colour from its top-left: `#####` `#.#.#` (it doesn't bob),
  then a feet row `#...#` (`la`) / `.#.#.` (`lb`).
- Chomping mouth (hungry): the open mouth is 2 x 2 in `m` at the mouth cell (`la slow`); the shut mouth is
  2 x 1 in `m` on its lower row (`lb slow`). The weak art opens a 3 x 1 line in `k` at (6,10) under its own
  mouth (`la slow`) and shows only its own mouth in between.

## The poses

| pose | when | eyes | arms | body motion | feet | bubble / props |
|---|---|---|---|---|---|---|
| think | running, no tool | glance | down / mid | | | think |
| read | Read, Grep, Glob, LS | scan | | | | read |
| bash, write | Bash, Edit, Write and friends | open | | jog, 0.4 s | step fast | bash |
| web | WebFetch, WebSearch | up | | | | web |
| plan | TodoWrite | open | | | | plan |
| delegate | Task, Agent | open | swing | jog | step | mini in tow |
| run | any other tool | open | swing | jog | step | "…" |
| compact | compacting | strain | | squeeze | | compact |
| celebrate | idle for under 6 s after a turn | happy | up / up | hop | with the body | flag and sparks |
| hungry | idle with an unseen result | up | | | | cookie, chomping mouth |
| wait | waiting | up | | | | "?" |
| err | error | X | up / up | | | "!" |
| start | starting | open | | | | "…" |
| exit | exited | dots | | | | hollow outline |
| idle | idle | shut | | | | "z z" |

- swing: down / mid on `la`, mid / down on `lb`.
- step: the feet cells leave the body and are drawn twice: still x on `la`, mid-step x on `lb` (with `fast`
  for bash). The feet move with the body: a jog lifts the whole pet, feet and all, so they never come
  apart from it.
- jog: the whole pet (body, hat, arms, feet, its aura) lifts 1 cell for the second half of each beat:
  `pjog` 0.5 s, `pjog pjf` 0.4 s for bash and write.
- squeeze: every body cell above row (first feet row - 2) drops 1 cell, then 2 cells, drawn over the two rows
  above the feet; the eyes and hat drop with it and stay on top (a mouth on the rows below is squeezed shut). The kid drops only 1 cell (its face
  reaches row 9, so it keeps one row, row 10, to drop over): every body cell above row 10 drops 1 cell
  (class `psq1`). An egg keeps its bottom two rows (12 and 13).
- hollow: every body cell whose four neighbours are all body cells is removed (the hat stays), the eyes become
  dots, the hat takes its shade, and there is no aura.
- A working hat (any pose from think to compact in the table, the builder and weak arts) runs through the
  hat's working frames from `HAT_ART` (`critter` frames, `ms` each). Cells that are the same in every frame
  are drawn with the body; the others one layer per frame. The kid's small hats and the devil horns have no
  frames and hold still. Every other pose shows the still hat.

### Eggs

An egg has no eyes, arms or feet. It shows the same bubbles at its bubble box, and:

- rocks while its agent works (think, read, web, plan, run, delegate: `prock`; bash and write rock twice as
  fast: `prk2`), moving 0, -1, 0, +1 cells in x;
- squeezes while compacting, hops when a turn ends (sparks, no flag), and is drawn hollow when its agent
  has exited;
- a mini pet trots beside it while it delegates.

A ready egg still wobbles a whole cell and glows (`prdy`); the pose plays on top of it. Hatching shakes
1 then 2 cells, flashes white (the egg's cells in #FFFFFF) and is gone; the new pet rises from 2 cells
below (`pborn`). These move the whole pet, aura and all.

## Which wins

From the top, the first rule that applies decides:

1. A drawing that holds still (stage previews, the ready-egg list, the dark shapes of stages ahead): no pose.
2. Hatching (the burst) and waking (the ice cracking off, 2 s): no pose until it ends.
3. Hibernating (the ice): the pet stays frozen, eyes shut, no motion. Only waiting and error show their "?"
   and "!" beside the cube, so an agent that needs you still says so.
4. Hungry for food (the pet's mood) and idle: it stays awake, eyes open and mouth open, no "z z".
5. Otherwise the agent's pose plays in full. A pet hungry for food keeps its 2 x 2 grey mouth in every pose
   (the chomping mouth replaces it in the hungry pose); a weak pet keeps its weak art and colour, slumped on
   its ground line, and does the pose there.
6. An exited agent's pet has no aura.

## Timings

The classes are the critter's (`settings.css`, the deck's critters), except where it moves by part of a cell:
those have whole-cell versions for pets (`pjog`, `hop`, `bobc`, `zz`); the critter's own keyframes stay as
they were. Nothing on a pet moves by part of a cell, scales or glides: things that travel step one cell at
a time. Each runs forever unless noted.

| class | period | what |
|---|---|---|
| la / lb | 0.5 s | la shows for the first half, lb for the second |
| fast | 0.2 s | la and lb at this speed (bash's feet) |
| slow | 0.8 s | la and lb at this speed |
| pjog | 0.5 s | 0, then -1 cell in y for the second half (the whole pet) |
| pjf | 0.4 s | pjog at this speed (bash, write) |
| hop | 0.4 s | 0, then -2 cells in y for the second half |
| glance | 1.2 s | -1 cell in x, then +1 |
| scan | 1.2 s | 0, -1, 0, +1 cell in x, a quarter each |
| t1 / t2 / t3 | 1.2 s | t1 always; t2 from 25 %; t3 from 50 % |
| cursor | 1 s | on, then off, half each |
| tick0 / tick1 / tick2 | 2 s | each shows until 25 / 50 / 75 % |
| dots | 1.2 s | opacity 0.2, 1, 0.2 in 3 steps |
| blink | 1 s | opacity 1 for 60 %, then 0.15 |
| spark | 0.6 s | opacity 1 for the first third, then 0.45; s1 starts 0.2 s later |
| bobc | 0.6 s | 0, then +1 cell in y |
| zz | 2.4 s | 0 at opacity 0.9, then -1 cell at 0.45; z1 half a cycle later |
| prock | 1.2 s | 0, -1, 0, +1 cell in x, a quarter each |
| prk2 | 0.6 s | prock at this speed |
| prdy | 1.6 s | ready egg: 0 until 60 %, then -1, +1, -1 cell in x, a tenth each |
| phx | 1.4 s, once | hatch: -1, +1, -1, +1, -2, +2 cells in x, a tenth each; white from 70 %; gone at 85 % |
| pborn | 0.6 s, once | +2 cells in y hidden, +2 shown, -1, then in place, a quarter each |
| pfc | 1.8 s | the second flame layer: off, then on, half each |
| prs | 2.4 s | sparks: up 1 cell per step, 9 steps; shown from step 1 to 7 |
| ptw | 1.1 s | a legend's star: a dot, then the dot and its 4 arms, half each |
| pfall | 2.6 s | snow: from 3 cells above down 1 cell a step, 12 steps; shown from 17 % to 85 % |
| ps0 to ps4 | 1.6 s, once | ice shards: from 56 %, 2 cells a step at 70, 85 and 100 % (out to 6 cells), gone at the end |
| psq | 1.2 s | 0, +1, +2 cells in y, a third each (springs back at the loop) |
| psq1 | 1.2 s | 0 for a third, then +1 cell in y (the kid's squeeze) |
| press2 / press1 | 1.2 s | press2 shows for the first third, press1 for two thirds, the last bar always |
| hf-N-ms-i | N x ms | hat frame i shows for its ms slot |

## Stroke

Every pet has a black (#000000) outline one cell wide: each opaque cell of each layer (body, hat and every
hat frame, arms, eyes, mouth, props, bubbles, the mini pet) also paints its 4 neighbours (up, down, left,
right) in black, underneath the pet. So every extent in this spec grows by 1 cell on each side: an egg
spanning x 1 to 14 is drawn from x 0 to 15, a bubble box 5 x 5 becomes 7 x 7, and so on.

- Each outline belongs to its layer and moves with that layer's motion (jog, hop, rock, squeeze, glance, a
  hat frame, la / lb, the bubble's own class). A layer that is hidden (an lb frame, a hat frame not in its
  slot) hides its outline too.
- Inside the pet the outline is covered by the pet itself; it shows only at the outer edges and in the
  gaps (between the feet, between a raised arm and the head, inside a hollow outline).
- Nothing see-through gets a stroke (the ice), and neither do auras, sparks or stars. A silhouette (a stage
  not reached yet) has none.
- Within the pet, the body cells are painted first and every layer that moves on its own (the glancing and
  scanning eyes, the chomping mouth, arms, the flag, hat frames) after them, so nothing of the body covers
  the face; when squeezed, the rows it drops over are painted first and the dropping part after them.
- Layers back to front: aura flames, the strokes, the pet (body, hat, eyes, arms, flag), its bubble and props
  in white, the aura's sparks and the legend's stars, and when it hibernates the ice over everything. The
  aura (flames, sparks, stars) moves with the pet's motion, so the pet never slides over its flames.

The stroke replaces the old aura rules: hats keep their own colours inside any aura, props are always white,
and flames no longer keep a gap from the hat or the props (the black outline separates them).

## On orange

Where the background is orange (a tile that needs you, the sidebar), the pet keeps its own colours; the
stroke is what keeps it apart from the orange. There is no plate behind it and no other change.

## Motion budget

- All motion is CSS on a handful of paths: each colour of each moving layer is one path, rows of cells
  joined into rectangles, and the stroke is one path per motion class; every moving instance has its own class (the two zs are z0
  and z1), so each outline follows its own layer. A posed pet stays under 96 shapes.
- A pet off screen holds still: its animations pause until it scrolls back into view.
- With reduced motion nothing moves: the first frame of each pair shows (`la`, hat frame 0), every bubble
  stays up.
