// DeckLink: the Arduino face of the Deck Link C core (deck_link.h).
//
//   DeckLink deck;
//   void setup() {
//     Serial.begin(115200);
//     DeckLinkConfig cfg = {};
//     cfg.board_id = DeckLink::chipId();
//     cfg.type = "my-device";
//     cfg.on_agent = onAgent;
//     deck.begin(Serial, cfg);
//   }
//   void loop() { deck.loop(); }
//
// Callbacks receive the core's deck_link_t*; DeckLink::from(ctx) gets the
// DeckLink back and user() your cfg.user.
#pragma once
#if defined(ARDUINO)

#include <Arduino.h>

#include "deck_link.h"
#include "deck_tiles.h"

typedef deck_link_config_t DeckLinkConfig;

class DeckLink {
public:
	// Starts the link on port (already begun at the deck's baud rate) and
	// sends Hello. cfg.write and cfg.millis are filled in for you.
	void begin(Stream &port, const DeckLinkConfig &cfg);
	// Reads what the port has, decodes it and runs the timers. Call it from loop().
	void loop();

	bool online() const { return deck_link_online(&ctx_); }
	bool approved() const { return deck_link_approved(&ctx_); }
	agentsdeck_link_v1_Scope scope() const { return deck_link_scope(&ctx_); }
	deck_link_state_t state() const { return deck_link_state(&ctx_); }
	void *user() const { return user_; }
	deck_link_t *core() { return &ctx_; }

	// Commands: return the seq (matched by on_result), or 0 if not sent.
	uint32_t getState() { return deck_get_state(&ctx_); }
	uint32_t answer(const char *agentId, const char *promptId, const char *key) { return deck_answer(&ctx_, agentId, promptId, key); }
	uint32_t focus(const char *agentId) { return deck_focus(&ctx_, agentId); }
	uint32_t interrupt(const char *agentId) { return deck_interrupt(&ctx_, agentId); }
	uint32_t dismiss(const char *agentId) { return deck_dismiss(&ctx_, agentId); }
	uint32_t resume(const char *agentId) { return deck_resume(&ctx_, agentId); }
	uint32_t hatchPet(const char *agentId) { return deck_hatch_pet(&ctx_, agentId); }
	uint32_t move(const char *agentId, uint32_t slot) { return deck_move(&ctx_, agentId, slot); }
	uint32_t moveAttention(const char *agentId, uint32_t slot) { return deck_move_attention(&ctx_, agentId, slot); }
	uint32_t order(const char *const *agentIds, size_t n) { return deck_order(&ctx_, agentIds, n); }
	uint32_t setHat(const char *agentId, const char *folder, const char *shape, const char *color, bool autoHat)
	{
		return deck_set_hat(&ctx_, agentId, folder, shape, color, autoHat);
	}
	uint32_t listHats() { return deck_list_hats(&ctx_); }
	uint32_t listLive() { return deck_list_live(&ctx_); }
	uint32_t addLive(const char *sessionId, int32_t slot = -1) { return deck_add_live(&ctx_, sessionId, slot); }
	uint32_t restore() { return deck_restore(&ctx_); }
	uint32_t subscribe(uint32_t firstSlot, uint32_t slotCount, uint32_t fields) { return deck_subscribe(&ctx_, firstSlot, slotCount, fields); }

	// The DeckLink that owns a core context (in callbacks).
	static DeckLink *from(deck_link_t *ctx) { return static_cast<DeckLink *>(deck_link_user(ctx)); }
	// A stable board id: the chip's MAC as "AA:BB:CC:DD:EE:FF" (ESP32), or "unknown".
	static const char *chipId();

private:
	static size_t writeCb(const uint8_t *data, size_t len, void *self);
	static uint32_t millisCb(void *self);

	deck_link_t ctx_;
	Stream *port_ = nullptr;
	void *user_ = nullptr;
};

#endif // ARDUINO
