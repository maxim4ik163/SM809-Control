# Changelog

Earlier versions: see [Releases](https://github.com/maxim4ik163/SM809-Control/releases).

## Unreleased

**English**

- **Battery forecast counts only the time the mouse is connected.** It used calendar time, so nights and weekends with the PC off (the mouse sleeps and hardly drains) made the forecast jump several times up after every break. *Time left* is now hours of use, with calendar days at your usual routine next to it (average time connected per day over the last two weeks, after 3 days of history). *Drain* is shown in % per hour of use.

**Русский**

- **Прогноз заряда учитывает только время, пока мышь на связи.** Раньше считалось календарное время, и ночи и выходные с выключенным ПК (мышь спит и почти не тратит заряд) после каждого перерыва завышали прогноз в разы. *Хватит примерно на* теперь показывает часы работы, а рядом — дни при вашем обычном режиме (среднее время на связи в сутки за последние две недели, после 3 дней истории). *Расход* — в % за час работы.

## 2.3.0 — 2026-11-09

**English**

This release is for mice flashed with the **zones firmware** — a modified version of the stock firmware, made with the included patcher. With the stock firmware everything works as in 2.2.0.

**What the zones firmware changes**

1. **Side-light fix.** The side buttons have their own small LED controller; the mouse only tells it which effect to show. The stock firmware sends that command too early after switching the controller on (after power-on, wake-up, reconnect), while the controller is still starting. The command is lost and the side buttons fall back to the controller's default rainbow effect (*Colorful streaming*) instead of the chosen mode — until the next time the mouse resends the mode, e.g. when it is moved after idling. The fix waits longer before sending and sends the command once. On some mice this happens constantly, on others only now and then.
2. **Lighting zones.** The stock firmware cannot switch the lighting zones separately: mode *Off* turns everything off, the wheel always shows the DPI colour and the logo always cycles through 7 colours. The zones firmware adds:
   - side buttons on/off — when on, the mode chosen on the *Lighting* tab works as before;
   - wheel on/off — the red blink at critical battery stays;
   - logo off, or a fixed colour of your choice.

   The settings use a byte the stock firmware stores but never reads and the 8th DPI colour slot (the app uses at most 6 DPI levels), so no new commands are needed. The patch adds 56 bytes of code; nothing else in the firmware is changed.

**In the app**

- A *Lighting zones* card on the *Lighting* tab. Zones are part of the profile and, like everything else, written to the mouse only when they change.
- The zones firmware is detected when the settings are read from the mouse over the cable (*Settings → Mouse → Read settings from the mouse*); it can also be switched on by hand in *Settings → Experimental options*. With the stock firmware these settings do nothing.

**`SM809_IAP_zones_patcher.exe`**

The firmware itself is not distributed here. The patcher takes the vendor updater `SM809_Mouse_Wired_USB_IAP25-08-11-1.exe` (it checks the exact file) and creates `SM809_Mouse_IAP_zones.exe` next to it — the same updater with the zones firmware, English interface, no "already up to date" refusal and a 30 s wait for the mouse to reconnect after flashing. Drag the vendor updater onto the patcher.

Where to get the vendor updater: [solakaka.com/pages/driver-download](https://solakaka.com/pages/driver-download) → mouse **SM809 Pro** → **[Download Firmware Upgrade]**. It downloads `SM809PRO_Firmware_Upgrade.rar`; the updater `SM809_Mouse_Wired_USB_IAP25-08-11-1.exe` is inside.

**Flashing — read before you start**

- Close SM809 Control, the vendor software and the web driver; connect the mouse with the cable.
- **Flashing erases the mouse settings and the dongle pairing** — with any updater, the stock one too. Afterwards the 2.4 GHz indicator blinks: unplug the dongle and plug it back in, it pairs with the mouse on its own. Then in SM809 Control read the settings from the mouse over the cable and write your profile.
- To go back, flash with the original vendor updater.

Firmware details: [docs/PROTOCOL.md](docs/PROTOCOL.md#lighting-hardware-and-the-zones-firmware-patch).

**Русский**

Этот выпуск для мышей с **прошивкой «зоны»** — изменённой заводской прошивкой, которую делает патчер из релиза. С заводской прошивкой всё работает как в 2.2.0.

**Что меняет прошивка «зоны»**

1. **Исправление боковой подсветки.** У боковых кнопок свой маленький контроллер светодиодов; мышь только сообщает ему, какой эффект показывать. Заводская прошивка отправляет эту команду слишком рано после включения контроллера (при включении, пробуждении, переподключении), пока он ещё запускается. Команда теряется, и боковые кнопки показывают радужный эффект контроллера по умолчанию («Цветной поток») вместо выбранного режима — пока мышь не отправит режим ещё раз, например когда её сдвинут после простоя. Исправление ждёт дольше и отправляет команду один раз. На одних мышах это происходит постоянно, на других изредка.
2. **Зоны подсветки.** Заводская прошивка не умеет выключать зоны по отдельности: режим «Выкл» гасит всё, колесо всегда показывает цвет DPI, логотип всегда переливается 7 цветами. Прошивка «зоны» добавляет:
   - боковые кнопки вкл/выкл — когда включены, режим с вкладки «Подсветка» работает как раньше;
   - колесо вкл/выкл — красное мигание при критическом заряде остаётся;
   - логотип выключен или постоянный цвет на выбор.

   Настройки хранятся в байте, который заводская прошивка сохраняет, но не читает, и в 8-м слоте цветов DPI (программа использует не больше 6 уровней), новых команд не нужно. Патч добавляет 56 байт кода, остальная прошивка не меняется.

**В программе**

- Блок «Зоны подсветки» на вкладке «Подсветка». Зоны — часть профиля и, как всё остальное, записываются в мышь только при изменении.
- Прошивка «зоны» определяется при чтении настроек из мыши по кабелю («Настройки → Мышь → Прочитать настройки из мыши»); её можно включить и вручную в «Настройки → Экспериментальные опции». С заводской прошивкой эти настройки ни на что не влияют.

**`SM809_IAP_zones_patcher.exe`**

Саму прошивку мы не распространяем. Патчер берёт фирменный прошивальщик `SM809_Mouse_Wired_USB_IAP25-08-11-1.exe` (проверяет, что это именно он) и создаёт рядом `SM809_Mouse_IAP_zones.exe` — тот же прошивальщик с прошивкой «зоны», английским интерфейсом, без отказа «прошивка уже актуальна» и с ожиданием переподключения мыши 30 с. Перетащите фирменный прошивальщик на патчер.

Где взять фирменный прошивальщик: [solakaka.com/pages/driver-download](https://solakaka.com/pages/driver-download) → мышь **SM809 Pro** → кнопка **[Download Firmware Upgrade]**. Скачается архив `SM809PRO_Firmware_Upgrade.rar`, прошивальщик `SM809_Mouse_Wired_USB_IAP25-08-11-1.exe` лежит в нём.

**Прошивка — прочитайте до начала**

- Закройте SM809 Control, фирменную программу и веб-драйвер; подключите мышь кабелем.
- **Прошивка стирает настройки мыши и привязку к донглу** — любым прошивальщиком, заводским тоже. После неё мигает индикатор 2,4 ГГц: выньте донгл и вставьте обратно — он сам привяжет мышь. Затем в SM809 Control прочитайте настройки из мыши по кабелю и запишите свой профиль.
- Чтобы вернуться, прошейте оригинальным фирменным прошивальщиком.

Подробности о прошивке: [docs/PROTOCOL.md](docs/PROTOCOL.md#lighting-hardware-and-the-zones-firmware-patch).

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
