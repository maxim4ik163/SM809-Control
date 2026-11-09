# SM809 IAP patcher (zones firmware)

`SM809_IAP_zones_patcher.exe` turns the vendor updater **`SM809_Mouse_Wired_USB_IAP25-08-11-1.exe`**
(SHA-256 `ab1f8f3c…f84088`) into **`SM809_Mouse_IAP_zones.exe`** — the same updater with:

- **zones firmware**: the side-light fix (one lighting command, sent with a delay) plus switchable lighting zones — side buttons on/off, wheel on/off (the critical-battery blink stays), logo off or a fixed colour. Set them in SM809 Control 2.2+ on the *Lighting* tab;
- **English** interface;
- **forced update** (also when the mouse already reports this firmware);
- **30 s** wait for the mouse to reconnect after the update.

The repository holds only our changes (`patch_data.go`, ~6 KB); the vendor updater is the input and is checked by its SHA-256, the result too.

**Use:** drag the vendor updater onto the patcher (or put both in one folder and run the patcher). `SM809_Mouse_IAP_zones.exe` appears next to it. Close SM809 Control, the vendor software and the web driver, connect the mouse with the cable and flash.

**After flashing** — any updater, the stock one too, erases the mouse settings and the dongle pairing:
1. unplug the dongle and plug it back in — it pairs with the mouse on its own;
2. SM809 Control → Settings → Mouse → *Read settings from the mouse* (cable), then write your profile.

To go back, flash with the original updater. Firmware details: [docs/PROTOCOL.md](../../docs/PROTOCOL.md#lighting-hardware-and-the-zones-firmware-patch).

---

**По-русски.** `SM809_IAP_zones_patcher.exe` делает из фирменного прошивальщика `SM809_Mouse_Wired_USB_IAP25-08-11-1.exe` файл `SM809_Mouse_IAP_zones.exe`: прошивка с зонами подсветки и фиксом боковой подсветки, английский интерфейс, прошивка без проверки версии, ожидание переподключения 30 с. Перетащите фирменный прошивальщик на патчер. Перед прошивкой закройте SM809 Control, фирменную программу и веб-драйвер. После прошивки настройки и привязка к донглу стираются: выньте и вставьте донгл, затем прочитайте настройки из мыши в SM809 Control и запишите профиль. Вернуть заводскую прошивку — прошить оригинальным прошивальщиком.

**Developers.** `fw/zones.s` is the patch code, `fw/build_fw.py ORIGINAL.exe UIBASE.exe OUT.exe` extracts the stock firmware from the vendor updater, applies the d64 fix and the zones hooks, rebuilds the container (CRC-16 per 32-byte block, CS1) and puts it into an updater that already has our UI patches (e.g. the patcher's output). Then `python3 mkpatch.py ORIGINAL.exe OUT.exe > patch_data.go`. Rebuilding with the current sources reproduces `SM809_Mouse_IAP_zones.exe` byte for byte.
