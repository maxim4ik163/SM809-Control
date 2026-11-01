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
