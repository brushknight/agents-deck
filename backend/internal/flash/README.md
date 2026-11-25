# flash

Writes a firmware image to an ESP32-S3 over USB through the chip's ROM serial bootloader, in plain Go:
no esptool, no Python, no cgo. `agentctl flash <port> <image.bin> [--offset 0x0] [--probe]` uses it.

## What it does

- **Reset into download mode.** Two sequences, both from esptool: the classic DTR/RTS pulse for boards
  with a USB serial chip (CH340/CH9102, CP210x, FTDI) wired to EN and IO0, tried with a short and a long
  IO0 hold; and the USB-Serial-JTAG sequence for the S3's own USB (303a:1001). The port's USB ids pick
  the sequence; unknown ids try both. Seven resets, five SYNCs after each.
- **SLIP framing, SYNC**, and the 4-byte status the ROM sends after each reply.
- **Chip check.** `GET_SECURITY_INFO` gives the chip id (9 is the S3). A chip without it (ESP32,
  ESP8266) is named from the magic register at `0x40001000`. Anything but an S3 is refused before a
  byte is written. Secure download mode is refused too (no MD5 there, so nothing could be verified).
- **MAC** from eFuse (`0x60007044`), **flash size** from the flash's JEDEC id, read with the RDID command
  (0x9F) through the SPI controller's user-command registers. If that read fails, the size is 0 and
  `SPI_SET_PARAMS` is told 16 MB.
- **Watchdogs.** Over USB-Serial-JTAG the RTC and super watchdogs keep running in download mode; they are
  turned off as esptool does (the ROM's own variable at `0x3FCEF14C` says which port it talks on).
- **Write.** `SPI_ATTACH`, `SPI_SET_PARAMS`, then `FLASH_DEFL_BEGIN/DATA` with zlib data in 1 KiB blocks
  (the ROM block size without the stub). If the ROM refuses `FLASH_DEFL_BEGIN`, it writes uncompressed
  with `FLASH_BEGIN/DATA`. No `FLASH_END`: on the ROM that leaves the bootloader before the check.
- **Speed.** Through a USB serial chip it asks the ROM for 460800 baud (`CHANGE_BAUDRATE`) and checks the
  link with a register read; `--baud 115200` keeps the ROM's rate. Native USB ignores baud and is left
  alone.
- **Verify** with `SPI_FLASH_MD5` (the ROM answers 32 hex digits), then clear the forced-download bit and
  reset the board into the new firmware (EN pulse; the RTC watchdog on USB-OTG, where RTS does nothing).
- Offsets must be multiples of 0x1000: the ROM erases whole sectors.

## What it doesn't do

- **No stub loader.** esptool normally uploads a small flasher program into RAM first; it is faster and
  can erase, read flash and take larger blocks. This uses the ROM only, so writes are slower (the ROM
  erases the whole region up front, then takes 1 KiB per command).
- **Other chips**: ESP32, S2, C3, C6 and the rest are detected and refused.
- **Encrypted flash, secure boot, secure download mode**: not supported. The encrypted-write flag is
  always 0.
- It doesn't patch the flash mode/size bytes of a bootloader header at 0x0 (esptool's `--flash-mode`
  and friends): images are written as they are, which is what our release images expect.
- No retry after the board drops off USB mid-write, no reopening of a port that re-enumerates.
- Firmware using TinyUSB CDC (a USB id other than 303a:1001) may not react to DTR/RTS; then the board
  needs BOOT and RST by hand.

## How it was checked

Only against a simulated board (`fake_test.go`): its reset wiring (two-transistor auto-reset with the
slow EN, and USB-Serial-JTAG), its boot log, and a ROM loader that answers SYNC, registers, security
info, SPI attach and params, baud changes, plain and deflate writes into a 4 MB flash array, and MD5.
Tests cover probe, writes at 0x0 and at an offset (the next sector untouched), plain fallback,
baud change, an MD5 mismatch, a cancelled context, wrong chips, secure download mode, and a board that
never enters download mode (the error says to hold BOOT and press RST). The protocol details (opcodes,
status bytes, ROM block size, reset timings, S3 registers) were checked against the esptool
documentation ("Serial Protocol") and esptool's sources.

**Not yet run on real hardware.** Before relying on it, try on an S3 with native USB and one with a
CH340: `--probe`, a write at 0x0 of a release image, a write at an offset, and a board that's crashing.
