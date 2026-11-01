# SM809 mouse — HID protocol (reverse-engineered)

Sources: mouse firmware (Beken BK3633, app image in `SM809_Mouse_Wired_USB_IAP` updater), vendor Windows app `GamingMS.exe` v2.1.15, vendor web driver (`driver.yuandaxin-tech.com/HYX/V1000_SM809_3311`, files `index-CQl4Vnyy.js`, `DeviceView-C4JAQhLH.js`).

## Device identification
- VID `0x04F3` (1267). PID `0x026E` wired USB, `0x026F` 2.4G dongle, `0x026D` Bluetooth LE (PnP ID in firmware).
- Vendor top-level collections, usage page `0x000A`:
  - input report `0x04`, 31 data bytes (32 incl. ID) — status, pushed by the mouse;
  - feature reports `0x05`, `0x06` (31 bytes), `0xBC` (7), `0xBD/0xBE/0xBF` (135; `0xBF` = firmware IAP).
- Web driver picks the HID device that has more than 2 collections.

## Status — input report 0x04 (data bytes after report ID)
| idx | meaning |
|---|---|
| 0 | link (dongle: 0 = mouse offline) |
| 1 | current DPI level (0-based) |
| 2 | current polling-rate code |
| 3 | battery % |
| 4 | 0 on battery, 1 charging, 2 charged |
| 5 | lighting mode |
| 7 | max polling-rate code supported |
| 8 | sensor type (17=3311 default, 149=3395, 80=3950, 32=3220, 18=3212, 9=3809, 6=S205, 112=OM70) |
Firmware sends it on change and periodically (USB ≈ every 5000 ticks). `receiveFeatureReport(5)` is used as a status/health poll.

## Commands — feature report 0x06
Build payload array `D` (no report ID), pad to 30 bytes, then insert checksum at index 3:
`sum = (6 + Σ D[i]) & 0xFF; D.splice(3, 0, sum)` → 31 bytes. Send `sendFeatureReport(6, D)`.
Wait ~60 ms, `receiveFeatureReport(6)`; byte at index 1 (after report ID) `== 1` means OK. Retry up to 3×.
`seq` = rolling counter 1..255.

| cmd | payload before checksum | notes |
|---|---|---|
| 1 | `[1, seq, 1, btn×9 (3 bytes each)]` | keys, ≤9 buttons; order swaps index 3↔4; unset = FF FF FF; pad FF |
| 1 | `[1, seq, 2, …]` + `[1, seq, 18, …]` | keys, >9 buttons (SM809 has 16): packet order of button indices `[11,12,13,14,15,9,10,0,1]` then `[2,3,4,5,8,6,7]` |
| 2 | `[2, seq, 2(X)/18(Y), curLevel, enableMask, 8×(lo,hi)]` | DPI value code = DPI/50−1 for sensors 149, 80; else DPI/100−1. Unused levels default 1600. Pad FF |
| 3 | `[3, seq, 1, 8×(R,G,B)]` | DPI level colours. Pad FF |
| 4 | `[4, seq, 1, rate]` | polling: 8=125, 4=250, 2=500, 1=1000, 17=2000, 18=4000, 20=8000 Hz. Pad 00 |
| 5 | `[5, seq, 1, lod]` | lift-off distance: 1 = 1 mm, 2 = 2 mm. Pad FF |
| 6 | `[6, seq, 1, 255, mode, bright<<4\|speed, dir, R,G,B?]` | lighting; SM809 scheme 3 modes 0 off,1 Stars twinkle,2 Colorful streaming,3 Seven-colour stretch,4 Seven-colour flashing,5 Colorful breath,6 Explosion flash,7 Colorful coverage,8 Red full,9 Green full,10 Blue full (no brightness/speed/colour). Pad FF |
| 7 | `[7, seq, 1, sleepMin, moveWakeup, moveLightOff]` | power; sleep 1..20 min. Pad FF |
| 8 | `[8, seq, 1, angleSnap, motionSync, rippleControl]` | sensor. Pad FF |
| 15 | `[15, seq, 1, 255]` | factory reset |
| 17 | `[17, seq, 1, 255]` | erase macro area (before uploading macros). Pad 00 |
| 16 | `[16, seq, (pkt<<4)\|npkts, 1+len, slot, data(≤25)]` | macro upload, see below. Pad 00 |

### Button function codes (3 bytes)
- Mouse: left `16,1,0`; right `16,2,0`; middle `16,4,0`; forward `16,16,0`; back `16,8,0`.
- Fire key `48,interval,count` (triple fire `48,50,3`, fire `48,50,0`).
- DPI: cycle `64,1,0`, + `64,2,0`, − `64,3,0`. DPI lock (sniper) `80,n,0` → (n+1)×100 DPI, n=1..11 (200..1200).
- Disable `96,0,0`; lighting toggle `96,4,0`.
- Key/combo `112,mods,usage` (mods bit0 Ctrl, 1 Shift, 2 Alt, 3 Win; usage = HID keyboard usage).
- Consumer `128,lo,hi` (usage 16-bit LE): play/pause `128,205,0`, mute `128,226,0`, vol+ `128,233,0`, vol− `128,234,0`, next `128,181,0`, prev `128,182,0`, stop `128,183,0`, player `128,131,1`, mail `128,138,1`, calc `128,146,1`, my computer `128,148,1`.
- Macro `144, mode<<4 | (buttonId & 15), repeat` — mode 1 = repeat while the button is held, 2 = run `repeat` times, 4 = repeat until any key is pressed (web driver executeType 1/2/3); buttonId 16 → 12.
- SM809 default layout (web): b1 Win+D, b2 "2", b3 "1", b4 forward, b5 "4", b6 "3", b7 "6", b8 "5", b9 back, b10 fire `48,16,2`, b11 My Computer, b12 left (disabled), b13 right, b14 middle, b15 DPI+, b16 DPI−.

### Macro data
Header buffer `[9, 144, buttonIndex, actionCount]` then 3 bytes per event:
- key down/up: `b1 = (delayHi & 0x0F) | type | (keyup ? 0x80 : 0)`, `b2 = delayLo`, `b3 = code`; delay = delay of the *next* event (ms, max 4095); type `0x30` HID key (<145, code = usage), `0x20` modifier (usage 224..231, code = bit `1<<(usage&7)`), `0x10` mouse button (240..244 → bit), `0x40`/`0x50`/`0x60` for 245/246/247.
- delay event: `[delayHi & 0x0F, delayLo, 0]`.
- cursor move: `[0x40 left / 0x41 right / 0x50 up / 0x51 down, delayLo, value (two's complement for left/up)]`.
Payload = buffer from index 4, split into packets of 25 bytes; packet slot byte = buttonIndex (16 → 12).
Upload sequence: keys command (1) → erase macros (17) → each macro's packets (16), ~120 ms apart.

## Firmware behaviour notes
- Each accepted command marks a config block dirty; the main loop erases and rewrites a 4 KB flash sector (config 0x7C000, keys/macros 0x7B000/0x7D000). Every profile apply = several sector erases → limit how often profiles are pushed.
- Settings are not read back by the vendor tools; the host keeps the source of truth.
- Side lighting is a separate controller driven over UART 19200 by command `{0x70, mode}`; stock firmware sends it 5× starting 10 ticks after power-up (bug: random "colorful" fallback). Fixed by 1 send with a 64-tick delay (`movs r3,#0x40` at 0x2DC90, `movs r7,#1` at 0x2DC9A).
- Firmware image format: 16-byte header (CS1 = CRC32 reflected, init 0xFFFFFFFF, **no final XOR**, over file[16:]), then 32-byte blocks each followed by big-endian CRC16 (poly 0x8005, init 0xFFFF).

## Undocumented commands

The firmware's report-6 dispatcher (`switch` on the command byte, 0..17) has two cases the vendor tools never send:

| cmd | effect |
|---|---|
| 9 | `config[0x78] = D[4]` — key debounce threshold in scan ticks (factory value 2) |
| 10 | `config[0x7B] = D[4]` — purpose unknown |

Command 9 is offered by SM809 Control as an experimental setting (Settings → Mouse); command 10 is not used.

## Reading the mouse memory (report 0xBF)

Feature report `0xBF` (136 bytes incl. ID, wired USB interface 1) is the firmware's IAP channel:

| byte | meaning |
|---|---|
| 0 | `0xBF` |
| 1 | write length — if ≠ 0 the firmware **writes** that many bytes from [8..] to flash |
| 2 | read length — if [1] = 0 and [2] ≠ 0 the firmware reads [2] bytes (≤ 128) into [8..] |
| 3 | command, only executed when [1] = [2] = 0 (1/3 erase, 2 enter bootloader, …) |
| 5..7 | flash address, big endian |

`GET_FEATURE(0xBF)` then returns the buffer (header echoed, data from byte 8). SM809 Control only ever sends [1] = 0, [2] = 1..128, and only over the cable (through the dongle the dongle itself would answer).

### Settings sector 0x7C000 (RAM copy of the config struct, 0x81 bytes)

| offset | content |
|---|---|
| 0x00–0x04 | header |
| 0x05–0x34 | 16 buttons × 3 bytes, in key-packet order (indices 11,12,13,14,15,9,10,0,1,2,3,4,5,8,6,7) |
| 0x38 | current DPI level |
| 0x39 | enabled levels bit mask |
| 0x3A–0x49 | DPI X, 8 × (lo, hi) |
| 0x4A–0x59 | DPI Y |
| 0x5A–0x71 | DPI colours, 8 × RGB |
| 0x72 | polling-rate code |
| 0x73 | LOD |
| 0x74–0x76 | angle snap, motion sync, ripple |
| 0x77 | sleep minutes |
| 0x78 | key debounce (cmd 9) |
| 0x7A | sensor type |
| 0x7B | cmd 10 value |
| 0x7C–0x7E | lighting mode, brightness/speed, direction |
| 0x7F | wake on movement |
| 0x80 | lighting off while moving |

### Macros

Slot index `i = button − 1` (button 16 uses slot 12 → i = 11). `i < 8` → `0x7D000 + i·0x200`, otherwise `0x7B000 + (i−8)·0x200`. Content is the event stream described above, zero-padded.

### Cursor moves in macros

The firmware's macro player handles event types `0x4_` (X) and `0x5_` (Y) by writing the signed value into the motion accumulators (`0x400AA8/0x400AAA`), but it does not raise the "motion pending" flag that the sensor path sets, and the accumulators are cleared/overwritten before a report is built. In practice the move is lost (confirmed on a real mouse; the vendor web driver has the same result). SM809 Control therefore disables cursor-move events in the macro editor.
