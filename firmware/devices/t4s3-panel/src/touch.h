// The T4-S3's touch controller: a CST226SE on I2C, in the panel's
// coordinates (portrait, 450x600).
#pragma once

#include <stdbool.h>

// Resets the controller and starts the I2C bus. false if it doesn't answer.
bool touch_init(void);

// The first touch point, if a finger is down.
bool touch_read(int *x, int *y);
