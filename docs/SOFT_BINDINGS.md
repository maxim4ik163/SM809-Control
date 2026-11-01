# Software bindings for the side buttons — starting brief

Branch `soft-bindings`. Read this first in the new conversation.

## Goal

Switch button assignments per program **instantly and without flash wear**. Today a profile switch rewrites the mouse's config sector (every apply = several 4 KB sector erases, see `docs/PROTOCOL.md`), which is why per-game switching is opt-in behind a wear warning.

Idea (proven on the Razer Naga in [NAGA-MMOG-Control](https://github.com/maxim4ik163/NAGA-MMOG-Control)): write the mouse **once** with a fixed "carrier" layout where every remappable button sends a key that no ordinary keyboard has — **F13–F23**. A low-level keyboard hook in SM809 Control catches those keys, swallows them and sends whatever the active profile says. Only the app's memory changes when profiles switch; the mouse is never written again.

## Why F-keys and not WinUSB (Zadig) as on the Naga

On the Naga the side keypad sends ordinary digits, so a hook cannot tell it from the keyboard; the fix there was WinUSB on the keypad's USB interface. On SM809 that would be wrong:

- the keys most likely share an interface with the vendor collections (status report 4, commands 5/6) → the HID command channel would be lost; if they share the mouse interface the cursor would stop;
- the 2.4G dongle is another USB device (separate driver install); **Bluetooth cannot use WinUSB at all**;
- without the app the mouse would be partly dead.

SM809 buttons are programmable, so unique keys solve it without any driver: F13–F23 come only from the mouse, through every connection (cable, dongle, Bluetooth).

Measured on the Naga (`cmd/hookprobe` in NAGA-MMOG-Control): a `WH_KEYBOARD_LL` hook runs ~1 ms **before** Raw Input reports the device, and a key blocked in the hook never reaches Raw Input or any program. So blocking F13–F23 in the hook makes them invisible everywhere.

## Buttons

SM809 Pro has 16 slots (`defaultButtons()` in `store.go`):

| slot | default | role in this mode |
|---|---|---|
| b1–b11 | Win+D, 2, 1, forward, 4, 3, 6, 5, back, fire, My Computer | **remappable**: 9 side buttons + 2 next to the left button → F13–F23 |
| b12–b14 | left, right, middle | locked (left click must never be lost) |
| b15, b16 | DPI+, DPI− | **untouched** — stay firmware DPI buttons in every mode |

Which physical button is which slot — confirm on the mouse at the start (the UI's mouse picture already has hotspots).

Firmware code for a key: `112, mods, usage` (cmd 1). F13–F23 = HID usages **104–114** (`0x68–0x72`); `web/keys.js` already lists F13–F24. Windows: VK_F13–VK_F23 = `0x7C–0x86`, set-1 scan codes `0x64–0x6E` (verify with the probe).

## Design

- **Mode "software bindings"** (opt-in, per user setting). Turning it on writes the carrier layout once (b1–b11 → F13–F23, b12–b16 unchanged) through the normal diff-only write queue; the wear counter shows that one write. Turning it off writes the normal layout of the base profile back.
- **Keyboard hook** on its own thread (same pattern as Naga Control's `hooks.go`): non-injected VK 0x7C–0x86 → swallow, send a press/release event to the engine. Windows' typematic repeat arrives as repeated key-downs: ignore repeats of a held key and emulate repeat in the app only for single-key bindings (Naga Control `input.go` `repeater`).
- **Bindings engine** — port from Naga Control: `model.go` (Binding kinds: default/keys/disable, profiles with exe lists, global bindings with **Force = over profiles**), `core.go` (`resolve`, `activeProfileFor`), `keys.go` (key table shared with the UI, combo parsing), `input.go` (SendInput with an injected marker), foreground watcher (`SetWinEventHook(EVENT_SYSTEM_FOREGROUND)` + 2 s fallback timer, ignore our own window). Profiles switch by the **active window**, not by running processes.
- **What stays on the mouse**: DPI table, polling rate, lighting, LOD, sleep, the DPI buttons. Changing those still writes flash and stays as today (explicit apply). Software profiles change bindings only — so the existing per-game firmware switching can stay off.
- **Default action** of a slot = what the base profile had (e.g. b4 = forward), so the mode feels the same until the user remaps.
- **App not running**: F13–F23 do nothing in almost every program. Optional "restore the normal layout on exit" costs one flash write per exit — default off, explained in the UI.
- **Combo capture** in the UI through the app's keyboard hook, not browser events (Windows acts on Ctrl+Win+arrows before the window sees them) — Naga Control `captureCombo`.

## Limits to keep in mind

- Programs running as administrator do not accept SendInput from a normal app (UIPI); the hook still sees the keys. Option: run elevated.
- A few tools bind F13–F24 themselves (rare); document it.
- Mouse-button actions (back/forward/middle as outputs) need `SendInput` mouse events — supported by the same INPUT structure.
- Macros: firmware macros cannot be produced from a software binding; software macros would be a later step.

## First steps

1. **Probe** (read-only for the app): with SM809 Control 1.4.1 assign F13 to one side button, then check that Windows receives VK 0x7C / scan 0x64, how repeat behaves while held, and that it works over cable, 2.4G and Bluetooth. Naga Control's `cmd/hookprobe` phase 1 shows hook + Raw Input per device; a smaller "key logger" probe may be quicker.
2. Port the engine pieces listed above (with their tests), wire the keyboard hook.
3. UI: a "Software bindings" section — mouse picture with b1–b11, scope selector (global / profile), action editor, Force checkbox; profiles by program.
4. Byte-exact test for the carrier layout packets (existing `protocol_test.go` style).

## Lessons from Naga Control

- Hooks on a dedicated locked OS thread with its own message loop; the callback only decides and queues.
- Lists in JSON must never be `null` (the UI broke on an empty profile list).
- Anything clicked with a worn left button must tolerate doubled clicks.
- `go vet -unsafeptr=false` for hook callbacks (lParam → struct pointer).
- Commits as `maxim4ik163 <130061064+maxim4ik163@users.noreply.github.com>`; pushing branches works, tags are created by the user on GitHub.

## Implementation (1.5.0-beta1)

Files: `softbind.go` (model, resolve, carrier layout — platform independent), `softkeys.go` (key table, HID usage → key name), `soft_windows.go` (hook thread, SendInput, foreground, combo capture), `softbind_test.go`, `linuxtest` `TestSoftBindings`, UI tab "Soft buttons" in `web/app.js`, probe `cmd/keyprobe`.

Decisions made while implementing:

- **Which buttons are carried.** Of b1–b11 only those whose action in the mouse profile can be reproduced by the app: keys and combinations, media/launch keys, right/middle/back/forward, "disabled". Macros, rapid fire, DPI, DPI lock, lighting toggle and any left click stay on the mouse unchanged (the left click can never be lost). The UI shows such buttons as "mouse" and explains how to make them remappable.
- **Default action = the profile on the mouse.** Changing b4 from "forward" to "back" in the Buttons tab is not a flash write in soft mode (both are carried, the carrier packet is identical); changing it to a macro is.
- **Only the keys command.** Turning the mode on/off, "restore on exit" and the start-up check write only command group `keys`; other pending profile changes stay pending.
- **Device state.** `device.json` → `carrier` = buttons that send F13..F23 according to the last keys write or memory read. The hook swallows only those keys (also while the mode is being turned off and the write is still pending), and is installed only while that set is non-empty or a combination is being recorded.
- **Restore on exit** (off by default): writes the normal layout on exit; at the next start, after the mouse is first seen, the carrier is written again (keys only, only if different).
- **Profiles by foreground window** (`SetWinEventHook` + 2 s fallback); our own window and WebView2 keep the current profile. Popup on switch per profile, gated by Settings → popups → "on profile change".
- **Import from the mouse** maps carrier keys back to the actions of the profile on the mouse.

Still to verify on the real mouse (run `keyprobe.exe` first): VK 0x7C–0x86 / scan codes and auto-repeat over cable, 2.4G and Bluetooth; that a blocked carrier key reaches nothing; which physical button is which slot (the soft tab lights up the pressed button).

### Actions (1.5.0-beta2)

Besides keys, mouse buttons and "nothing" a soft button can:

- **Macro** — a macro from the Macros tab played by the app (`playMacro`), same repeat modes: while held / N times / until pressed again. Mode 3 is labelled "repeat until pressed again" (2.1.3) and the app stops it only when the same button is pressed again (a toggle; that press is consumed) — that is also how the user observed the firmware mode. Cursor moves are performed (`MOUSEEVENTF_MOVE`, subject to pointer acceleration). Keys and buttons left down by an interrupted macro are released. A macro used by a soft button cannot be deleted.
- **Text** — `unicode` (KEYEVENTF_UNICODE, layout independent) or `keys` (VkKeyScanEx in a chosen installed layout; the foreground window is switched with WM_INPUTLANGCHANGEREQUEST before typing and switched back 250 ms after the last key, because posted messages overtake keyboard input; Caps Lock is turned off while typing). Characters missing on the layout fall back to Unicode. Line break = Enter, tab = Tab. Optional key after the text (Enter, Tab, Ctrl+Enter…), optional delay between characters. **Secrets** are sealed with DPAPI (current user, this PC) on save: `enc` = `dpapi:` + base64, the plain text is never stored or sent back to the UI; after a backup is moved to another PC/user the secret must be entered again (the status line says so).
- **Start** — ShellExecute "open": a program, shortcut, document, folder or URL, with arguments; `%VAR%` expanded; normal user rights. The file picker runs in the settings window process on its own thread (a modal loop inside a WebView2 event handler is not allowed) and returns through `window.smPickResult`.

Not done yet: running elevated (UIPI), a tray-menu entry. Macros still use the Macros tab editor, so its firmware limits (size, 4095 ms delays) apply to soft macros too.

## 2.0: one Buttons tab, button modes

The separate "Soft buttons" tab of the betas is gone; it made it unclear which assignment of a button was the working one.

- `Settings.ButtonMode`: `""` (not chosen — the UI asks on start with pros and cons), `hardware`, `software`. Changed through `/api/buttons/mode` (keys-only write). A header badge shows it.
- `Settings.HWButtons[11]`: in software mode, buttons 1–11 marked "hardware button" (one setting for all profiles, so the carrier layout never depends on the profile). Changed through `/api/buttons/hw` (keys-only write) from the checkbox in the button editor. Buttons 12–16 are always hardware.
- `Profile.Soft[11]`: software actions per profile (keys, mouse, macro, text, start, nothing). `Profile.Buttons` keeps the hardware actions, used by hardware buttons and in hardware mode — switching modes loses nothing.
- **One list of profiles.** Software buttons use the profile of the first enabled rule for the program in front (rules work in software mode even when per-game switching is off); otherwise the profile on the mouse. Hardware settings follow per-game switching (running process) as in 1.x, opt-in.
- First switch to software: buttons with firmware-only actions (rapid fire, DPI, DPI lock, lighting toggle, left click) default to hardware; every profile's software actions are filled from the hardware ones (`fillSoft`), so the mouse behaves the same. New profiles get them too.
- Guard: the layout written to the mouse must keep a hardware left click (`deviceHasLeft`) — checked on profile save, mode switch and the hardware-button change.
- Migration (`dataVersion` 2): 1.x data → mode not chosen, key repeat on. 1.5 beta data → global bindings into every profile, beta program profiles into new profiles + rules, the set of software buttons as on the mouse, `KeyRepeat`/`RestoreOnExit` into Settings.
- Without the app, software buttons do nothing ("restore on exit" writes the hardware layout).

## 2.1: every button but the left click

- `softSlots` = 16; button 12 (`lockedSlot`, the main left click) is always a hardware button (forced in `/api/buttons/hw` and on load).
- Carrier keys (`carriers` table): buttons 1–11 → F13–F23 (`112,0,104..114`), button 13 → F24 (`112,0,115`, VK 0x87), buttons 14–16 → HID consumer AC Search `0x221`, AC Bookmarks `0x22A`, AC Stop `0x226` (`128,lo,hi`; Windows maps them to VK_BROWSER_SEARCH 0xAA, VK_BROWSER_FAVORITES 0xAB, VK_BROWSER_STOP 0xA9). F24 and the browser keys need confirming with `keyprobe` (it logs them too). A real keyboard's browser Search/Favorites/Stop key is swallowed while that button is a software button.
- Buttons 12–16 start as hardware buttons (`defaultHW`); `dataVersion` 3 marks them hardware in older data.
- UI: the button list is laid out like the mouse — side panel 1–9 (1 long on top, 4 and 9 tall on the right) and the top 10–16 (12 and 13 tall); a green number = software button, red = hardware.

## Probe results (keyprobe, 2026-11-01, 2.4G dongle) — `testdata/keyprobe-2026-11-01.log`

- **F13–F23 confirmed.** VK 0x7C–0x86, scan codes 0x64–0x6E, not injected, from the dongle's `MI_01&COL01` keyboard collection. The hook sees each key 0.4–1.6 ms before Raw Input. Blocked in the hook, the keys never reached Raw Input (phase 2: 0 raw events for F-keys).
- **No auto-repeat** was seen while a button was held (2.8 s hold, no repeated key-downs); the app handles both cases.
- **Browser keys (buttons 14–16) arrive injected.** Windows turns HID consumer usages into keystrokes itself: the hook sees VK 0xA9/0xAA/0xAB with `LLKHF_INJECTED`, scan 00 + E0, and Raw Input reports no device. 2.1.0 ignored all injected events, so these buttons did nothing. 2.1.1 accepts injected browser carrier keys unless they carry our own `injectedMark` (`hookEventKind`). Whether blocking them in the hook also stops Raw Input / app commands is to be confirmed (the probe now blocks them in phase 2 and logs `dwExtraInfo`).
- **F24 (button 13)** did not appear in the log — not tested yet (button 13 was probably not a software button).
- Not yet tested: cable and Bluetooth.

## Probe 2 (2026-11-02, dongle) — `testdata/keyprobe-2026-11-02.log` → 2.1.2

- F13–F23 (incl. F22 on button 10) confirmed again. Held F-keys get Windows typematic repeat: first after ~505 ms, then every 31–47 ms; the app ignores those repeats and repeats single keys itself.
- Browser keys (2.1.0/2.1.1 carriers of buttons 14–16): arrive injected with `dwExtraInfo` 0, **no auto-repeat**, and blocking them in the hook works (0 raw events) — **but without the app they act**: the middle button opened the browser's search bar. That breaks "software buttons do nothing without the app".
- 2.1.2: buttons 14–16 use keyboard-page International4/5/1 (`112,0,0x8A/0x8B/0x87`, VK_CONVERT / VK_NONCONVERT / VK_ABNT_C1, scan 79/7B/73). Real key presses like the F-keys; no effect without a Japanese IME. The hook matches carriers by VK or (non-extended) scan code. `carrierTableVersion` 2 makes the app rewrite the key layout once on start when the mouse still has old codes. The hook ignores injected input again.
- F24 (button 13) still untested. To confirm with keyprobe: buttons 13–16 software, app closed, Notepad/browser in front must show nothing.

## Probe 3 (2026-11-02, dongle, 2.1.2 codes) — `testdata/keyprobe-2026-11-02-b.log`

- Buttons 14–16 confirmed: International4 → scan 79, **VK 0xFF**; International5 → scan 7B, **VK 0xEB**; International1 → scan 73, VK 0xC1. All real key presses from `MI_01&COL01`, with typematic repeat (~505 ms, then 31–47 ms), fully blocked by the hook (0 raw events). Two of three VKs differ from the textbook values on this PC, so matching by scan code is required (it is the primary key; VK 0xFF alone never matches).
- F13–F23 confirmed for the third time.
- F24 (button 13) still absent: no key at all from the mouse for it — either button 13 was not a software button, or the firmware does not send usage 0x73.

## Probe 4 (2026-11-02): F24, cable, Bluetooth — all carriers confirmed

| connection | log | collection | result |
|---|---|---|---|
| 2.4G dongle | `keyprobe-2026-11-02-f24-dongle.log` (+ earlier ones) | `VID_04F3&PID_026F&MI_01&COL01` | F24 (button 13): scan 76, VK 0x87 |
| cable | `keyprobe-2026-11-02-cable.log` | `VID_04F3&PID_026E&MI_01&COL01` | all 15 carriers |
| Bluetooth LE | `keyprobe-2026-11-02-bluetooth.log` | BT HID (`…VID&0204F3…`) | all 15 carriers |

On every connection: real key presses (not injected), same scan codes (F13–F24 = 64–6E, 76; International 1/4/5 = 73/79/7B with VK C1/FF/EB on this PC), Windows typematic repeat (~505 ms, then 31–47 ms), and 0 raw events once blocked in the hook. Overlapping presses of several buttons (seen over Bluetooth) arrive as independent down/up pairs. The carrier table of 2.1.2 is final.

## 2.1.3

- **Bluetooth not seen** (user report: header shows only "2.4G dongle — offline" while the mouse is on Bluetooth). The status collection was matched by an input length of exactly 32 bytes and the reader only waited for pushed reports. Now any vendor-page (`0x000A`) collection with an input report of 10+ bytes is read, and when it stays silent for 8 s it is polled with `HidD_GetInputReport` on a second handle (a synchronous handle serialises I/O). Not yet confirmed on the mouse — Settings → Mouse → *Connection diagnostics* lists every HID collection of the mouse with what its reader got, and copies it as JSON for the developer.
- Macro mode 3 is named "repeat until pressed again" (the observed behaviour); in the app's own playback only the same button stops it. "Run N times" moved next to its N field.
- Every button (except the main left one) has "↺ Default: …" in its editor: the factory assignment for a hardware button, its software equivalent for a software button (`/api/all` → `defaults`).
- 2026-11-02: still not seen over Bluetooth with 2.1.3 (an apparent fix after removing a duplicate pairing was a mix-up: the mouse had switched to the dongle). The vendor app does see the mouse over Bluetooth, so a collection exists that SM809 Control does not recognise — waiting for the Connection diagnostics output taken on Bluetooth.

## 2.1.4: Bluetooth status through feature report 5

Diagnostics on Bluetooth (2026-11-02): the BLE HID device has the same collections as the dongle (`col05` vendor page, in 32 = status; `col06` feature 136 = commands). Its status reader was open but the mouse pushed only one report 4 in minutes, and `HidD_GetInputReport` fails over Bluetooth — so the app never listed the mouse. The vendor tools poll **feature report 5** ("health"). 2.1.4 polls it on the command collection of the same device (paused while the engine writes) when the status collection is silent for 8 s; a reply is used only if it looks like the status layout (`plausibleStatus`: link 0/1, level ≤ 7, known polling code, battery ≤ 100, charge ≤ 2, known sensor). Input report 4 polling is given up after two failures in a row (it may block over BLE). Diagnostics show the raw bytes of the last report 4 and feature 5 to decode the layout if it differs.
