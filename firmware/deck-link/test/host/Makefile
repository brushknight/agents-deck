# Host tests for the Deck Link core (Apple clang or gcc).
#   make test       build and run the unit tests
#   make board_sim  build the board simulator
#   make test_tls   the pairing tests with mbedTLS 3 (Homebrew's mbedtls, or MBEDTLS=<prefix>)
#   make board_wifi a board that pairs over serial and connects over TLS 1.3
SRC := ../../src
CC ?= cc
CFLAGS ?= -std=c99 -O1 -g -Wall -Wextra -Werror=implicit-function-declaration
CPPFLAGS += -I$(SRC) -D_POSIX_C_SOURCE=200809L
# The plain tests build without Wi-Fi (no mbedTLS); test_tls and board_wifi
# (below) build the same sources with mbedTLS 3 and TLS 1.3.
TEST_CPPFLAGS := -DDECK_LINK_NO_WIFI
CORE := $(SRC)/deck_link.c $(SRC)/deck_tiles.c $(SRC)/cobs.c $(SRC)/crc16.c \
	$(SRC)/deck_store.c $(SRC)/deck_pair.c $(SRC)/deck_tls.c \
	$(SRC)/proto/link.pb.c $(SRC)/proto/pb_common.c $(SRC)/proto/pb_encode.c $(SRC)/proto/pb_decode.c
HDRS := $(wildcard $(SRC)/*.h $(SRC)/proto/*.h)
OUT := build

all: $(OUT)/test_link $(OUT)/board_sim

test: $(OUT)/test_link
	./$(OUT)/test_link

board_sim: $(OUT)/board_sim

$(OUT)/test_link: test_link.c $(CORE) $(HDRS) | $(OUT)
	$(CC) $(CPPFLAGS) $(TEST_CPPFLAGS) $(CFLAGS) -o $@ test_link.c $(CORE)

$(OUT)/board_sim: board_sim.c $(CORE) $(HDRS) | $(OUT)
	$(CC) $(CPPFLAGS) $(CFLAGS) -o $@ board_sim.c $(CORE)

# With mbedTLS 3 (TLS 1.3): the pairing tests and board_wifi, a board that
# pairs and then connects over TLS (backend/internal/link's Go tests run it).
MBEDTLS ?= $(shell brew --prefix mbedtls 2>/dev/null || echo /usr/local)
TLS_FLAGS := -I$(MBEDTLS)/include
TLS_LIBS := -L$(MBEDTLS)/lib -lmbedtls -lmbedx509 -lmbedcrypto

test_tls: $(OUT)/test_tls
	./$(OUT)/test_tls

board_wifi: $(OUT)/board_wifi

$(OUT)/test_tls: test_tls.c $(CORE) $(HDRS) | $(OUT)
	$(CC) $(CPPFLAGS) $(TLS_FLAGS) $(CFLAGS) -o $@ test_tls.c $(CORE) $(TLS_LIBS)

$(OUT)/board_wifi: board_wifi.c $(CORE) $(HDRS) | $(OUT)
	$(CC) $(CPPFLAGS) $(TLS_FLAGS) $(CFLAGS) -o $@ board_wifi.c $(CORE) $(TLS_LIBS)

$(OUT):
	mkdir -p $@

clean:
	rm -rf $(OUT)

.PHONY: all test board_sim test_tls board_wifi clean
