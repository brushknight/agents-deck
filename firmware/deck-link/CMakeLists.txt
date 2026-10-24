# ESP-IDF component: the C core only (the Arduino wrapper, DeckLink.cpp, is
# for Arduino builds). Put this folder in your project's components/ (or
# point EXTRA_COMPONENT_DIRS at it) and include "deck_link.h".
idf_component_register(
  SRCS
    "src/deck_link.c"
    "src/deck_tiles.c"
    "src/cobs.c"
    "src/crc16.c"
    "src/deck_store.c"
    "src/deck_pair.c"
    "src/deck_tls.c"
    "src/proto/link.pb.c"
    "src/proto/pb_common.c"
    "src/proto/pb_encode.c"
    "src/proto/pb_decode.c"
  INCLUDE_DIRS "src" "src/proto"
  # Pairing and Wi-Fi (deck_store, deck_pair, deck_tls): NVS, mbedTLS, sockets.
  # TLS 1.3 needs CONFIG_MBEDTLS_SSL_PROTO_TLS1_3=y; without it they are stubs.
  PRIV_REQUIRES nvs_flash mbedtls lwip bootloader_support
)
