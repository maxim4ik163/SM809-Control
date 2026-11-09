# Changelog

Earlier versions: see [Releases](https://github.com/maxim4ik163/SM809-Control/releases).

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
