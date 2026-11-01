'use strict';
// Language helper and static tables shared by the UI.

let LANG = 'en';
const L = (ru, en) => (LANG === 'ru' ? ru : en);

// HID keyboard usages by KeyboardEvent.code (physical key, layout independent).
const HID = (() => {
  const m = {};
  for (let i = 0; i < 26; i++) m['Key' + String.fromCharCode(65 + i)] = 4 + i;
  for (let i = 1; i <= 9; i++) m['Digit' + i] = 29 + i;
  m.Digit0 = 39;
  Object.assign(m, {
    Enter: 40, Escape: 41, Backspace: 42, Tab: 43, Space: 44, Minus: 45, Equal: 46,
    BracketLeft: 47, BracketRight: 48, Backslash: 49, Semicolon: 51, Quote: 52, Backquote: 53,
    Comma: 54, Period: 55, Slash: 56, CapsLock: 57,
    PrintScreen: 70, ScrollLock: 71, Pause: 72, Insert: 73, Home: 74, PageUp: 75, Delete: 76,
    End: 77, PageDown: 78, ArrowRight: 79, ArrowLeft: 80, ArrowDown: 81, ArrowUp: 82,
    NumLock: 83, NumpadDivide: 84, NumpadMultiply: 85, NumpadSubtract: 86, NumpadAdd: 87,
    NumpadEnter: 88, Numpad0: 98, NumpadDecimal: 99, IntlBackslash: 100, ContextMenu: 101,
    ControlLeft: 224, ShiftLeft: 225, AltLeft: 226, MetaLeft: 227,
    ControlRight: 228, ShiftRight: 229, AltRight: 230, MetaRight: 231,
  });
  for (let i = 1; i <= 9; i++) m['Numpad' + i] = 88 + i;
  for (let i = 1; i <= 12; i++) m['F' + i] = 57 + i;
  for (let i = 13; i <= 24; i++) m['F' + i] = 91 + i; // F13 = 104
  return m;
})();

// Language-neutral key cap names; the rest is translated in keyName().
const HID_CAP = (() => {
  const n = {};
  for (let i = 0; i < 26; i++) n[4 + i] = String.fromCharCode(65 + i);
  for (let i = 1; i <= 9; i++) n[29 + i] = String(i);
  n[39] = '0';
  Object.assign(n, {
    40: 'Enter', 41: 'Esc', 42: 'Backspace', 43: 'Tab', 45: '-', 46: '=', 47: '[', 48: ']',
    49: '\\', 51: ';', 52: "'", 53: '`', 54: ',', 55: '.', 56: '/', 57: 'Caps Lock',
    70: 'Print Screen', 71: 'Scroll Lock', 72: 'Pause', 73: 'Insert', 74: 'Home', 75: 'Page Up',
    76: 'Delete', 77: 'End', 78: 'Page Down', 79: '→', 80: '←', 81: '↓', 82: '↑',
    83: 'Num Lock', 84: 'Num /', 85: 'Num *', 86: 'Num -', 87: 'Num +', 88: 'Num Enter',
    98: 'Num 0', 99: 'Num .', 100: '\\ (ISO)',
  });
  for (let i = 1; i <= 9; i++) n[88 + i] = 'Num ' + i;
  for (let i = 1; i <= 12; i++) n[57 + i] = 'F' + i;
  for (let i = 13; i <= 24; i++) n[91 + i] = 'F' + i;
  return n;
})();

function keyName(c) {
  if (HID_CAP[c]) return HID_CAP[c];
  const mod = ['Ctrl', 'Shift', 'Alt', 'Win'];
  if (c >= 224 && c <= 227) return mod[c - 224] + L(' (лев.)', ' (left)');
  if (c >= 228 && c <= 231) return mod[c - 228] + L(' (прав.)', ' (right)');
  const t = {
    44: ['Пробел', 'Space'], 101: ['Меню', 'Menu'],
    127: ['Без звука (клав.)', 'Mute (keyboard)'], 128: ['Громкость + (клав.)', 'Volume + (keyboard)'], 129: ['Громкость − (клав.)', 'Volume − (keyboard)'],
    240: ['ЛКМ', 'Left click'], 241: ['ПКМ', 'Right click'], 242: ['Колесо (СКМ)', 'Middle click'],
    243: ['Кнопка 4 (назад)', 'Button 4 (back)'], 244: ['Кнопка 5 (вперёд)', 'Button 5 (forward)'],
  }[c];
  return t ? L(t[0], t[1]) : L('код ', 'code ') + c;
}

// Groups for key pickers (macro editor, combos).
const KEY_GROUPS = () => [
  [L('Буквы', 'Letters'), Array.from({ length: 26 }, (_, i) => 4 + i)],
  [L('Цифры', 'Digits'), [30, 31, 32, 33, 34, 35, 36, 37, 38, 39]],
  [L('Модификаторы', 'Modifiers'), [224, 225, 226, 227, 228, 229, 230, 231]],
  [L('Управление', 'Navigation & editing'), [40, 41, 42, 43, 44, 57, 73, 74, 75, 76, 77, 78, 79, 80, 81, 82, 70, 71, 72, 101]],
  [L('Символы', 'Symbols'), [45, 46, 47, 48, 49, 51, 52, 53, 54, 55, 56, 100]],
  [L('F-клавиши', 'Function keys'), [58, 59, 60, 61, 62, 63, 64, 65, 66, 67, 68, 69, 104, 105, 106, 107, 108, 109, 110, 111, 112, 113, 114, 115]],
  [L('Цифровой блок', 'Numpad'), [83, 84, 85, 86, 87, 88, 89, 90, 91, 92, 93, 94, 95, 96, 97, 98, 99]],
];
const MOUSE_KEYS = [240, 241, 242, 243, 244];

// Button function catalog (codes from the vendor web driver). Names: [ru, en].
const CATALOG = [
  { id: 'mouse', name: ['Мышь', 'Mouse'], items: [
    [['Левая кнопка', 'Left click'], 16, 1, 0], [['Правая кнопка', 'Right click'], 16, 2, 0], [['Средняя кнопка', 'Middle click'], 16, 4, 0],
    [['Вперёд', 'Forward'], 16, 16, 0], [['Назад', 'Back'], 16, 8, 0],
  ] },
  { id: 'key', name: ['Клавиатура', 'Keyboard'], items: [] }, // combo editor
  { id: 'edit', kind: 'key', name: ['Правка', 'Editing'], items: [
    [['Копировать (Ctrl+C)', 'Copy (Ctrl+C)'], 112, 1, 6], [['Вставить (Ctrl+V)', 'Paste (Ctrl+V)'], 112, 1, 25], [['Вырезать (Ctrl+X)', 'Cut (Ctrl+X)'], 112, 1, 27],
    [['Выделить всё (Ctrl+A)', 'Select all (Ctrl+A)'], 112, 1, 4], [['Создать (Ctrl+N)', 'New (Ctrl+N)'], 112, 1, 17], [['Сохранить (Ctrl+S)', 'Save (Ctrl+S)'], 112, 1, 22],
    [['Печать (Ctrl+P)', 'Print (Ctrl+P)'], 112, 1, 19], [['Проводник (Win+E)', 'Explorer (Win+E)'], 112, 8, 8], [['Отменить (Ctrl+Z)', 'Undo (Ctrl+Z)'], 112, 1, 29],
    [['Рабочий стол (Win+D)', 'Show desktop (Win+D)'], 112, 8, 7],
  ] },
  { id: 'media', name: ['Мультимедиа', 'Media'], items: [
    [['Проигрыватель', 'Media player'], 128, 131, 1], [['Предыдущий трек', 'Previous track'], 128, 182, 0], [['Следующий трек', 'Next track'], 128, 181, 0],
    [['Стоп', 'Stop'], 128, 183, 0], [['Пуск / пауза', 'Play / pause'], 128, 205, 0], [['Без звука', 'Mute'], 128, 226, 0],
    [['Громкость +', 'Volume +'], 128, 233, 0], [['Громкость −', 'Volume −'], 128, 234, 0], [['Почта', 'Mail'], 128, 138, 1],
    [['Калькулятор', 'Calculator'], 128, 146, 1], [['Мой компьютер', 'This PC'], 128, 148, 1],
  ] },
  { id: 'dpi', name: ['DPI', 'DPI'], items: [
    [['DPI +', 'DPI +'], 64, 2, 0], [['DPI −', 'DPI −'], 64, 3, 0], [['DPI по кругу', 'DPI cycle'], 64, 1, 0],
  ] },
  { id: 'dpilock', name: ['Фиксация DPI', 'DPI lock'], items: Array.from({ length: 11 }, (_, i) => [[`DPI-фикс ${200 + i * 100}`, `DPI lock ${200 + i * 100}`], 80, i + 1, 0]) },
  { id: 'fire', name: ['Огонь', 'Rapid fire'], items: [[['Тройной выстрел', 'Triple shot'], 48, 50, 3]] },
  { id: 'special', name: ['Особые', 'Special'], items: [[['Отключить кнопку', 'Disable button'], 96, 0, 0], [['Вкл/выкл подсветку', 'Lighting on/off'], 96, 4, 0]] },
  { id: 'macro', name: ['Макрос', 'Macro'], items: [] },
];
const nm = pair => L(pair[0], pair[1]);

const BUTTON_POS = [
  [120, 152], [117, 201], [137, 185], [153, 171], [127, 242], [147, 227], [143, 292], [160, 268],
  [167, 231], [163, 56], [173, 95], [196, 41], [258, 33], [246, 76], [262, 165], [266, 203],
];
const LOCKED_BUTTON = 11; // index of the physical left button (12)

const LIGHT_MODES = [
  ['Выключено', 'Off'], ['Мерцание звёзд', 'Twinkling stars'], ['Цветной поток', 'Colorful streaming'],
  ['Семицветное растяжение', 'Seven-color stretch'], ['Семицветное мигание', 'Seven-color flashing'],
  ['Цветное дыхание', 'Colorful breathing'], ['Семицветные вспышки', 'Explosion flash'], ['Цветная заливка', 'Colorful coverage'],
  ['Красный', 'Red'], ['Зелёный', 'Green'], ['Синий', 'Blue'],
];
const LIGHT_SWATCH = [
  '#333', 'radial-gradient(circle,#fff 0 2px,#223 3px) 0 0/12px 10px', 'linear-gradient(90deg,#f00,#ff0,#0f0,#0ff,#00f,#f0f)',
  'linear-gradient(90deg,#f00,#f80,#ff0,#0f0,#0ff,#00f,#90f)', 'repeating-linear-gradient(90deg,#f00 0 14px,#0f0 14px 28px,#00f 28px 42px)',
  'linear-gradient(90deg,#300,#f0f,#003)', 'radial-gradient(circle,#fff,#f80,#f00)', 'linear-gradient(90deg,#0ff,#f0f,#ff0)',
  '#e53935', '#43a047', '#1e88e5',
];

// Standard palette of the MFC colour button used by the vendor Windows app (8×5).
const PALETTE = [
  ['#000000', 'Чёрный', 'Black'], ['#993300', 'Коричневый', 'Brown'], ['#333300', 'Оливково-зелёный', 'Olive green'], ['#003300', 'Тёмно-зелёный', 'Dark green'],
  ['#003366', 'Тёмно-бирюзовый', 'Dark teal'], ['#000080', 'Тёмно-синий', 'Dark blue'], ['#333399', 'Индиго', 'Indigo'], ['#333333', 'Серый 80%', 'Gray 80%'],
  ['#800000', 'Тёмно-красный', 'Dark red'], ['#ff6600', 'Оранжевый', 'Orange'], ['#808000', 'Тёмно-жёлтый', 'Dark yellow'], ['#008000', 'Зелёный', 'Green'],
  ['#008080', 'Сине-зелёный', 'Teal'], ['#0000ff', 'Синий', 'Blue'], ['#666699', 'Сине-серый', 'Blue gray'], ['#808080', 'Серый 50%', 'Gray 50%'],
  ['#ff0000', 'Красный', 'Red'], ['#ff9900', 'Светло-оранжевый', 'Light orange'], ['#99cc00', 'Лайм', 'Lime'], ['#339966', 'Цвет морской волны', 'Sea green'],
  ['#33cccc', 'Аква', 'Aqua'], ['#3366ff', 'Светло-синий', 'Light blue'], ['#800080', 'Фиолетовый', 'Violet'], ['#999999', 'Серый 40%', 'Gray 40%'],
  ['#ff00ff', 'Розовый', 'Pink'], ['#ffcc00', 'Золотой', 'Gold'], ['#ffff00', 'Жёлтый', 'Yellow'], ['#00ff00', 'Ярко-зелёный', 'Bright green'],
  ['#00ffff', 'Бирюзовый', 'Turquoise'], ['#00ccff', 'Голубой', 'Sky blue'], ['#993366', 'Сливовый', 'Plum'], ['#c0c0c0', 'Серый 25%', 'Gray 25%'],
  ['#ff99cc', 'Светло-розовый', 'Rose'], ['#ffcc99', 'Бежевый', 'Tan'], ['#ffff99', 'Светло-жёлтый', 'Light yellow'], ['#ccffcc', 'Бледно-зелёный', 'Light green'],
  ['#ccffff', 'Светло-бирюзовый', 'Light turquoise'], ['#99ccff', 'Бледно-голубой', 'Pale blue'], ['#cc99ff', 'Сиреневый', 'Lavender'], ['#ffffff', 'Белый', 'White'],
];
