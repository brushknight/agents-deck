// The panel's screens, in LVGL: six agents per page in a 2x3 grid, an
// agent's card when you tap one, and the link's state when there's no deck.
// Call everything from the UI task.
#pragma once

void ui_init(void);

// Redraws what changed in the agents state since the last call.
void ui_update(void);
