# Changelog

Earlier versions: see [Releases](https://github.com/maxim4ik163/SM809-Control/releases).

## 2.3.0 — 2026-11-09

**English**

- **Lighting zones (patched firmware).** The stock firmware has no way to switch the lighting zones separately: mode "Off" turns off everything, and the logo always cycles through 7 colours. A small firmware patch (56 bytes, on top of the side-light fix) reads a byte the stock firmware stores but never uses (command 10) and DPI colour slot 8. With it the *Lighting* tab gets a *Lighting zones* card: side buttons on/off (their modes work as before), wheel on/off (the critical-battery blink stays), logo off or a fixed colour of your choice. Works over cable, dongle and Bluetooth; switching is per profile and written diff-only like everything else.
- The patched firmware is detected automatically when the settings are read from the mouse over the cable; it can also be set by hand in *Settings → Experimental options*. With the stock firmware these settings do nothing.
- **`SM809_IAP_zones_patcher.exe`** (attached to the release) turns the vendor updater `SM809_Mouse_Wired_USB_IAP25-08-11-1.exe` into `SM809_Mouse_IAP_zones.exe`: zones firmware with the side-light fix, English interface, forced update, 30 s reconnect wait. Flashing (with any updater) erases the mouse settings and the dongle pairing: re-plug the dongle, then read the settings from the mouse.
- docs/PROTOCOL.md: how the three lighting zones are driven (LED controller on UART for the side buttons, multiplexed PWM RGB LEDs for the wheel and the logo) and what the patch changes.

**Русский**

- **Зоны подсветки (пропатченная прошивка).** Заводская прошивка не умеет выключать зоны по отдельности: режим «Выкл» гасит всё, а логотип всегда переливается 7 цветами. Небольшой патч прошивки (56 байт, поверх фикса боковой подсветки) читает байт, который заводская прошивка сохраняет, но не использует (команда 10), и 8-й слот цветов DPI. С ним на вкладке «Подсветка» появляется блок «Зоны подсветки»: боковые кнопки вкл/выкл (их режимы работают как раньше), колесо вкл/выкл (мигание при критическом заряде остаётся), логотип выключен или постоянный цвет на выбор. Работает по кабелю, через донгл и по Bluetooth; задаётся в профиле и пишется только при изменении, как всё остальное.
- Пропатченная прошивка определяется автоматически при чтении настроек из мыши по кабелю; её можно отметить и вручную в «Настройки → Экспериментальные опции». С заводской прошивкой эти настройки ни на что не влияют.
- **`SM809_IAP_zones_patcher.exe`** (в релизе) делает из фирменного прошивальщика `SM809_Mouse_Wired_USB_IAP25-08-11-1.exe` файл `SM809_Mouse_IAP_zones.exe`: прошивка с зонами и фиксом боковой подсветки, английский интерфейс, прошивка без проверки версии, ожидание переподключения 30 с. Прошивка (любым прошивальщиком) стирает настройки мыши и привязку к донглу: переподключите донгл и прочитайте настройки из мыши.
- docs/PROTOCOL.md: как управляются три зоны подсветки (контроллер боковых кнопок по UART, мультиплексированные RGB-светодиоды на ШИМ для колеса и логотипа) и что меняет патч.

## 2.2.0 — 2026-11-09

**English**

- **Macro folders.** On the Macros tab macros can be sorted into folders: *+ Folder* creates one; a folder header collapses and expands it and, on hover, offers a new macro inside it, rename, move up/down and delete. Drag a macro onto a folder (or onto *No folder*) to move it, or pick the folder in the macro editor. Deleting a folder keeps its macros — they move to *No folder*; renaming a folder onto an existing one merges them. Collapsed folders are remembered.
- **Search** above the macro list once there are 8 or more macros (matches macro and folder names).
- The macro pickers in the button editor (hardware and software buttons) show the same folders and search; the folder of the assigned macro opens by itself.
- Folders only organise the list in the app: nothing is written to the mouse, and existing macros start without a folder. Export/import of profiles and macros and the full backup keep the folders.

**Русский**

- **Папки для макросов.** На вкладке «Макросы» макросы можно раскладывать по папкам: «+ Папка» создаёт папку; заголовок папки сворачивает и разворачивает её, а при наведении даёт создать в ней макрос, переименовать, переместить выше/ниже и удалить. Макрос переносится перетаскиванием на папку (или на «Без папки») либо выбором папки в редакторе макроса. Удаление папки не удаляет макросы — они попадают в «Без папки»; переименование в имя существующей папки объединяет их. Свёрнутые папки запоминаются.
- **Поиск** над списком, когда макросов 8 и больше (по имени макроса и папки).
- Выбор макроса в редакторе кнопки (аппаратной и программной) показывает те же папки и поиск; папка назначенного макроса раскрывается сама.
- Папки нужны только для порядка в программе: в мышь ничего не записывается, существующие макросы оказываются «Без папки». Экспорт/импорт профилей и макросов и полная резервная копия сохраняют папки.

## 2.1.7 — 2026-11-07

**English**

- **Autostart works when the app runs as administrator.** Windows silently skips `Run` autostart entries of programs that need elevation (e.g. *Run as administrator* set in the exe's compatibility properties): the entry showed as enabled, but the app did not start. Running as administrator, the app now creates a Task Scheduler task `SM809Control` (at logon, highest privileges, no UAC prompt, no 72-hour limit, runs on battery, normal priority) instead of the `Run` entry, and moves an existing `Run` entry over. Settings → Application shows a note about it; a normal-level app shows the task as read-only.
- README: new section *Administrator rights* — what works in elevated programs when SM809 Control runs with and without rights.

**Русский**

- **Автозапуск работает, когда программа запущена от администратора.** Windows молча пропускает записи автозапуска `Run` у программ, которым нужны права (например, в свойствах exe включено «Запускать от имени администратора»): запись была «включена», но программа не стартовала. Запущенная от администратора программа теперь создаёт задачу Планировщика заданий `SM809Control` (при входе, с наивысшими правами, без запроса UAC, без лимита 72 часа, от батареи, обычный приоритет) вместо записи `Run` и переносит в неё существующую запись. В «Настройки → Программа» об этом есть строка; программа без прав показывает задачу только для чтения.
- README: новый раздел «Права администратора» — что работает в программах от администратора, когда SM809 Control запущен с правами и без.

## 2.1.6 — 2026-11-05

**English**

- **No more 0 % dips after connecting.** For the first seconds after the mouse connects (PC start, dongle plugged in, mouse woken up) the firmware reports 0 %, then a value about 10 % too high, and the real charge only ~12 s later. The app now ignores the battery reading for 15 s after the link comes up and shows the last known value meanwhile. The false "battery critical" notification at start-up is gone.
- **Smoothed battery value.** Near a 10 % step the mouse jumps back and forth (80 ↔ 90 every few seconds, for up to an hour). A drop now counts once it has held for 3 minutes, a step back after 30 minutes; a change of the charging state or a jump of 20 % or more counts at once. The tray icon, the chart, the forecast and the low-battery notifications use the smoothed value (the raw one is still in the diagnostics as `batteryRaw`). This also stops the low-battery notification from being re-armed by the jitter.
- **Existing history is cleaned up once** on the first start (and when a backup is restored): the start-up zeros are removed and the same smoothing is applied, so the chart becomes a clean staircase.

**Русский**

- **Больше нет провалов до 0 % после подключения.** В первые секунды после подключения (старт ПК, донгл вставлен, мышь проснулась) прошивка отдаёт 0 %, затем значение примерно на 10 % выше реального, и настоящий заряд — только через ~12 с. Теперь 15 с после появления связи заряд не учитывается, показывается последнее известное значение. Ложное уведомление «очень низкий заряд» при старте больше не появляется.
- **Сглаживание заряда.** На границе шага 10 % мышь скачет туда-обратно (80 ↔ 90 каждые несколько секунд, до часа). Снижение засчитывается, если продержалось 3 минуты, обратный шаг — через 30 минут; смена состояния зарядки и скачок на 20 % и больше — сразу. Иконка в трее, график, прогноз и уведомления о низком заряде используют сглаженное значение (сырое остаётся в диагностике как `batteryRaw`). Заодно дрожание больше не взводит уведомление о низком заряде повторно.
- **Старая история один раз чистится** при первом запуске (и при восстановлении из резервной копии): нули при старте удаляются, применяется то же сглаживание — график становится ровной лесенкой.
