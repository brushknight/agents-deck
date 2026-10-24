idf_component_register(
  SRCS "main.c" "display.c" "touch.c" "agents.c" "link.c" "ui.c" "debug.c" "critter.c" "hats.c"
  INCLUDE_DIRS "."
  REQUIRES deck-link driver esp_timer esp_driver_usb_serial_jtag
)
target_compile_options(${COMPONENT_LIB} PRIVATE -Wall -Wextra)
