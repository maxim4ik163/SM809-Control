# SM809 Control

An unofficial, open-source configuration app for the **SM809 / SM809 Pro** gaming mouse (Beken BK3633, VID `04F3`) — a replacement for the vendor's Windows software with more features: unlimited profiles, per-game switching, an editable DPI table, a full macro editor, battery history and forecast.

**[Русская версия ниже ↓](#sm809-control-по-русски)**

![Buttons](docs/screenshots/buttons.png)

> ⚠️ Not affiliated with the mouse manufacturer. The protocol was reverse-engineered and every command is checked byte-for-byte against the vendor's own web driver, but you use it at your own risk.

## Features

- **Buttons** — assign any of the 15 remappable buttons: mouse buttons, key or key combination (Ctrl/Shift/Alt/Win), media keys, editing shortcuts, DPI +/−/cycle, DPI lock (sniper), rapid fire with custom interval and count, disable, lighting toggle, macro. The main left button is locked so the mouse can never lose its click.
- **DPI** — 1 to 6 levels, any value per level (limits and step follow the sensor, e.g. PAW3311: 200–24000 in steps of 100), a colour per level from the standard palette or any custom colour, start level.
- **Lighting** — all 11 modes supported by the firmware.
- **Performance** — polling rate (the list follows what the current connection supports), lift-off distance (LOD 1/2 mm — the vendor Windows app lacks it), sleep timer, wake on movement, lighting off while moving, angle snapping, Motion Sync, ripple control.
- **Macros** — recorder (keys and mouse buttons, real or fixed delays) and a manual editor (press / release / delay, type a whole text, reorder, edit delays), three run modes like the vendor app, memory usage meter, warning about keys left pressed. Encoding matches the vendor driver exactly. (Cursor moves inside macros are not performed by the stock firmware, so the editor disables them.)
- **Profiles** — unlimited, stored on the PC; only the current one is written to the mouse, and **only the settings that differ** (to spare the mouse flash memory).
- **Per-game profiles** (opt-in) — a profile is applied when an `.exe` starts and reverted when it exits, with an optional on-screen popup per program. Off by default, enabled only after a warning about flash wear.
- **On-screen popups** for DPI and profile changes with a fully customisable look (position, size, colours, font, timing).
- **Connection indicator** — wired / 2.4 GHz dongle / Bluetooth 1 / Bluetooth 2, battery and charging state.
- **Battery** — history chart (day/week/month), drain per day, estimated time left or time to full, low-battery notifications with the estimate.
- **Tray icon** with battery level, quick profile switching, autostart.
- **Backup** — export/import profiles and macros, or a full backup of all app data (profiles, settings, flash write counter, battery history) to move to another PC.
- **English and Russian** UI; the system language is picked on first run.

| | |
|---|---|
| ![DPI](docs/screenshots/dpi.png) | ![Lighting](docs/screenshots/lighting.png) |
| ![Performance](docs/screenshots/performance.png) | ![Macros](docs/screenshots/macros.png) |
| ![Profiles](docs/screenshots/profiles.png) | ![Battery](docs/screenshots/battery.png) |

![Settings and popups](docs/screenshots/settings.png)

## Download and run

Get `SM809Control.exe` from [Releases](../../releases) — a single file, no installation.

- Windows 10/11 (uses the WebView2 runtime that ships with Windows; without it the UI opens in the default browser).
- It lives in the tray. Left click opens the window, right click opens the menu.
- The window is a separate short-lived process: when you close it, only a small background part (≈10–15 MB) keeps running for the tray, popups and per-game switching.
- Do not run the vendor software or web driver at the same time.

**First run.** The mouse cannot report its settings, so the PC is the source of truth. The default profile is initialised from what the mouse does report (lighting, polling rate, current DPI level); check the tabs and press **“Write … to the mouse”** in the yellow bar once. Until that first full write, per-game switching is disabled.

**Flash wear.** The mouse rewrites a 4 KB flash sector on every accepted command. The app sends only differences, never rewrites the mouse while a game runs, keeps per-game switching off by default and shows a write counter in Settings.

Data is stored in `%APPDATA%\SM809Control` (`data.json` — profiles, macros, rules, settings; `device.json` — what is on the mouse; `battery.json` — battery history).

## Supported devices

SM809 / SM809 Pro family with Elan VID `0x04F3`: PID `0x026E` (wired), `0x026F` (2.4 GHz dongle), `0x026D` (Bluetooth LE). Sensor type is read from the mouse (PAW3311, PAW3395, PAW3950 and others from the vendor table).

## Building

Go 1.24, from any OS:

```sh
GOOS=windows GOARCH=amd64 go build -ldflags "-H windowsgui -s -w" -o SM809Control.exe .
```

- Dependencies (go-webview2, go-winloader, trimmed golang.org/x/sys) are vendored in `third_party/` via `replace` in `go.mod` — no network needed.
- Icon, manifest and version info: `res/`, compiled into `rsrc_windows_amd64.syso` with `llvm-windres --target=pe-x86-64 -i res/app.rc -o rsrc_windows_amd64.syso`.
- The UI is plain HTML/JS/CSS in `web/`, embedded into the exe.

Tests:

```sh
go test .               # protocol encoders vs. packets captured from the vendor web driver
sh linuxtest/run.sh     # engine/API logic with HID stubbed out (Linux/macOS)
```

## Project layout

| Path | What |
|---|---|
| `protocol.go` | HID command encoders (keys, DPI, colours, polling, LOD, power, sensor, lighting, macros, reset) |
| `hid.go` | HID enumeration, status reader (input report 4), command channel (feature report 6 with ack/retry) |
| `engine.go` | Device state, diff-only apply queue, per-game profile stack, notifications |
| `store.go` | Data model and persistence |
| `battery.go` | Battery history and forecast |
| `api.go` | Local HTTP API (127.0.0.1, random port, token) + SSE for the UI |
| `main.go`, `osd.go`, `icon.go` | Tray, on-screen popups, battery tray icon (Win32) |
| `uihost.go` | WebView2 window process |
| `web/` | UI |
| `docs/PROTOCOL.md` | Reverse-engineered HID protocol |

## Protocol

See [docs/PROTOCOL.md](docs/PROTOCOL.md) — status report, all commands, button codes, macro format and firmware notes.

---

# SM809 Control
Неофициальная программа с открытым кодом для настройки мыши **SM809 / SM809 Pro**. Заменяет фирменную программу и умеет больше: неограниченное число профилей, переключение по играм, любые значения DPI, полноценный редактор макросов, история и прогноз заряда.

> ⚠️ Программа не связана с производителем мыши. Протокол восстановлен самостоятельно, каждая команда сверена байт-в-байт с официальным веб-драйвером, но используете вы её на свой риск.

**Возможности**

- **Кнопки:** любая функция на 15 переназначаемых кнопок — кнопки мыши, клавиша или сочетание, мультимедиа, правка, DPI, фиксация DPI, «огонь», отключение, подсветка, макрос. Основная левая кнопка заблокирована.
- **DPI:** 1–6 уровней с любыми значениями, цвет каждого уровня из стандартной палитры или любой свой, стартовый уровень.
- **Подсветка:** все 11 режимов прошивки.
- **Производительность:** частота опроса, высота отрыва LOD (её нет в фирменной программе), сон, пробуждение движением, angle snapping, Motion Sync, Ripple.
- **Макросы:** запись и ручной редактор, ввод целого текста, три режима повтора, индикатор памяти. Кодирование совпадает с официальным драйвером. Сдвиг курсора в макросах заводская прошивка не выполняет, поэтому он отключён.
- **Профили:** без ограничения количества, хранятся на компьютере. В мышь пишется только то, что отличается.
- **Переключение по играм:** профиль включается при запуске exe и возвращается при выходе. По умолчанию выключено и включается только после предупреждения об износе флеш-памяти.
- **Всплывающие окна** DPI и профиля с настройкой внешнего вида.
- **Индикатор подключения:** провод, донгл, BT1, BT2; заряд.
- **Батарея:** график, расход в сутки, прогноз остатка и времени до полной зарядки.
- **Трей, автозапуск, резервная копия всех данных** для переноса на другой ПК.
- **Русский и английский** интерфейс.

**Запуск:** скачайте `SM809Control.exe` в [Releases](../../releases). Установка не нужна, программа работает в трее. Не запускайте одновременно с фирменной программой или веб-драйвером.

**Первый запуск:** мышь не умеет отдавать свои настройки, поэтому их нужно один раз записать целиком — кнопка в жёлтой полосе вверху окна.

**Сборка:** см. раздел [Building](#building) выше.
