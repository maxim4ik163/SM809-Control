'use strict';
// SM809 Control UI. Talks to the background process over a local token-protected API.

const TOKEN = new URLSearchParams(location.search).get('t') || '';
const $ = s => document.querySelector(s);
const esc = s => String(s ?? '').replace(/[&<>"']/g, c => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[c]));
const clone = o => JSON.parse(JSON.stringify(o));
const store = {
  get(k, d) { try { const v = localStorage.getItem('sm809.' + k); return v == null ? d : JSON.parse(v); } catch { return d; } },
  set(k, v) { try { localStorage.setItem('sm809.' + k, JSON.stringify(v)); } catch { /* ignore */ } },
};
const Q = s => L('«' + s + '»', '“' + s + '”'); // quoted name

async function api(path, body) {
  const opt = { headers: { 'X-Token': TOKEN } };
  if (body !== undefined) {
    opt.method = 'POST';
    opt.headers['Content-Type'] = 'application/json';
    opt.body = JSON.stringify(body);
  }
  const r = await fetch('/api/' + path, opt);
  let j = null;
  try { j = await r.json(); } catch { /* empty */ }
  if (!r.ok) throw new Error((j && j.error) || (L('Ошибка ', 'Error ') + r.status));
  return j;
}

function toast(text, err) {
  const d = document.createElement('div');
  d.textContent = text;
  if (err) d.className = 'err';
  $('#toast').appendChild(d);
  setTimeout(() => d.remove(), err ? 6000 : 3500);
}
async function run(fn, okText) {
  try { const r = await fn(); if (okText) toast(okText); return r; } catch (e) { toast(e.message, true); return undefined; }
}

function modal(html, onMount) {
  $('#modalBox').innerHTML = html;
  $('#modal').classList.remove('hidden');
  if (onMount) onMount($('#modalBox'));
}
function closeModal() { $('#modal').classList.add('hidden'); $('#modalBox').innerHTML = ''; $('#modalBox').style.maxWidth = ''; }
$('#modal').addEventListener('mousedown', e => { if (e.target.id === 'modal') closeModal(); });

function confirmBox(title, text, okLabel, danger) {
  return new Promise(res => {
    modal(`<h2>${esc(title)}</h2><div>${text}</div><div class="foot"><button data-r="0">${L('Отмена', 'Cancel')}</button><button class="${danger ? 'danger' : 'primary'}" data-r="1">${esc(okLabel)}</button></div>`,
      box => box.querySelectorAll('[data-r]').forEach(b => b.onclick = () => { closeModal(); res(b.dataset.r === '1'); }));
  });
}
function promptBox(title, value, okLabel = 'OK') {
  return new Promise(res => {
    modal(`<h2>${esc(title)}</h2><input type="text" id="pb" style="width:100%" value="${esc(value)}"><div class="foot"><button data-r="0">${L('Отмена', 'Cancel')}</button><button class="primary" data-r="1">${esc(okLabel)}</button></div>`,
      box => {
        const inp = box.querySelector('#pb');
        inp.focus(); inp.select();
        const done = ok => { closeModal(); res(ok ? inp.value.trim() : null); };
        inp.onkeydown = e => { if (e.key === 'Enter') done(true); if (e.key === 'Escape') done(false); };
        box.querySelectorAll('[data-r]').forEach(b => b.onclick = () => done(b.dataset.r === '1'));
      });
  });
}
const discardChanges = what => confirmBox(L('Несохранённые изменения', 'Unsaved changes'),
  what === 'macro' ? L('Изменения макроса будут потеряны. Продолжить?', 'Your macro changes will be lost. Continue?')
    : L('Изменения профиля будут потеряны. Продолжить?', 'Your profile changes will be lost. Continue?'),
  L('Отбросить', 'Discard'));

// ---------------------------------------------------------------------------
// colour palette popover (standard palette of the vendor app + custom colour)

let palClose = null;
function swatch(attr, color) {
  return `<button type="button" class="swatch" ${attr} title="${esc(color)}"><i style="background:${esc(color)}"></i></button>`;
}
function openPalette(anchor, current, onPick) {
  if (palClose) palClose();
  const box = $('#palette');
  const cur = String(current || '').toLowerCase();
  box.innerHTML = `<div class="pg">${PALETTE.map(([c, ru, en]) => `<div class="pc ${c === cur ? 'on' : ''}" data-c="${c}" title="${esc(L(ru, en))}" style="background:${c}"></div>`).join('')}</div>
    <div class="pf"><input type="color" id="pcol" value="${esc(/^#[0-9a-f]{6}$/.test(cur) ? cur : '#ffffff')}" title="${L('Другой цвет', 'Custom colour')}"><input type="text" id="phex" value="${esc(cur)}" maxlength="7"><button id="pok">OK</button></div>`;
  box.classList.remove('hidden');
  const r = anchor.getBoundingClientRect();
  const w = box.offsetWidth, h = box.offsetHeight;
  box.style.left = Math.max(8, Math.min(innerWidth - w - 8, r.left)) + 'px';
  box.style.top = (r.bottom + h + 8 > innerHeight ? Math.max(8, r.top - h - 6) : r.bottom + 6) + 'px';
  const pick = c => { close(); onPick(c.toLowerCase()); };
  box.querySelectorAll('[data-c]').forEach(el => el.onclick = () => pick(el.dataset.c));
  const hex = box.querySelector('#phex');
  box.querySelector('#pcol').oninput = e => { hex.value = e.target.value; };
  box.querySelector('#pcol').onchange = e => pick(e.target.value);
  const okHex = () => { const v = hex.value.trim(); if (/^#?[0-9a-f]{6}$/i.test(v)) pick(v.startsWith('#') ? v : '#' + v); else toast(L('Цвет в формате #RRGGBB', 'Colour must be #RRGGBB'), true); };
  box.querySelector('#pok').onclick = okHex;
  hex.onkeydown = e => { if (e.key === 'Enter') okHex(); };
  const outside = e => { if (!box.contains(e.target) && !anchor.contains(e.target)) close(); };
  const onKey = e => { if (e.key === 'Escape') close(); };
  setTimeout(() => { document.addEventListener('mousedown', outside); document.addEventListener('keydown', onKey); });
  function close() {
    box.classList.add('hidden');
    document.removeEventListener('mousedown', outside);
    document.removeEventListener('keydown', onKey);
    palClose = null;
  }
  palClose = close;
}

// ---------------------------------------------------------------------------
// state

let S = null;             // {data, state, autostart, autostartMode, elevated, version}
let tab = store.get('tab', 'buttons');
let editId = null;        // profile being edited
let draft = null;         // working copy of that profile
let dirty = false;
let selBtn = 0;
let btnCat = 'mouse';
let mEditId = null, mDraft = null, mDirty = false, mSel = -1;
let rulesDraft = null, rulesDirty = false;
let setDraft = null, setDirty = false, autostartDraft = null;
let dismissedErr = '';

const profiles = () => S.data.profiles;
const profileById = id => profiles().find(p => p.id === id);
const macroById = id => (S.data.macros || []).find(m => m.id === id);
const st = () => S.state;

async function reload() {
  S = await api('all');
  LANG = S.data.settings.language === 'ru' ? 'ru' : 'en';
  document.documentElement.lang = LANG;
  if (!profileById(editId)) { editId = st().current || S.data.settings.baseProfile; dirty = false; }
  if (!dirty) draft = clone(profileById(editId));
  if (mEditId && !macroById(mEditId) && mEditId !== 'new') { mEditId = null; mDraft = null; mDirty = false; }
  if (!rulesDirty) rulesDraft = clone(S.data.rules || []);
  if (!setDirty) { setDraft = clone(S.data.settings); autostartDraft = S.autostart; }
  if (!softKeys) softKeys = await api('soft/keys').catch(() => []);
  if (!softLayouts.length) softLayouts = await api('soft/layouts').catch(() => []);
  renderAll();
}

function startEvents() {
  const es = new EventSource('/api/events?t=' + encodeURIComponent(TOKEN));
  let lastCurrent = null;
  es.onmessage = ev => {
    if (!S) return;
    S.state = JSON.parse(ev.data);
    renderTop();
    renderEditbar();
    if (lastCurrent !== null && lastCurrent !== S.state.current && tab === 'profiles' && !rulesDirty) renderView();
    if (tab === 'battery') batteryLive();
    if (tab === 'buttons') buttonsLive();
    lastCurrent = S.state.current;
  };
}

// ---------------------------------------------------------------------------
// header / banners

const modeName = m => ({ wired: L('Провод', 'Wired'), dongle: L('Донгл 2.4G', '2.4G dongle') }[m]
  || (m && m.startsWith('bt') ? 'Bluetooth ' + m.slice(2) : m));
const POLL_HZ = { 8: 125, 4: 250, 2: 500, 1: 1000, 17: 2000, 18: 4000, 20: 8000 };
const pollLabel = v => POLL_HZ[v] ? POLL_HZ[v] + L(' Гц', ' Hz') : '?';
const cmdName = k => ({
  keys: L('кнопки и макросы', 'buttons and macros'), dpi: 'DPI', dpiColors: L('цвета DPI', 'DPI colours'),
  poll: L('частота опроса', 'polling rate'), lod: 'LOD', power: L('энергосбережение', 'power saving'),
  sensor: L('сенсор', 'sensor'), light: L('подсветка', 'lighting'),
}[k] || k);

function renderTop() {
  const s = st();
  const devs = s.devices || [];
  let h = '';
  if (!devs.length) h = `<span class="chip off"><span class="dot bad"></span>${L('Мышь не найдена', 'Mouse not found')}</span>`;
  for (const d of devs) {
    const isP = s.primary && d.mode === s.primary.mode && d.pid === s.primary.pid;
    h += `<span class="chip ${d.link ? 'on' : 'off'}"><span class="dot ${d.link ? 'ok' : ''}"></span><b>${esc(modeName(d.mode))}</b>`;
    if (d.link && isP) {
      h += ` <span class="bat"><i style="width:${Math.max(2, Math.round(d.battery * 0.21))}px;background:${d.battery <= 15 ? 'var(--err)' : d.battery <= 30 ? 'var(--warn)' : 'var(--accent)'}"></i></span> ${d.battery}%`;
      if (d.charge === 1) h += ' ⚡'; else if (d.charge === 2) h += ' ✓';
      const f = s.battery;
      if (f && f.hours > 0) h += `<span class="muted"> · ${f.state === 'charging' ? L('до полной ', 'full in ') : '≈ '}${fmtHours(f.hours)}</span>`;
    } else if (!d.link) h += ' ' + L('нет связи', 'offline');
    h += '</span>';
  }
  if (s.primary && s.primary.link) {
    const p = profileById(s.active || s.current);
    const lvl = s.primary.level;
    const dpi = p && p.dpiLevels[lvl] ? p.dpiLevels[lvl] : '';
    h += `<span class="chip">${esc(s.sensor.Name)}</span><span class="chip">${pollLabel(s.primary.rate)}</span>`;
    h += `<span class="chip">DPI ${lvl + 1}${dpi ? ' · ' + dpi : ''}</span>`;
  }
  const bm = S.data.settings.buttonMode;
  h += `<button class="chip modechip ${bm === 'software' ? 'on' : ''}" id="modeChip" title="${L('Режим кнопок — нажмите, чтобы сменить', 'Button mode — click to change')}">${L('Кнопки', 'Buttons')}: <b>${modeTitle(bm)}</b></button>`;
  if ($('#device').dataset.h !== h) { $('#device').dataset.h = h; $('#device').innerHTML = h; $('#modeChip').onclick = () => modeDialog(false); }

  const cur = profileById(s.current);
  let c = `${L('Профиль', 'Profile')}: <b>${esc(cur ? cur.name : '—')}</b>`;
  const top = (s.stack || []).slice(-1)[0];
  c += top ? '<br>' + L('по правилу: ', 'by rule: ') + esc(top.exe) : '<br>' + L('выбран вручную', 'selected manually');
  if (isSoftMode()) {
    const bp = profileById(softStat().profile);
    if (bp && bp.id !== s.current) c += '<br>' + L('кнопки: ', 'buttons: ') + `<b>${esc(bp.name)}</b>`;
  }
  if (s.busy) c += `<br><span style="color:var(--warn)">${esc(s.progress || L('запись…', 'writing…'))}</span>`;
  $('#current').innerHTML = c;

  $('#navfoot').innerHTML = `${L('Версия', 'Version')} ${esc(S.version)}<br>${L('Записей во флеш', 'Flash writes')}: ${s.flashWrites}`;

  let b = '';
  if (!s.synced) {
    b += `<div class="bn warn"><span><b>${L('Мышь ещё не синхронизирована.', 'The mouse is not synchronised yet.')}</b> ${L(
      'Программа ещё не знает, что записано в мыши: подключите кабель и прочитайте настройки из мыши или один раз запишите профиль целиком. До этого автопереключение по играм выключено.',
      'The app does not know yet what is stored in the mouse: connect the cable and read the settings from the mouse, or write a profile in full once. Until then, switching by games is disabled.')}</span>
      ${s.primary && s.primary.mode === 'wired' ? `<button id="bnRead">${L('Прочитать из мыши', 'Read from the mouse')}</button>` : ''}
      <button class="primary" id="bnSync" ${s.primary && s.primary.link && !s.busy ? '' : 'disabled'}>${L('Записать', 'Write')} ${esc(Q(cur ? cur.name : ''))} ${L('в мышь', 'to the mouse')}</button></div>`;
  }
  if (s.pending) {
    const p = profileById(s.pending);
    b += `<div class="bn info"><span>${L('Профиль', 'Profile')} ${esc(Q(p ? p.name : ''))} ${L('будет записан, когда мышь появится на связи.', 'will be written when the mouse reconnects.')}</span></div>`;
  }
  if (s.lastError && s.lastError !== dismissedErr) {
    b += `<div class="bn err"><span>${L('Ошибка', 'Error')}: ${esc(s.lastError)}</span><button class="small" id="bnErr">${L('Скрыть', 'Dismiss')}</button></div>`;
  }
  if ($('#banner').dataset.h === b) return;
  $('#banner').dataset.h = b;
  $('#banner').innerHTML = b;
  const sy = $('#bnSync');
  if (sy) sy.onclick = () => run(() => api('profile/apply', { id: st().current, force: true }), L('Записываю профиль в мышь…', 'Writing the profile to the mouse…'));
  const br = $('#bnRead');
  if (br) br.onclick = () => readFromMouse(br);
  const be = $('#bnErr');
  if (be) be.onclick = () => { dismissedErr = st().lastError; renderTop(); };
}

let lastMsgShown = '';
function showLastMessage() {
  const m = st().lastMessage;
  if (m && m !== lastMsgShown) { lastMsgShown = m; toast(m); }
}

// ---------------------------------------------------------------------------
// edit bar (profile being edited)

const PROFILE_TABS = ['buttons', 'dpi', 'light', 'perf'];

function renderEditbar() {
  const bar = $('#editbar');
  if (tab === 'settings') return settingsBar(bar);
  if (!PROFILE_TABS.includes(tab)) { bar.style.display = 'none'; bar.dataset.h = ''; return; }
  bar.style.display = '';
  const s = st();
  const isCur = editId === s.current;
  const pend = isCur && !dirty ? (s.pendingChanges || []) : [];
  let h = `<span class="muted">${L('Профиль', 'Profile')}</span><select id="ebSel">${profiles().map(p => `<option value="${p.id}" ${p.id === editId ? 'selected' : ''}>${esc(p.name)}${p.id === s.current ? L('  ● в мыши', '  ● on the mouse') : ''}${isSoftMode() && p.id === softStat().profile && p.id !== s.current ? L('  ● кнопки сейчас', '  ● buttons now') : ''}</option>`).join('')}</select>`;
  h += '<span class="grow"></span>';
  if (dirty) h += `<span class="dirty">● ${L('есть несохранённые изменения', 'unsaved changes')}</span>`;
  else if (pend.length && s.synced) h += `<span class="pend">${L('Не записано в мышь', 'Not written to the mouse')}: ${pend.map(cmdName).join(', ')}</span>`;
  h += `<button id="ebRevert" ${dirty ? '' : 'disabled'}>${L('Отменить', 'Revert')}</button>`;
  if (isCur) {
    const can = (dirty || (pend.length && s.synced)) && !s.busy;
    h += `<button class="primary" id="ebSave" ${can ? '' : 'disabled'}>${dirty ? L('Сохранить и записать в мышь', 'Save and write to the mouse') : L('Записать в мышь', 'Write to the mouse')}</button>`;
  } else {
    h += `<button class="primary" id="ebSave" ${dirty ? '' : 'disabled'}>${L('Сохранить', 'Save')}</button>`;
    h += `<button id="ebUse" ${s.busy ? 'disabled' : ''}>${L('Включить этот профиль', 'Use this profile')}</button>`;
  }
  if (bar.dataset.h === h) return;
  bar.dataset.h = h;
  bar.innerHTML = h;
  $('#ebSel').onchange = async e => {
    if (dirty && !await discardChanges()) { e.target.value = editId; return; }
    editId = e.target.value; draft = clone(profileById(editId)); dirty = false; renderAll();
  };
  $('#ebRevert').onclick = () => { draft = clone(profileById(editId)); dirty = false; renderAll(); };
  $('#ebSave').onclick = saveProfile;
  const u = $('#ebUse');
  if (u) u.onclick = async () => {
    if (dirty && !await saveDraft()) return;
    await run(() => api('profile/apply', { id: editId, makeBase: true }), L('Профиль включён', 'Profile activated'));
    await reload();
  };
}

function settingsBar(bar) {
  bar.style.display = '';
  const h = `<span class="muted">${L('Настройки', 'Settings')}</span><span class="grow"></span>`
    + (setDirty ? `<span class="dirty">● ${L('есть несохранённые изменения', 'unsaved changes')}</span>` : `<span class="pend">${L('Язык и переключение по играм сохраняются сразу', 'Language and game switching are saved immediately')}</span>`)
    + `<button id="srev" ${setDirty ? '' : 'disabled'}>${L('Отменить', 'Revert')}</button><button class="primary" id="ssave" ${setDirty ? '' : 'disabled'}>${L('Сохранить настройки', 'Save settings')}</button>`;
  if (bar.dataset.h === h) return;
  bar.dataset.h = h;
  bar.innerHTML = h;
  $('#srev').onclick = () => { setDraft = clone(S.data.settings); autostartDraft = S.autostart; setDirty = false; renderEditbar(); renderView(); };
  $('#ssave').onclick = async () => {
    if (await run(() => api('settings', { settings: setDraft, autostart: autostartDraft }), L('Настройки сохранены', 'Settings saved'))) { setDirty = false; await reload(); }
  };
}

async function readFromMouse(btn) {
  if (btn) btn.disabled = true;
  const r = await run(() => api('mouse/read', {}));
  if (btn) btn.disabled = false;
  if (!r) return;
  await reload();
  const p = r.profile;
  const btns = p.buttons.map((b, i) => `<tr><td class="muted">${i + 1}</td><td>${esc(b.kind === 'macro' ? L('Макрос', 'Macro') + ' (' + ((r.macros.find(m => m.id === b.macroId) || {}).events || []).length + ' ' + L('действий', 'actions') + ')' : assignLabel(b))}</td></tr>`).join('');
  const same = r.differences.length === 0;
  modal(`<h2>${L('Настройки в мыши', 'Settings in the mouse')}</h2>
    <p>${same ? L(`Совпадает с профилем ${esc(Q(r.compared))} — записывать ничего не нужно.`, `Identical to profile ${esc(Q(r.compared))} — nothing needs to be written.`)
      : L(`Отличается от профиля ${esc(Q(r.compared))}: `, `Differs from profile ${esc(Q(r.compared))}: `) + r.differences.map(cmdName).join(', ') + '.'}</p>
    <div class="grid2" style="gap:12px"><div><table class="small">${btns}</table></div>
    <div class="small"><p><b>DPI:</b> ${p.dpiLevels.slice(0, p.dpiCount).map((d, i) => `<span style="color:${p.dpiColors[i]}">●</span> ${d}${i === p.dpiStart ? '*' : ''}`).join(' &nbsp;')}</p>
      <p><b>${L('Подсветка', 'Lighting')}:</b> ${esc(nm(LIGHT_MODES[p.lighting] || ['?', '?']))}</p>
      <p><b>${L('Опрос', 'Polling')}:</b> ${pollLabel(p.pollRate)} · <b>LOD:</b> ${p.lod} ${L('мм', 'mm')}</p>
      <p><b>${L('Сон', 'Sleep')}:</b> ${p.sleepMin} ${L('мин', 'min')} · ${L('пробуждение движением', 'wake on move')}: ${p.moveWakeup ? '✓' : '—'} · ${L('гасить при движении', 'light off moving')}: ${p.moveLightOff ? '✓' : '—'}</p>
      <p><b>${L('Сенсор', 'Sensor')}:</b> angle snap ${p.angleSnap ? '✓' : '—'} · Motion Sync ${p.motionSync ? '✓' : '—'} · Ripple ${p.ripple ? '✓' : '—'}</p>
      <p class="muted">${L('Антидребезг кнопок (скрытый параметр)', 'Button debounce (hidden setting)')}: ${r.debounce}</p></div></div>
    <p class="muted small">${L('Теперь программа знает точное содержимое мыши: дальше будут записываться только реальные отличия.', 'The app now knows the exact mouse contents: from now on only real differences are written.')}</p>
    <div class="foot"><button data-r="close">${L('Закрыть', 'Close')}</button>
      ${same ? '' : `<button data-r="write">${L('Записать', 'Write')} ${esc(Q(r.compared))}</button>`}
      <button class="primary" data-r="import">${L('Создать профиль из мыши', 'Create profile from the mouse')}</button></div>`, box => {
    box.querySelector('[data-r=close]').onclick = closeModal;
    const w = box.querySelector('[data-r=write]');
    if (w) w.onclick = async () => { closeModal(); await run(() => api('profile/apply', { id: st().current }), L('Записываю в мышь…', 'Writing to the mouse…')); await reload(); };
    box.querySelector('[data-r=import]').onclick = async () => {
      closeModal();
      const name = await promptBox(L('Имя нового профиля', 'New profile name'), L('Из мыши', 'From the mouse'), L('Создать', 'Create'));
      if (!name) return;
      const res = await run(() => api('mouse/import', { name }), L('Профиль создан и выбран — в мышь ничего не записывалось', 'Profile created and selected — nothing was written to the mouse'));
      if (res) { editId = res.id; dirty = false; await reload(); }
    };
  });
}

function autoSwitchWarning() {
  return new Promise(res => {
    modal(`<h2>${L('Внимание: износ памяти мыши', 'Warning: mouse memory wear')}</h2>
      <div class="warnbox">${L(`<p>Мышь хранит настройки во флеш-памяти. Каждое переключение профиля перезаписывает её: и при запуске игры, и при выходе из неё. Обычно 1–3 перезаписи в каждую сторону, по одной на каждую группу настроек, которая отличается (кнопки, DPI, подсветка, частота…).</p>
        <p>Ресурс флеш-памяти ограничен. Производитель чипа его не указывает; для такой памяти это обычно от 10 000 до 100 000 перезаписей. Когда ресурс исчерпан, мышь может начать терять настройки.</p>
        <p>Пример: 10 запусков игры в день с тремя отличающимися группами — около 60 перезаписей в день. При ресурсе 10 000 это примерно полгода, при 100 000 — несколько лет.</p>
        <p><b>Как беречь мышь:</b> делайте отличия в игровых профилях только там, где они нужны; не привязывайте программы, которые часто открываются и закрываются (браузер, мессенджер). Счётчик записей виден в «Настройки → Мышь».</p>`,
      `<p>The mouse stores its settings in flash memory. Every profile switch rewrites it — both when a game starts and when it exits. Usually 1–3 rewrites each way, one for every settings group that differs (buttons, DPI, lighting, polling rate…).</p>
        <p>Flash endurance is limited. The chip maker does not state it; for this kind of memory it is typically 10,000 to 100,000 rewrites. Once it is worn out, the mouse may start losing its settings.</p>
        <p>Example: 10 game launches a day with three differing groups is about 60 rewrites a day. With 10,000 cycles that is roughly half a year; with 100,000 — several years.</p>
        <p><b>How to spare the mouse:</b> make game profiles differ only where needed; do not bind programs that are opened and closed often (browser, messenger). The write counter is under Settings → Mouse.</p>`)}</div>
      <label class="check" style="margin-top:12px"><input type="checkbox" id="wok">${L('Я понимаю риск и хочу включить переключение по играм', 'I understand the risk and want to enable switching by games')}</label>
      <div class="foot"><button data-r="0">${L('Отмена', 'Cancel')}</button><button class="primary" data-r="1" id="wgo" disabled>${L('Включить', 'Enable')}</button></div>`, box => {
      box.querySelector('#wok').onchange = e => { box.querySelector('#wgo').disabled = !e.target.checked; };
      box.querySelectorAll('[data-r]').forEach(b => b.onclick = () => { closeModal(); res(b.dataset.r === '1'); });
    });
  });
}

async function saveDraft() {
  const r = await run(() => api('profile/save', draft));
  if (!r) return false;
  dirty = false;
  return true;
}
async function saveProfile() {
  const isCur = editId === st().current;
  if (dirty && !await saveDraft()) return;
  if (isCur) await run(() => api('profile/apply', { id: editId }), L('Записываю в мышь…', 'Writing to the mouse…'));
  else toast(L('Профиль сохранён', 'Profile saved'));
  await reload();
}
function touch() { dirty = true; renderEditbar(); }

// ---------------------------------------------------------------------------
// tabs

const TABS = [
  ['buttons', 'Кнопки', 'Buttons'], ['dpi', 'DPI', 'DPI'], ['light', 'Подсветка', 'Lighting'],
  ['perf', 'Производительность', 'Performance'], ['macros', 'Макросы', 'Macros'],
  ['profiles', 'Профили и игры', 'Profiles & games'], ['battery', 'Батарея', 'Battery'], ['settings', 'Настройки', 'Settings'],
];

function renderNav() {
  const nav = $('#tabs');
  nav.innerHTML = TABS.map(([id, ru, en]) => `<button data-tab="${id}" class="${id === tab ? 'on' : ''}">${L(ru, en)}</button>`).join('') + '<div class="navfoot" id="navfoot"></div>';
  nav.querySelectorAll('[data-tab]').forEach(b => b.onclick = () => { tab = b.dataset.tab; store.set('tab', tab); renderAll(); });
}

function renderAll() {
  if (palClose) palClose();
  renderNav();
  renderTop();
  renderEditbar();
  renderView();
}

function renderView() {
  const v = $('#view');
  const fn = { buttons: viewButtons, dpi: viewDPI, light: viewLight, perf: viewPerf, macros: viewMacros, profiles: viewProfiles, battery: viewBattery, settings: viewSettings }[tab] || viewButtons;
  v.innerHTML = '';
  fn(v);
}

// ---------------------------------------------------------------------------
// Buttons

function codeEq(a, b) { return a[0] === b[0] && a[1] === b[1] && a[2] === b[2]; }
function isLeft(b) { return b.kind !== 'macro' && codeEq(b.code, [16, 1, 0]); }

function comboLabel(mods, key) {
  const p = [];
  if (mods & 1) p.push('Ctrl'); if (mods & 2) p.push('Shift'); if (mods & 4) p.push('Alt'); if (mods & 8) p.push('Win');
  if (key) p.push(keyName(key));
  return p.join('+') || '—';
}
const fireLabel = (i, c) => L(`Огонь (${i} мс${c ? ', ' + c : ''})`, `Rapid fire (${i} ms${c ? ', ' + c : ''})`);

// The label is computed from the code so that it follows the UI language.
function assignLabel(b) {
  if (b.kind === 'macro') { const m = macroById(b.macroId); return L('Макрос: ', 'Macro: ') + (m ? m.name : L('(удалён)', '(deleted)')); }
  for (const c of CATALOG) for (const it of c.items) if (codeEq(b.code, it.slice(1))) return nm(it[0]);
  if (b.code[0] === 112) return comboLabel(b.code[1], b.code[2]);
  if (b.code[0] === 48) return fireLabel(b.code[1], b.code[2]);
  return b.label || '—';
}
function catOf(b) {
  if (b.kind === 'macro') return 'macro';
  for (const c of CATALOG) if (c.items.some(it => codeEq(b.code, it.slice(1)))) return c.id;
  return { 16: 'mouse', 112: 'key', 128: 'media', 64: 'dpi', 80: 'dpilock', 48: 'fire', 96: 'special' }[b.code[0]] || 'mouse';
}

function setButton(i, a) {
  const nb = draft.buttons.slice();
  nb[i] = a;
  if (!nb.some((b, k) => isLeft(b) && !isSoftBtn(k))) { toast(L('Хотя бы одна аппаратная кнопка должна оставаться левым кликом', 'At least one hardware button must stay a left click'), true); return; }
  draft.buttons = nb;
  touch();
  renderView();
}

function viewButtons(v) {
  const sw = isSoftMode(), held = softStat().held || [];
  let hs = '', list = '';
  const items = [];
  BUTTON_POS.forEach(([x, y], i) => {
    const soft = isSoftBtn(i);
    hs += `<div class="hot ${i === selBtn ? 'sel' : ''} ${i === LOCKED_BUTTON ? 'locked' : ''} ${soft ? 'soft' : ''} ${held.includes('b' + (i + 1)) ? 'held' : ''}" style="left:${x}px;top:${y}px" data-b="${i}" title="${L('Кнопка', 'Button')} ${i + 1}: ${esc(btnLabel(i))}"></div>`;
    const badge = i === LOCKED_BUTTON ? '🔒' : '';
    items[i] = `<div class="bitem ${i === selBtn ? 'sel' : ''} ${soft ? 'soft' : ''}" data-b="${i}" style="grid-area:b${i + 1}" title="${esc(btnLabel(i))}${sw ? ' — ' + (soft ? L('программная', 'software') : L('аппаратная', 'hardware')) : ''}"><span class="n">${i + 1}</span><span class="l">${esc(btnLabel(i))}</span>${badge ? `<span class="k">${badge}</span>` : ''}</div>`;
  });
  // laid out like the buttons on the mouse: the side panel 1–9, the top 10–16
  list = `<h3 class="bgh">${L('Боковые кнопки', 'Side buttons')}</h3><div class="bgrid side">${items.slice(0, 9).join('')}</div>
    <h3 class="bgh">${L('Верх мыши', 'Top of the mouse')}</h3><div class="bgrid top">${items.slice(9).join('')}</div>`;
  const help = sw
    ? L('<span class="numk soft">●</span> зелёный номер — программная кнопка: её выполняет SM809 Control по профилю активной программы, без записи в мышь.<br><span class="numk">●</span> красный — аппаратная: действие хранится в мыши. Тип меняется галочкой в настройке кнопки (кроме основной левой).<br>Нажмите кнопку на мыши — программная подсветится здесь.',
      '<span class="numk soft">●</span> a green number is a software button: SM809 Control performs it by the profile of the program in front, without writing the mouse.<br><span class="numk">●</span> red is a hardware button: the action is stored in the mouse. Change the type with the checkbox in the button settings (all but the main left button).<br>Press a button on the mouse — a software one lights up here.')
    : L('Нажмите на номер на картинке или в списке, чтобы назначить функцию.<br>Кнопка 12 — основная левая, она заблокирована, чтобы мышью всегда можно было кликнуть.',
      'Click a number on the picture or in the list to assign a function.<br>Button 12 is the main left button; it is locked so the mouse can always click.');
  v.innerHTML = (sw ? `<div class="card slim small" id="btnStat">${buttonsStatusHTML()}</div>` : '') + `<div class="btnlayout">
    <div><div class="mouse"><img src="mouse.png" alt="">${hs}</div>
      <p class="muted small">${help}</p>
      <p class="small">${L('Режим кнопок', 'Button mode')}: <b>${modeTitle(S.data.settings.buttonMode)}</b> · <a href="#" id="chMode">${L('изменить', 'change')}</a></p></div>
    <div><div class="card">${list}</div><div class="card" id="bed"></div></div></div>`;
  v.querySelectorAll('[data-b]').forEach(el => el.onclick = () => {
    selBtn = +el.dataset.b; btnCat = catOf(draft.buttons[selBtn]); renderView();
  });
  v.querySelector('#chMode').onclick = e => { e.preventDefault(); modeDialog(false); };
  renderButtonEditor(v.querySelector('#bed'));
}

function renderButtonEditor(box) {
  const i = selBtn, b = draft.buttons[i];
  let title = `<h2>${L('Кнопка', 'Button')} ${i + 1}: ${esc(btnLabel(i))}</h2>`;
  if (i === LOCKED_BUTTON) {
    box.innerHTML = title + `<p class="muted">${L('Это основная левая кнопка мыши. Её функция не меняется — так мышь нельзя случайно «сломать» неработающим кликом.',
      'This is the main left button. Its function cannot be changed, so the mouse can never be left without a working click.')}</p>`;
    return;
  }
  if (isSoftMode()) {
    const hw = S.data.settings.hwButtons[i];
    title += `<label class="check hwtoggle"><input type="checkbox" id="hwchk" ${hw ? 'checked' : ''}><span>${L('Аппаратная кнопка', 'Hardware button')}<small>${hw
      ? L('действие хранится в мыши и работает без программы; можно назначить огонь, DPI, макрос прошивки', 'the action is stored in the mouse and works without the app; rapid fire, DPI and firmware macros are available')
      : L('действие выполняет SM809 Control по профилю активной программы; без программы кнопка ничего не делает', 'SM809 Control performs the action by the profile of the program in front; without the app the button does nothing')}</small></span></label>`;
  }
  // reset to the factory assignment (hardware) or its software equivalent
  const soft = isSoftBtn(i);
  const defB = S.defaults.buttons[i], defS = S.defaults.soft[i];
  const isDef = soft ? JSON.stringify(draft.soft[i] || {}) === JSON.stringify(defS) : (draft.buttons[i].kind !== 'macro' && codeEq(draft.buttons[i].code, defB.code));
  title += `<div class="row" style="margin:-4px 0 10px"><button class="small" id="bdef" ${isDef ? 'disabled' : ''} title="${L('Заводское назначение этой кнопки', 'The factory assignment of this button')}">↺ ${L('По умолчанию', 'Default')}: ${esc(soft ? softLabel(defS) : assignLabel(defB))}</button></div>`;
  const bindHW = () => {
    const c = box.querySelector('#hwchk'); if (c) c.onchange = () => toggleHW(i, c.checked);
    const d = box.querySelector('#bdef');
    if (d) d.onclick = () => {
      if (soft) { draft.soft[i] = clone(defS); touch(); renderView(); }
      else { btnCat = catOf(defB); setButton(i, clone(defB)); }
    };
  };
  if (isSoftBtn(i)) {
    box.innerHTML = title + '<div id="sbody"></div>';
    bindHW();
    softButtonEditor(box.querySelector('#sbody'), i);
    return;
  }
  let h = title + '<div class="cats">';
  for (const c of CATALOG) h += `<button data-cat="${c.id}" class="${c.id === btnCat ? 'on' : ''}">${nm(c.name)}</button>`;
  h += '</div><div id="catBody"></div>';
  box.innerHTML = h;
  bindHW();
  box.querySelectorAll('[data-cat]').forEach(el => el.onclick = () => { btnCat = el.dataset.cat; renderButtonEditor(box); });
  const body = box.querySelector('#catBody');
  const cat = CATALOG.find(c => c.id === btnCat);

  if (btnCat === 'key') return comboEditor(body, b);
  if (btnCat === 'fire') return fireEditor(body, b, cat);
  if (btnCat === 'macro') return macroAssign(body, b);

  const kind = cat.kind || cat.id;
  body.innerHTML = `<div class="catalog">${cat.items.map((it, k) =>
    `<button data-k="${k}" class="${b.kind !== 'macro' && codeEq(b.code, it.slice(1)) ? 'cur' : ''}">${esc(nm(it[0]))}</button>`).join('')}</div>`
    + (btnCat === 'dpilock' ? `<p class="muted small">${L('Пока кнопка зажата, мышь работает с фиксированным DPI (снайперский режим).', 'While the button is held, the mouse uses a fixed DPI (sniper mode).')}</p>` : '')
    + (btnCat === 'special' ? `<p class="muted small">${L('«Отключить кнопку» — кнопка ничего не делает.', '“Disable button” makes the button do nothing.')}</p>` : '');
  body.querySelectorAll('[data-k]').forEach(el => el.onclick = () => {
    const it = cat.items[+el.dataset.k];
    setButton(i, { kind, code: it.slice(1), label: nm(it[0]) });
  });
}

function keyOptions(withMouse) {
  return KEY_GROUPS().map(([g, ks]) => `<optgroup label="${esc(g)}">${ks.map(k => `<option value="${k}">${esc(keyName(k))}</option>`).join('')}</optgroup>`).join('')
    + (withMouse ? `<optgroup label="${L('Мышь', 'Mouse')}">${MOUSE_KEYS.map(k => `<option value="${k}">${esc(keyName(k))}</option>`).join('')}</optgroup>` : '');
}

function comboEditor(body, b) {
  let mods = 0, key = 0;
  if (b.kind !== 'macro' && b.code[0] === 112) { mods = b.code[1]; key = b.code[2]; }
  const capText = () => key ? keyName(key) : L('нажмите, затем нажмите клавишу', 'click, then press a key');
  body.innerHTML = `<div class="row">${['Ctrl', 'Shift', 'Alt', 'Win'].map((m, k) => `<label class="check"><input type="checkbox" data-m="${1 << k}" ${mods & (1 << k) ? 'checked' : ''}>${m}</label>`).join('')}</div>
    <div class="row"><span class="keycap" id="cap" tabindex="0">${esc(capText())}</span>
    <span class="muted">${L('или', 'or')}</span><select id="ksel"><option value="0">${L('— выбрать —', '— choose —')}</option>${keyOptions(false)}</select></div>
    <div class="row"><span class="muted">${L('Будет', 'Result')}: <b id="cprev">${esc(comboLabel(mods, key))}</b></span><span class="grow"></span><button class="primary" id="cset">${L('Назначить', 'Assign')}</button></div>
    <p class="muted small">${L('Можно назначить одну клавишу или сочетание с Ctrl/Shift/Alt/Win. Для последовательностей используйте макросы.',
      'Assign a single key or a combination with Ctrl/Shift/Alt/Win. Use macros for sequences.')}</p>`;
  const upd = () => { body.querySelector('#cprev').textContent = comboLabel(mods, key); body.querySelector('#cap').textContent = capText(); };
  body.querySelectorAll('[data-m]').forEach(el => el.onchange = () => { mods = el.checked ? mods | +el.dataset.m : mods & ~+el.dataset.m; upd(); });
  const sel = body.querySelector('#ksel');
  if (key) sel.value = key;
  sel.onchange = () => { key = +sel.value; upd(); };
  const cap = body.querySelector('#cap');
  cap.onclick = () => {
    cap.classList.add('listen'); cap.textContent = L('нажмите клавишу…', 'press a key…');
    const on = e => {
      e.preventDefault(); e.stopPropagation();
      const c = HID[e.code];
      if (!c) { toast(L('Эту клавишу назначить нельзя', 'This key cannot be assigned'), true); return; }
      const modBit = { 224: 1, 228: 1, 225: 2, 229: 2, 226: 4, 230: 4, 227: 8, 231: 8 }[c];
      window.removeEventListener('keydown', on, true);
      cap.classList.remove('listen');
      if (modBit && !key) { mods |= modBit; body.querySelectorAll('[data-m]').forEach(el => el.checked = !!(mods & +el.dataset.m)); }
      else { key = c; sel.value = c; }
      upd();
    };
    window.addEventListener('keydown', on, true);
  };
  body.querySelector('#cset').onclick = () => {
    if (!key && !mods) { toast(L('Выберите клавишу', 'Choose a key'), true); return; }
    setButton(selBtn, { kind: 'key', code: [112, mods, key], label: comboLabel(mods, key) });
  };
}

function fireEditor(body, b, cat) {
  let iv = 50, cnt = 0;
  if (b.kind !== 'macro' && b.code[0] === 48) { iv = b.code[1]; cnt = b.code[2]; }
  body.innerHTML = `<div class="row"><label class="field"><span>${L('Интервал между кликами, мс', 'Interval between clicks, ms')}</span><input type="number" id="fiv" min="1" max="255" value="${iv}"></label>
    <label class="field"><span>${L('Число кликов (0 — пока зажата)', 'Number of clicks (0 = while held)')}</span><input type="number" id="fcnt" min="0" max="255" value="${cnt}"></label>
    <span class="grow"></span><button class="primary" id="fset">${L('Назначить', 'Assign')}</button></div>
    <div class="row"><span class="muted">${L('Готовое', 'Preset')}:</span>${cat.items.map((it, k) => `<button class="small" data-k="${k}">${esc(nm(it[0]))}</button>`).join('')}</div>
    <p class="muted small">${L('«Огонь» — кнопка повторяет левый клик с заданным интервалом.', 'Rapid fire repeats the left click at the given interval.')}</p>`;
  body.querySelector('#fset').onclick = () => {
    const i = Math.max(1, Math.min(255, +body.querySelector('#fiv').value | 0));
    const c = Math.max(0, Math.min(255, +body.querySelector('#fcnt').value | 0));
    setButton(selBtn, { kind: 'fire', code: [48, i, c], label: fireLabel(i, c) });
  };
  body.querySelectorAll('[data-k]').forEach(el => el.onclick = () => {
    const it = cat.items[+el.dataset.k];
    setButton(selBtn, { kind: 'fire', code: it.slice(1), label: nm(it[0]) });
  });
}

const execName = t => ({ 1: L('пока кнопка зажата', 'while held'), 2: L('заданное число раз', 'N times'), 3: L('до повторного нажатия', 'until pressed again') }[t]);
// ---------------------------------------------------------------------------
// Macro list with folders (Macros tab and the macro pickers)

const macroSub = m => `${m.events.length} ${L('действий', 'actions')} · ${execName(m.executeType)}${m.executeType === 2 ? ' (' + m.repeatCount + ')' : ''}`;
const closedGroups = () => new Set(store.get('mClosed', []));
function setGroupClosed(g, closed) {
  const c = closedGroups();
  if (closed) c.add(g); else c.delete(g);
  store.set('mClosed', [...c]);
}
const sameGroup = (a, b) => (a || '').toLowerCase() === (b || '').toLowerCase();
// Folder names in display order, plus folders that only a draft uses.
function groupNames(extra) {
  const gs = [...(S.data.macroGroups || [])];
  if (extra && !gs.some(g => sameGroup(g, extra))) gs.push(extra);
  return gs;
}
const listQuery = {}; // search text per list ('tab', 'pick')

// mountMacroList renders macros grouped by folder into box.
// o: {key, cur, onPick(id), sub(m), manage, draft} — manage adds folder actions and drag-and-drop.
function mountMacroList(box, o) {
  const ms = S.data.macros || [];
  const search = ms.length >= 8;
  box.innerHTML = `${search ? `<input type="search" class="msearch" placeholder="${L('Поиск макроса…', 'Search macros…')}" value="${esc(listQuery[o.key] || '')}">` : ''}<div class="mlist"></div>`;
  const list = box.querySelector('.mlist');
  const draw = () => {
    const q = (listQuery[o.key] || '').trim().toLowerCase();
    const items = ms.map(m => ({ m, id: m.id, name: m.name, group: m.group || '', sub: o.sub(m) }));
    if (o.draft) items.push({ id: 'new', name: o.draft.name, group: o.draft.group || '', sub: L('не сохранён', 'not saved'), draft: true });
    const groups = groupNames(o.draft && o.draft.group);
    const closed = closedGroups();
    const curItem = items.find(it => it.id === o.cur);
    const item = it => `<div class="it ${it.id === o.cur ? 'on' : ''}" ${it.draft ? '' : `data-m="${esc(it.id)}"`} ${o.manage ? `draggable="true" data-drag="${esc(it.id)}"` : ''} data-in="${esc(it.group)}">${esc(it.name)}<small>${esc(it.sub)}</small></div>`;
    const hit = it => !q || it.name.toLowerCase().includes(q);
    let html = '';
    for (const g of groups) {
      const all = items.filter(it => sameGroup(it.group, g));
      const shown = q && g.toLowerCase().includes(q) ? all : all.filter(hit);
      if (q && !shown.length) continue;
      const open = q || !closed.has(g) || (curItem && sameGroup(curItem.group, g));
      html += `<div class="mgrp ${open ? 'open' : ''}" data-g="${esc(g)}" data-in="${esc(g)}"><span class="caret">${open ? '▾' : '▸'}</span><span class="gname">${esc(g)}</span><span class="cnt">${all.length}</span>${o.manage ? `<span class="gact">
        <button class="small" data-ga="new" title="${L('Новый макрос в этой папке', 'New macro in this folder')}">+</button><button class="small" data-ga="rename" title="${L('Переименовать', 'Rename')}">✎</button><button class="small" data-ga="up" title="${L('Выше', 'Move up')}">↑</button><button class="small" data-ga="down" title="${L('Ниже', 'Move down')}">↓</button><button class="small danger" data-ga="delete" title="${L('Удалить папку (макросы останутся)', 'Delete the folder (macros are kept)')}">✕</button></span>` : ''}</div>`;
      if (open) html += `<div class="gitems">${shown.map(item).join('') || `<div class="gempty muted small">${L('Пусто', 'Empty')}${o.manage ? L(' — перетащите сюда макрос', ' — drag a macro here') : ''}</div>`}</div>`;
    }
    const loose = items.filter(it => !it.group).filter(hit);
    if (groups.length && (loose.length || (o.manage && !q))) html += `<div class="mgrp open loose" data-in=""><span class="caret"></span><span class="gname">${L('Без папки', 'No folder')}</span><span class="cnt">${items.filter(it => !it.group).length}</span></div>`;
    html += loose.map(item).join('');
    if (q && !html) html = `<div class="muted small" style="padding:8px">${L('Ничего не найдено', 'Nothing found')}</div>`;
    list.innerHTML = html;

    list.querySelectorAll('[data-m]').forEach(el => el.onclick = () => o.onPick(el.dataset.m));
    list.querySelectorAll('.mgrp[data-g]').forEach(el => el.onclick = e => {
      if (e.target.closest('button') || q) return;
      setGroupClosed(el.dataset.g, el.classList.contains('open')); draw();
    });
    if (!o.manage) return;
    list.querySelectorAll('[data-ga]').forEach(el => el.onclick = () => groupAction(el.dataset.ga, el.closest('.mgrp').dataset.g));
    // drag a macro onto a folder header, the "no folder" header or a macro of the target folder
    list.querySelectorAll('[data-drag]').forEach(el => el.ondragstart = e => { e.dataTransfer.setData('text/x-macro', el.dataset.drag); e.dataTransfer.effectAllowed = 'move'; });
    list.querySelectorAll('[data-in]').forEach(el => {
      el.ondragover = e => { if (e.dataTransfer.types.includes('text/x-macro')) { e.preventDefault(); el.classList.add('drop'); } };
      el.ondragleave = () => el.classList.remove('drop');
      el.ondrop = e => { e.preventDefault(); el.classList.remove('drop'); moveMacro(e.dataTransfer.getData('text/x-macro'), el.dataset.in); };
    });
  };
  const inp = box.querySelector('.msearch');
  if (inp) inp.oninput = () => { listQuery[o.key] = inp.value; draw(); };
  draw();
}

async function moveMacro(id, group) {
  if (!id) return;
  if (id === 'new') { if (mDraft) { mDraft.group = group; renderView(); } return; }
  const m = macroById(id);
  if (!m || sameGroup(m.group, group)) return;
  if (!await run(() => api('macro/move', { id, group }))) return;
  if (group) setGroupClosed(group, false);
  if (mDraft && mEditId === id) mDraft.group = group; // the move is saved already; keep the draft in step
  await reload();
}

async function groupAction(op, g) {
  if (op === 'new') {
    if (mDirty && !await discardChanges('macro')) return;
    stopRec(); mEditId = 'new'; mDraft = { ...newMacro(), group: g }; mDirty = true; mSel = -1; setGroupClosed(g, false); renderView();
    return;
  }
  if (op === 'rename') {
    const to = await promptBox(L('Переименовать папку', 'Rename folder'), g, L('Переименовать', 'Rename'));
    if (!to || to === g) return;
    const r = await run(() => api('macro/group', { op, name: g, to }));
    if (!r) return;
    const wasClosed = closedGroups().has(g);
    setGroupClosed(g, false); setGroupClosed(r.name, wasClosed);
    if (mDraft && sameGroup(mDraft.group, g)) mDraft.group = r.name;
    await reload();
    return;
  }
  if (op === 'delete') {
    const n = (S.data.macros || []).filter(m => sameGroup(m.group, g)).length;
    if (n && !await confirmBox(L('Удалить папку', 'Delete folder'), L(`Удалить папку ${esc(Q(g))}? Макросы из неё (${n}) не удаляются — они окажутся «Без папки».`,
      `Delete the folder ${esc(Q(g))}? Its macros (${n}) are not deleted — they move to “No folder”.`), L('Удалить папку', 'Delete folder'), true)) return;
    if (!await run(() => api('macro/group', { op, name: g }))) return;
    setGroupClosed(g, false);
    if (mDraft && sameGroup(mDraft.group, g)) mDraft.group = '';
    await reload();
    return;
  }
  if (await run(() => api('macro/group', { op, name: g }))) await reload(); // up, down
}

async function newGroup() {
  const name = await promptBox(L('Новая папка', 'New folder'), '', L('Создать', 'Create'));
  if (!name) return null;
  const r = await run(() => api('macro/group', { op: 'add', name }));
  if (!r) return null;
  setGroupClosed(r.name, false);
  await reload();
  return r.name;
}

function macroAssign(body, b) {
  const ms = S.data.macros || [];
  if (!ms.length) {
    body.innerHTML = `<p class="muted">${L('Макросов пока нет.', 'No macros yet.')}</p><button class="primary" id="mgo">${L('Создать макрос', 'Create a macro')}</button>`;
    body.querySelector('#mgo').onclick = () => { tab = 'macros'; store.set('tab', tab); mEditId = 'new'; mDraft = newMacro(); mDirty = true; renderAll(); };
    return;
  }
  body.innerHTML = `<div class="mpick"></div>
    <p class="muted small">${L('Режим повтора задаётся в самом макросе (вкладка «Макросы»).', 'The repeat mode is set in the macro itself (Macros tab).')}</p>`;
  mountMacroList(body.querySelector('.mpick'), {
    key: 'pick', cur: b.kind === 'macro' ? b.macroId : '', sub: macroSub,
    onPick: id => { const m = macroById(id); setButton(selBtn, { kind: 'macro', code: [144, 0, 0], label: m.name, macroId: m.id }); },
  });
}

// ---------------------------------------------------------------------------
// DPI

function viewDPI(v) {
  const s = st(), si = s.sensor;
  const min = si.Step * 2;
  const live = s.primary && s.primary.link && editId === s.current ? s.primary.level : -1;
  let rows = '';
  for (let i = 0; i < draft.dpiCount; i++) {
    rows += `<div class="dpirow ${i === live ? 'cur' : ''}"><span class="lv">${i + 1}</span>
      ${swatch(`data-c="${i}"`, draft.dpiColors[i])}
      <input type="range" data-r="${i}" min="${min}" max="${si.MaxDPI}" step="${si.Step}" value="${draft.dpiLevels[i]}">
      <input type="number" data-n="${i}" min="${min}" max="${si.MaxDPI}" step="${si.Step}" value="${draft.dpiLevels[i]}">
      <label class="check"><input type="radio" name="start" data-s="${i}" ${draft.dpiStart === i ? 'checked' : ''}>${L('стартовый', 'start')}</label></div>`;
  }
  v.innerHTML = `<div class="card"><h2>${L('Уровни DPI', 'DPI levels')}</h2>
    <div class="row"><span class="muted">${L('Количество уровней', 'Number of levels')}</span><div class="seg">${[1, 2, 3, 4, 5, 6].map(n => `<button data-cnt="${n}" class="${n === draft.dpiCount ? 'on' : ''}">${n}</button>`).join('')}</div>
    <span class="grow"></span><button class="small" id="dpiDef">${L('Значения по умолчанию', 'Default values')}</button></div>
    <div class="sep"></div>${rows}
    <p class="muted small">${L(`Сенсор ${esc(si.Name)}: от ${min} до ${si.MaxDPI}, шаг ${si.Step}. Цвет — подсветка индикатора уровня на мыши.`, `Sensor ${esc(si.Name)}: ${min} to ${si.MaxDPI} in steps of ${si.Step}. The colour is the level indicator light on the mouse.`)}
    ${live >= 0 ? L(' Зелёным номером отмечен уровень, включённый сейчас.', ' The green number marks the level that is active now.') : ''}<br>
    ${L('«Стартовый» — уровень, который включается при записи профиля в мышь.', '“Start” is the level selected when the profile is written to the mouse.')}</p></div>`;
  v.querySelectorAll('[data-cnt]').forEach(el => el.onclick = () => {
    draft.dpiCount = +el.dataset.cnt;
    if (draft.dpiStart >= draft.dpiCount) draft.dpiStart = draft.dpiCount - 1;
    touch(); renderView();
  });
  const fix = x => Math.max(min, Math.min(si.MaxDPI, Math.round((+x || min) / si.Step) * si.Step));
  v.querySelectorAll('[data-r]').forEach(el => el.oninput = () => {
    const i = +el.dataset.r; draft.dpiLevels[i] = fix(el.value); v.querySelector(`[data-n="${i}"]`).value = draft.dpiLevels[i]; touch();
  });
  v.querySelectorAll('[data-n]').forEach(el => el.onchange = () => {
    const i = +el.dataset.n; draft.dpiLevels[i] = fix(el.value); el.value = draft.dpiLevels[i]; v.querySelector(`[data-r="${i}"]`).value = draft.dpiLevels[i]; touch();
  });
  v.querySelectorAll('[data-c]').forEach(el => el.onclick = () => {
    const i = +el.dataset.c;
    openPalette(el, draft.dpiColors[i], c => {
      draft.dpiColors[i] = c; el.querySelector('i').style.background = c; el.title = c; touch();
    });
  });
  v.querySelectorAll('[data-s]').forEach(el => el.onchange = () => { draft.dpiStart = +el.dataset.s; touch(); });
  v.querySelector('#dpiDef').onclick = () => {
    for (let i = 0; i < 8; i++) draft.dpiLevels[i] = si.Default[Math.min(i, si.Default.length - 1)];
    touch(); renderView();
  };
}

// ---------------------------------------------------------------------------
// Lighting

function viewLight(v) {
  v.innerHTML = `<div class="card"><h2>${L('Подсветка', 'Lighting')}</h2><div class="lgrid">${LIGHT_MODES.map((n, i) =>
    `<div class="lmode ${draft.lighting === i ? 'on' : ''}" data-l="${i}"><div class="sw" style="background:${LIGHT_SWATCH[i] || '#444'}"></div>${esc(nm(n))}</div>`).join('')}</div>
    <p class="muted small">${L('Прошивка SM809 поддерживает только выбор режима: яркость, скорость и цвет эффектов не настраиваются.',
      'The SM809 firmware only supports choosing a mode: brightness, speed and effect colours cannot be changed.')}</p></div>`;
  v.querySelectorAll('[data-l]').forEach(el => el.onclick = () => { draft.lighting = +el.dataset.l; touch(); renderView(); });
}

// ---------------------------------------------------------------------------
// Performance

function viewPerf(v) {
  const po = st().pollOptions.map(o => o.value);
  if (!po.includes(draft.pollRate)) po.push(draft.pollRate);
  const chk = (k, t, d) => `<label class="check"><input type="checkbox" data-k="${k}" ${draft[k] ? 'checked' : ''}><span>${t}<small>${d}</small></span></label>`;
  v.innerHTML = `<div class="grid2">
    <div class="card"><h2>${L('Частота опроса', 'Polling rate')}</h2><div class="seg">${po.map(o => `<button data-p="${o}" class="${o === draft.pollRate ? 'on' : ''}">${pollLabel(o)}</button>`).join('')}</div>
      <p class="muted small">${L('Список зависит от режима подключения (максимум сообщает сама мышь). Высокая частота быстрее разряжает батарею.',
        'The list depends on the connection (the mouse reports its maximum). Higher rates drain the battery faster.')}</p>
      <h3>${L('Высота отрыва (LOD)', 'Lift-off distance (LOD)')}</h3><div class="seg">${[1, 2].map(n => `<button data-lod="${n}" class="${draft.lod === n ? 'on' : ''}">${n} ${L('мм', 'mm')}</button>`).join('')}</div>
      <p class="muted small">${L('На какой высоте над ковриком сенсор перестаёт отслеживать движение. 1 мм — курсор не «плывёт», когда приподнимаете мышь; 2 мм — надёжнее на неровных и тканевых ковриках.',
        'The height above the pad at which the sensor stops tracking. 1 mm keeps the cursor still when you lift the mouse; 2 mm is more reliable on uneven or cloth pads.')}</p></div>
    <div class="card"><h2>${L('Энергосбережение', 'Power saving')}</h2>
      <label class="field"><span>${L('Уснуть через', 'Sleep after')} <b id="slv">${draft.sleepMin}</b> ${L('мин бездействия', 'min of inactivity')}</span><input type="range" min="1" max="20" id="sl" value="${draft.sleepMin}"></label>
      ${chk('moveWakeup', L('Пробуждение движением', 'Wake on movement'), L('мышь просыпается, когда её двигают, а не только по клику', 'the mouse wakes up when moved, not only on a click'))}
      ${chk('moveLightOff', L('Гасить подсветку при движении', 'Lighting off while moving'), L('подсветка выключается, пока мышь двигается', 'the lighting turns off while the mouse is moving'))}
      <h3>${L('Сенсор', 'Sensor')}</h3>
      ${chk('angleSnap', L('Выпрямление линий (angle snapping)', 'Angle snapping'), L('сглаживает дрожание при движении по прямой; в играх обычно выключают', 'straightens shaky lines; usually off for games'))}
      ${chk('motionSync', 'Motion Sync', L('синхронизирует данные сенсора с опросом — ровнее курсор, +~1 мс задержки', 'syncs sensor frames with polling — smoother cursor, about +1 ms latency'))}
      ${chk('ripple', 'Ripple control', L('подавляет «рябь» на высоких DPI ценой небольшой задержки', 'reduces jitter at high DPI at the cost of a little latency'))}</div></div>`;
  v.querySelectorAll('[data-p]').forEach(el => el.onclick = () => { draft.pollRate = +el.dataset.p; touch(); renderView(); });
  v.querySelectorAll('[data-lod]').forEach(el => el.onclick = () => { draft.lod = +el.dataset.lod; touch(); renderView(); });
  v.querySelector('#sl').oninput = e => { draft.sleepMin = +e.target.value; v.querySelector('#slv').textContent = draft.sleepMin; touch(); };
  v.querySelectorAll('[data-k]').forEach(el => el.onchange = () => { draft[el.dataset.k] = el.checked; touch(); });
}

// ---------------------------------------------------------------------------
// Macros

const MAX_BYTES = 375, MAX_DELAY = 4095;
function newMacro() { return { id: '', name: L('Новый макрос', 'New macro'), executeType: 2, repeatCount: 1, events: [] }; }

function evText(e) {
  if (e.type === 'delay') return L('Пауза', 'Delay');
  if (e.type === 'cursorMove') {
    const d = { left: L('Курсор влево', 'Cursor left'), right: L('Курсор вправо', 'Cursor right'), up: L('Курсор вверх', 'Cursor up'), down: L('Курсор вниз', 'Cursor down') }[e.direction];
    return d + L(' на ', ' by ') + e.value;
  }
  return keyName(e.code);
}
function evBadge(e) {
  return {
    keydown: `<span class="evtype down">${L('нажать', 'press')}</span>`, keyup: `<span class="evtype up">${L('отпустить', 'release')}</span>`,
    delay: `<span class="evtype delay">${L('пауза', 'delay')}</span>`, cursorMove: `<span class="evtype move">${L('курсор', 'cursor')}</span>`,
  }[e.type];
}
function macroWarnings(m) {
  const held = new Map();
  for (const e of m.events) {
    if (e.type === 'keydown') held.set(e.code, (held.get(e.code) || 0) + 1);
    if (e.type === 'keyup') held.set(e.code, Math.max(0, (held.get(e.code) || 0) - 1));
  }
  const w = [];
  for (const [k, n] of held) if (n > 0) w.push(L(`«${keyName(k)}» нажимается, но не отпускается — после макроса она останется зажатой.`, `“${keyName(k)}” is pressed but never released — it will stay held after the macro.`));
  return w;
}

let rec = null; // recorder state

function stopRec() {
  if (!rec) return;
  window.removeEventListener('keydown', rec.kd, true);
  window.removeEventListener('keyup', rec.ku, true);
  rec = null;
}

function viewMacros(v) {
  const ms = S.data.macros || [];
  if (!mDraft && mEditId && mEditId !== 'new') mDraft = clone(macroById(mEditId));
  v.innerHTML = `<div class="mlayout"><div><div id="mlistbox"></div><div class="row" style="margin-top:10px"><button class="primary" id="mnew">+ ${L('Новый макрос', 'New macro')}</button><button id="mgnew">+ ${L('Папка', 'Folder')}</button></div>
    ${(S.data.macroGroups || []).length ? `<p class="muted small">${L('Макрос можно перетащить в другую папку. Папки нужны только для порядка в списке — в мышь они не записываются.', 'Drag a macro to move it to another folder. Folders only organise this list — they are not written to the mouse.')}</p>` : ''}
    <p class="muted small">${L(`Макрос записывается в мышь вместе с профилем, в котором он назначен на кнопку. Память мыши — ${MAX_BYTES / 3} действий на все макросы профиля.`,
      `A macro is written to the mouse together with the profile that assigns it to a button. Mouse memory: ${MAX_BYTES / 3} actions for all macros of a profile.`)}</p></div>
    <div id="med"></div></div>`;
  mountMacroList(v.querySelector('#mlistbox'), {
    key: 'tab', cur: mEditId, manage: true, draft: mEditId === 'new' ? mDraft : null, sub: m => `${m.events.length} ${L('действий', 'actions')}`,
    onPick: async id => {
      if (id === mEditId) return;
      if (mDirty && !await discardChanges('macro')) return;
      stopRec(); mEditId = id; mDraft = clone(macroById(mEditId)); mDirty = false; mSel = -1; renderView();
    },
  });
  v.querySelector('#mgnew').onclick = () => newGroup();
  v.querySelector('#mnew').onclick = async () => {
    if (mDirty && !await discardChanges('macro')) return;
    stopRec(); mEditId = 'new'; mDraft = newMacro(); mDirty = true; mSel = -1; renderView();
  };
  const ed = v.querySelector('#med');
  if (!mDraft) { ed.innerHTML = `<div class="card muted">${L('Выберите макрос слева или создайте новый.', 'Pick a macro on the left or create a new one.')}</div>`; return; }
  renderMacroEditor(ed);
}

function renderMacroEditor(ed) {
  const m = mDraft;
  const bytes = m.events.length * 3;
  const warns = macroWarnings(m);
  const ms = L('мс', 'ms');
  let rows = '';
  m.events.forEach((e, i) => {
    const dmax = e.type === 'cursorMove' ? 255 : MAX_DELAY;
    rows += `<tr class="${i === mSel ? 'sel' : ''}" data-row="${i}"><td class="muted">${i + 1}</td><td>${evBadge(e)}</td><td>${esc(evText(e))}</td>
      <td><input type="number" min="0" max="${dmax}" value="${e.delay}" data-d="${i}"> ${ms}</td>
      <td style="white-space:nowrap"><button class="small" data-up="${i}" ${i ? '' : 'disabled'}>↑</button> <button class="small" data-dn="${i}" ${i < m.events.length - 1 ? '' : 'disabled'}>↓</button> <button class="small danger" data-del="${i}">✕</button></td></tr>`;
  });
  const exLabel = { 1: L('Повторять, пока кнопка зажата', 'Repeat while the button is held'), 2: L('Выполнить N раз', 'Run N times'), 3: L('Повторять до повторного нажатия', 'Repeat until the button is pressed again') };
  ed.innerHTML = `<div class="card">
    <div class="row"><label class="field" style="flex:1"><span>${L('Название', 'Name')}</span><input type="text" id="mname" value="${esc(m.name)}"></label>
      <label class="field"><span>${L('Папка', 'Folder')}</span><select id="mgroup"><option value="">${L('— без папки —', '— no folder —')}</option>${groupNames(m.group).map(g => `<option value="${esc(g)}" ${sameGroup(g, m.group) ? 'selected' : ''}>${esc(g)}</option>`).join('')}<option value="" id="mgnewopt">${L('Новая папка…', 'New folder…')}</option></select></label></div>
    <h3>${L('Как выполнять', 'Run mode')}</h3>
    <div class="row">${[1, 3].map(t => `<label class="check"><input type="radio" name="ex" data-ex="${t}" ${m.executeType === t ? 'checked' : ''}>${exLabel[t]}</label>`).join('')}
      <span class="nowrap"><label class="check"><input type="radio" name="ex" data-ex="2" ${m.executeType === 2 ? 'checked' : ''}>${exLabel[2]}</label>
      <label class="check" ${m.executeType === 2 ? '' : 'style="opacity:.4"'}>N = <input type="number" id="mrep" min="1" max="255" value="${m.repeatCount || 1}" ${m.executeType === 2 ? '' : 'disabled'}></label></span></div>
    <h3>${L('Действия', 'Actions')}</h3>
    <div class="evtable"><table><thead><tr><th>#</th><th>${L('Тип', 'Type')}</th><th>${L('Действие', 'Action')}</th><th>${L('Пауза перед', 'Delay before')}</th><th></th></tr></thead><tbody>${rows || `<tr><td colspan="5" class="muted" style="padding:16px">${L('Пусто. Запишите действия или добавьте их кнопками ниже.', 'Empty. Record actions or add them with the buttons below.')}</td></tr>`}</tbody></table></div>
    <div class="meter ${bytes > MAX_BYTES ? 'full' : ''}"><i style="width:${Math.min(100, bytes / MAX_BYTES * 100)}%"></i></div>
    <div class="row small muted" style="margin-top:4px"><span>${m.events.length} ${L('из', 'of')} ${MAX_BYTES / 3} ${L('действий', 'actions')}</span><span class="grow"></span><span>${mSel >= 0 ? L('новые действия вставляются после строки ', 'new actions are inserted after row ') + (mSel + 1) : L('новые действия добавляются в конец', 'new actions are added at the end')}</span></div>
    ${warns.map(w => `<p class="small" style="color:var(--warn)">⚠ ${esc(w)}</p>`).join('')}
    ${m.events.some(e => e.type === 'cursorMove') ? `<p class="small" style="color:var(--warn)">⚠ ${L('Сдвиги курсора в этом макросе мышь с заводской прошивкой не выполнит — их можно удалить.', 'The mouse with stock firmware will not perform the cursor moves in this macro — you can delete them.')}</p>` : ''}

    <h3>${L('Запись', 'Recording')}</h3>
    <div class="recpad ${rec ? 'rec' : ''}" id="pad">${rec ? L('● Идёт запись: нажимайте клавиши, кликайте мышью в этой области. Колесо и Win записать нельзя.', '● Recording: press keys and click mouse buttons inside this area. The wheel and Win key cannot be recorded.')
      : L('Нажмите «Записать», затем нажимайте клавиши и кнопки мыши в этой области', 'Press “Record”, then press keys and mouse buttons inside this area')}</div>
    <div class="row" style="margin-top:10px"><button class="${rec ? 'danger' : 'primary'}" id="mrec">${rec ? '■ ' + L('Остановить', 'Stop') : '● ' + L('Записать', 'Record')}</button>
      <label class="check"><input type="checkbox" id="rdel" ${store.get('recDelays', true) ? 'checked' : ''}>${L('записывать реальные паузы', 'record real delays')}</label>
      <label class="check">${L('иначе', 'otherwise')} <input type="number" id="rfix" min="0" max="${MAX_DELAY}" value="${store.get('recFixed', 30)}"> ${ms}</label></div>

    <h3>${L('Добавить вручную', 'Add manually')}</h3>
    <div class="row"><select id="akey">${keyOptions(true)}</select><button class="small" data-add="press">${L('Нажать и отпустить', 'Press and release')}</button><button class="small" data-add="keydown">${L('Зажать', 'Press')}</button><button class="small" data-add="keyup">${L('Отпустить', 'Release')}</button><span style="width:20px"></span><button class="small" id="atext">${L('Ввести текст…', 'Type text…')}</button></div>
    <div class="row"><input type="number" id="adel" min="1" max="${MAX_DELAY}" value="100"> ${ms} <button class="small" data-add="delay">${L('Пауза', 'Delay')}</button>
      <span style="width:20px"></span><span class="disabledgrp" title="${L('Заводская прошивка SM809 не выполняет сдвиг курсора из макроса (проверено: не работает и в официальном драйвере).', 'The stock SM809 firmware does not perform cursor moves from macros (confirmed: it does not work in the official driver either).')}"><select id="adir" disabled><option>${L('влево', 'left')}</option></select>
      <input type="number" id="aval" value="10" disabled><button class="small" data-add="move" disabled>${L('Сдвиг курсора', 'Move cursor')}</button>
      <span class="small muted">${L('не работает на заводской прошивке', 'does not work on the stock firmware')}</span></span></div>

    <div class="sep"></div>
    <div class="row"><button class="primary" id="msave" ${mDirty ? '' : 'disabled'}>${L('Сохранить макрос', 'Save macro')}</button><button id="mrev" ${mDirty && mEditId !== 'new' ? '' : 'disabled'}>${L('Отменить', 'Revert')}</button>
      <span class="grow"></span><button class="small" id="mclear" ${m.events.length ? '' : 'disabled'}>${L('Очистить', 'Clear')}</button><button class="small danger" id="mdel" ${mEditId && mEditId !== 'new' ? '' : 'disabled'}>${L('Удалить макрос', 'Delete macro')}</button></div>
    <p class="muted small">${L(`Пауза — время перед действием (до ${MAX_DELAY} мс; у сдвига курсора — до 255 мс). Кодирование совпадает с официальным драйвером.`,
      `Delay is the time before the action (up to ${MAX_DELAY} ms; up to 255 ms for cursor moves). The encoding matches the official driver.`)}</p></div>`;

  const mark = () => { mDirty = true; };
  ed.querySelector('#mname').oninput = e => { m.name = e.target.value; mark(); ed.querySelector('#msave').disabled = false; };
  ed.querySelector('#mgroup').onchange = async e => {
    let g = e.target.value;
    if (e.target.selectedOptions[0].id === 'mgnewopt') {
      g = await promptBox(L('Новая папка', 'New folder'), '', L('Создать', 'Create'));
      if (!g) { e.target.value = m.group || ''; return; }
    }
    if (mEditId !== 'new' && !mDirty) { await moveMacro(mEditId, g); return; } // a saved macro moves at once
    m.group = g; mark(); setGroupClosed(g, false); renderView();
  };
  ed.querySelectorAll('[data-ex]').forEach(el => el.onchange = () => { m.executeType = +el.dataset.ex; mark(); renderMacroEditor(ed); });
  const rep = ed.querySelector('#mrep');
  rep.onchange = () => { m.repeatCount = Math.max(1, Math.min(255, +rep.value | 0)); rep.value = m.repeatCount; mark(); ed.querySelector('#msave').disabled = false; };
  ed.querySelectorAll('[data-row]').forEach(el => el.onclick = e => {
    if (e.target.closest('input,button')) return;
    const i = +el.dataset.row; mSel = mSel === i ? -1 : i; renderMacroEditor(ed);
  });
  ed.querySelectorAll('[data-d]').forEach(el => el.onchange = () => {
    const e = m.events[+el.dataset.d], max = e.type === 'cursorMove' ? 255 : MAX_DELAY;
    e.delay = Math.max(0, Math.min(max, +el.value | 0)); el.value = e.delay; mark(); ed.querySelector('#msave').disabled = false;
  });
  const move = (i, d) => { const ev = m.events; [ev[i], ev[i + d]] = [ev[i + d], ev[i]]; mSel = i + d; mark(); renderMacroEditor(ed); };
  ed.querySelectorAll('[data-up]').forEach(el => el.onclick = () => move(+el.dataset.up, -1));
  ed.querySelectorAll('[data-dn]').forEach(el => el.onclick = () => move(+el.dataset.dn, 1));
  ed.querySelectorAll('[data-del]').forEach(el => el.onclick = () => { m.events.splice(+el.dataset.del, 1); mSel = -1; mark(); renderMacroEditor(ed); });

  const insert = evs => {
    if ((m.events.length + evs.length) * 3 > MAX_BYTES) { toast(L('Не хватает памяти макроса', 'Not enough macro memory'), true); return; }
    const at = mSel >= 0 ? mSel + 1 : m.events.length;
    m.events.splice(at, 0, ...evs);
    if (mSel >= 0) mSel += evs.length;
    mark(); renderMacroEditor(ed);
  };
  ed.querySelectorAll('[data-add]').forEach(el => el.onclick = () => {
    const k = +ed.querySelector('#akey').value;
    switch (el.dataset.add) {
      case 'press': insert([{ type: 'keydown', code: k, delay: 30 }, { type: 'keyup', code: k, delay: 30 }]); break;
      case 'keydown': insert([{ type: 'keydown', code: k, delay: 30 }]); break;
      case 'keyup': insert([{ type: 'keyup', code: k, delay: 30 }]); break;
      case 'delay': insert([{ type: 'delay', code: 0, delay: Math.max(1, Math.min(MAX_DELAY, +ed.querySelector('#adel').value | 0)) }]); break;
      case 'move': break; // disabled: the stock firmware drops macro cursor moves
    }
  });

  ed.querySelector('#atext').onclick = () => {
    const free = MAX_BYTES / 3 - m.events.length;
    modal(`<h2>${L('Ввести текст', 'Type text')}</h2>
      <textarea id="ttext" rows="5" style="width:100%;resize:vertical" placeholder="${L('Текст, который макрос напечатает', 'Text for the macro to type')}"></textarea>
      <div class="small" id="tinfo" style="margin-top:8px"></div>
      <p class="muted small">${L('Мышь нажимает физические клавиши, а какой символ появится, решает раскладка Windows в момент выполнения: латиница печатается правильно при английской раскладке, кириллица — при русской (ЙЦУКЕН). Смешивать латиницу и кириллицу в одном тексте нельзя. Пауза между нажатиями — 10 мс, Enter — перевод строки.',
        'The mouse presses physical keys; which character appears depends on the Windows keyboard layout at that moment: Latin text comes out right with an English layout, Cyrillic with the Russian (ЙЦУКЕН) one. Latin and Cyrillic cannot be mixed in one text. 10 ms between key presses; Enter is a line break.')}</p>
      <div class="foot"><button data-r="0">${L('Отмена', 'Cancel')}</button><button class="primary" data-r="1" id="tok" disabled>${L('Вставить', 'Insert')}</button></div>`, box => {
      const ta = box.querySelector('#ttext'), info = box.querySelector('#tinfo'), ok = box.querySelector('#tok');
      let res = null;
      const upd = () => {
        res = textToEvents(ta.value);
        const n = res.events.length;
        const msgs = [`${L('Действий', 'Actions')}: <b>${n}</b> ${L('из', 'of')} ${free} ${L('свободных', 'free')}`];
        if (n) msgs.push(res.layout === 'ru' ? L('нужна <b>русская</b> раскладка', 'needs the <b>Russian</b> layout') : L('нужна <b>английская</b> раскладка', 'needs the <b>English</b> layout'));
        let err = '';
        if (res.mixed) err = L('Текст содержит и латиницу, и кириллицу — так набрать нельзя.', 'The text mixes Latin and Cyrillic — it cannot be typed.');
        else if (n > free) err = L('Не помещается в память макроса — сократите текст.', 'Does not fit into the macro memory — shorten the text.');
        if (res.unsupported.length) msgs.push(`<span style="color:var(--warn)">${L('пропущены символы', 'skipped characters')}: ${esc(res.unsupported.join(' '))}</span>`);
        info.innerHTML = msgs.join(' · ') + (err ? `<div style="color:var(--err);margin-top:4px">${err}</div>` : '');
        ok.disabled = !n || !!err;
      };
      ta.oninput = upd; upd(); ta.focus();
      box.querySelector('[data-r="0"]').onclick = closeModal;
      ok.onclick = () => { closeModal(); if (res && res.events.length) { if (m.events.length) res.events[0].delay = 10; insert(res.events); } };
    });
  };
  ed.querySelector('#rdel').onchange = e => store.set('recDelays', e.target.checked);
  ed.querySelector('#rfix').onchange = e => store.set('recFixed', Math.max(0, Math.min(MAX_DELAY, +e.target.value | 0)));
  const recAdd = (type, code) => {
    if ((m.events.length + 1) * 3 > MAX_BYTES) { stopRec(); toast(L('Память макроса заполнена — запись остановлена', 'Macro memory is full — recording stopped'), true); renderMacroEditor(ed); return; }
    const now = performance.now();
    let d = 0;
    if (rec.last) d = store.get('recDelays', true) ? Math.round(now - rec.last) : store.get('recFixed', 30);
    rec.last = now;
    m.events.push({ type, code, delay: Math.min(MAX_DELAY, d) });
    mark(); mSel = -1;
    renderMacroEditor(ed);
    const body = ed.querySelector('.evtable'); body.scrollTop = body.scrollHeight;
  };
  ed.querySelector('#mrec').onclick = () => {
    if (rec) { stopRec(); renderMacroEditor(ed); return; }
    rec = { last: 0 };
    rec.kd = e => { e.preventDefault(); e.stopPropagation(); if (e.repeat) return; const c = HID[e.code]; if (c) recAdd('keydown', c); };
    rec.ku = e => { e.preventDefault(); e.stopPropagation(); const c = HID[e.code]; if (c) recAdd('keyup', c); };
    window.addEventListener('keydown', rec.kd, true);
    window.addEventListener('keyup', rec.ku, true);
    renderMacroEditor(ed);
  };
  const pad = ed.querySelector('#pad');
  const mb = { 0: 240, 2: 241, 1: 242, 3: 243, 4: 244 };
  pad.oncontextmenu = e => e.preventDefault();
  pad.onauxclick = e => e.preventDefault();
  pad.onmousedown = e => { if (!rec) return; e.preventDefault(); if (mb[e.button]) recAdd('keydown', mb[e.button]); };
  pad.onmouseup = e => { if (!rec) return; e.preventDefault(); if (mb[e.button]) recAdd('keyup', mb[e.button]); };

  ed.querySelector('#msave').onclick = async () => {
    stopRec();
    if (!m.events.length) { toast(L('Макрос пустой', 'The macro is empty'), true); return; }
    const r = await run(() => api('macro/save', { ...m, events: m.events.map((e, i) => ({ ...e, index: i })) }));
    if (!r) return;
    mEditId = r.id; mDirty = false; mDraft = null;
    if (m.group) setGroupClosed(m.group, false);
    toast(L('Макрос сохранён', 'Macro saved') + (usedInCurrent(r.id) ? L('. Он назначен в текущем профиле — запишите профиль в мышь.', '. It is assigned in the current profile — write the profile to the mouse.') : ''));
    await reload();
  };
  ed.querySelector('#mrev').onclick = () => { stopRec(); mDraft = clone(macroById(mEditId)); mDirty = false; mSel = -1; renderView(); };
  ed.querySelector('#mclear').onclick = () => { m.events = []; mSel = -1; mark(); renderMacroEditor(ed); };
  ed.querySelector('#mdel').onclick = async () => {
    if (!await confirmBox(L('Удалить макрос', 'Delete macro'), L('Удалить ', 'Delete ') + esc(Q(m.name)) + '?', L('Удалить', 'Delete'), true)) return;
    if (await run(() => api('macro/delete', { id: mEditId }), L('Макрос удалён', 'Macro deleted'))) { mEditId = null; mDraft = null; mDirty = false; await reload(); }
  };
}
function usedInCurrent(id) {
  const p = profileById(st().current);
  return p && p.buttons.some((b, i) => b.kind === 'macro' && b.macroId === id && !isSoftBtn(i)); // software buttons need no write
}

// ---------------------------------------------------------------------------
// Profiles & games

function viewProfiles(v) {
  const s = st(), set = S.data.settings;
  const running = new Set((s.stack || []).map(x => x.exe));
  let pl = '';
  for (const p of profiles()) {
    const badges = [];
    if (p.id === set.defaultProfile) badges.push('★ ' + L('основной', 'main'));
    if (p.id === s.base) badges.push(L('выбран вручную', 'selected manually'));
    if (p.id === s.current) badges.push(`<span style="color:var(--accent)">● ${L('в мыши', 'on the mouse')}</span>`);
    const nRules = (S.data.rules || []).filter(r => r.profileId === p.id).length;
    if (nRules) badges.push(L('правил: ', 'rules: ') + nRules);
    pl += `<tr><td><b>${esc(p.name)}</b><div class="small muted">${badges.join(' · ')}</div></td>
      <td style="text-align:right;white-space:nowrap">
      <button class="small" data-act="use" data-id="${p.id}" ${p.id === s.base ? 'disabled' : ''}>${L('Выбрать', 'Select')}</button>
      <button class="small" data-act="edit" data-id="${p.id}">${L('Настроить', 'Edit')}</button>
      <button class="small" data-act="ren" data-id="${p.id}">${L('Переименовать', 'Rename')}</button>
      <button class="small" data-act="dup" data-id="${p.id}">${L('Копия', 'Duplicate')}</button>
      <button class="small" data-act="def" data-id="${p.id}" ${p.id === set.defaultProfile ? 'disabled' : ''}>${L('Основной', 'Make main')}</button>
      <button class="small danger" data-act="del" data-id="${p.id}" ${p.id === set.defaultProfile ? 'disabled' : ''}>✕</button></td></tr>`;
  }
  const popt = id => profiles().map(p => `<option value="${p.id}" ${p.id === id ? 'selected' : ''}>${esc(p.name)}</option>`).join('');
  let rl = '';
  rulesDraft.forEach((r, i) => {
    rl += `<tr class="${r.enabled ? '' : 'off'}"><td><input type="checkbox" data-en="${i}" ${r.enabled ? 'checked' : ''} title="${L('Правило включено', 'Rule enabled')}"></td>
      <td><input type="text" data-exe="${i}" value="${esc(r.exe)}" placeholder="game.exe" style="width:100%"></td>
      <td><select data-pr="${i}">${popt(r.profileId)}</select></td>
      <td><label class="check"><input type="checkbox" data-osd="${i}" ${r.osd ? 'checked' : ''}>${L('показывать', 'show')}</label></td>
      <td>${running.has(r.exe) ? `<span style="color:var(--accent)">● ${L('запущена', 'running')}</span>` : ''}</td>
      <td><button class="small danger" data-rdel="${i}">✕</button></td></tr>`;
  });
  v.innerHTML = `<div class="card"><div class="row"><h2 style="margin:0">${L('Профили', 'Profiles')}</h2><span class="grow"></span><button class="primary" id="pnew">+ ${L('Новый профиль', 'New profile')}</button></div>
    <table style="margin-top:10px">${pl}</table>
    <p class="muted small">${L('Профили хранятся на компьютере, их число не ограничено. В мышь записывается только один — текущий, и только то, что в нём отличается. «Выбрать» — профиль, который действует, когда не запущена ни одна игра из списка ниже.',
      'Profiles are stored on the computer, with no limit on their number. Only the current one is written to the mouse, and only what differs. “Select” sets the profile used when none of the games below is running.')}</p></div>

    ${!set.autoSwitch && !isSoftMode() ? `<div class="card locked"><h2>🔒 ${L('Игры и программы', 'Games and programs')}</h2>
      <p>${L('Переключение профилей по запуску игр выключено. Оно перезаписывает память мыши при каждом запуске и закрытии игры, поэтому включается отдельно — после предупреждения в настройках.',
        'Switching profiles by game launch is turned off. It rewrites the mouse memory every time a game starts and exits, so it has to be enabled separately, after a warning in Settings.')}</p>
      ${rulesDraft.length ? `<p class="muted small">${L(`Сохранённых правил: ${rulesDraft.length}. Они не действуют, пока переключение выключено.`, `Saved rules: ${rulesDraft.length}. They are inactive while switching is off.`)}</p>` : ''}
      <button class="primary" id="goAuto">${L('Открыть настройки', 'Open Settings')}</button></div>` : `
    <div class="card"><div class="row"><h2 style="margin:0">${L('Игры и программы', 'Games and programs')}</h2><span class="grow"></span>
      <button id="radd">+ ${L('Добавить из запущенных', 'Add from running')}</button><button id="raddm">+ ${L('Вручную', 'Manually')}</button></div>
    ${isSoftMode() ? `<p class="small">${L('<b>Программные кнопки</b> берутся из профиля программы, окно которой сейчас активно, — сразу и без записи в мышь. Если для активного окна правила нет — из профиля в мыши.',
      '<b>Software buttons</b> come from the profile of the program whose window is in front — instantly and without writing the mouse. If there is no rule for that window — from the profile on the mouse.')}</p>` : ''}
    <p class="muted small">${!set.autoSwitch ? L('Настройки мыши (DPI, подсветка, аппаратные кнопки) по запуску игр не переключаются — это включается в настройках после предупреждения об износе.',
      'Mouse settings (DPI, lighting, hardware buttons) are not switched by game launch — that is enabled in Settings after a wear warning.') + ' ' : L('Когда запускается exe из списка, мышь переключается на его профиль; когда он закрывается — возвращается к предыдущему. Пока программа работает, мышь не перезаписывается. Если запущено несколько — действует запущенная последней.',
      'When an exe from the list starts, the mouse switches to its profile; when it exits, the previous profile returns. While the program runs, the mouse is not rewritten. If several are running, the last started one wins.')}
    ${L('«Всплывающее окно» — показывать окно со сменой профиля для этой программы', '“Popup” shows the profile-change popup for this program')}${S.data.settings.osdProfile ? '' : L(' (сейчас окна выключены в настройках)', ' (popups are currently turned off in Settings)')}.</p>
    <table><thead><tr><th style="width:30px"></th><th>${L('Файл программы', 'Program file')}</th><th>${L('Профиль', 'Profile')}</th><th>${L('Всплывающее окно', 'Popup')}</th><th></th><th></th></tr></thead><tbody>
    ${rl || `<tr><td colspan="6" class="muted" style="padding:14px">${L('Правил пока нет.', 'No rules yet.')}</td></tr>`}</tbody></table>
    <div class="row" style="margin-top:12px"><span class="grow"></span>${rulesDirty ? `<span style="color:var(--warn)">● ${L('не сохранено', 'not saved')}</span>` : ''}
      <button id="rrev" ${rulesDirty ? '' : 'disabled'}>${L('Отменить', 'Revert')}</button><button class="primary" id="rsave" ${rulesDirty ? '' : 'disabled'}>${L('Сохранить правила', 'Save rules')}</button></div>
    ${!st().synced ? `<p class="small" style="color:var(--warn)">${L('Правила начнут работать после первой полной записи профиля в мышь (кнопка в жёлтой полосе вверху).', 'Rules start working after the first full write of a profile to the mouse (button in the yellow bar at the top).')}</p>` : ''}</div>`}`;

  v.querySelectorAll('[data-act]').forEach(el => el.onclick = () => profileAction(el.dataset.act, el.dataset.id));
  const ga = v.querySelector('#goAuto');
  if (ga) ga.onclick = () => {
    tab = 'settings'; store.set('tab', tab); renderAll();
    const c = $('#autoCard'); c.scrollIntoView({ block: 'center' }); c.classList.add('flash'); setTimeout(() => c.classList.remove('flash'), 1600);
  };
  v.querySelector('#pnew').onclick = async () => {
    const name = await promptBox(L('Новый профиль', 'New profile'), L('Профиль ', 'Profile ') + (profiles().length + 1), L('Создать', 'Create'));
    if (!name) return;
    if (await run(() => api('profile/create', { name, copyFrom: '' }), L('Профиль создан (настройки по умолчанию)', 'Profile created (default settings)'))) await reload();
  };
  if (!set.autoSwitch && !isSoftMode()) return;
  const rd = () => { rulesDirty = true; };
  v.querySelectorAll('[data-en]').forEach(el => el.onchange = () => { rulesDraft[+el.dataset.en].enabled = el.checked; rd(); renderView(); });
  v.querySelectorAll('[data-exe]').forEach(el => el.onchange = () => { rulesDraft[+el.dataset.exe].exe = el.value.trim().toLowerCase(); rd(); renderView(); });
  v.querySelectorAll('[data-pr]').forEach(el => el.onchange = () => { rulesDraft[+el.dataset.pr].profileId = el.value; rd(); renderView(); });
  v.querySelectorAll('[data-osd]').forEach(el => el.onchange = () => { rulesDraft[+el.dataset.osd].osd = el.checked; rd(); renderView(); });
  v.querySelectorAll('[data-rdel]').forEach(el => el.onclick = () => { rulesDraft.splice(+el.dataset.rdel, 1); rd(); renderView(); });
  const addRule = exe => {
    exe = exe.trim().toLowerCase();
    if (!exe) return;
    if (!exe.endsWith('.exe')) exe += '.exe';
    if (rulesDraft.some(r => r.exe === exe)) { toast(exe + L(' уже в списке', ' is already in the list'), true); return; }
    rulesDraft.push({ exe, profileId: editId || S.data.settings.baseProfile, osd: true, enabled: true });
    rd(); renderView();
  };
  v.querySelector('#raddm').onclick = async () => { const n = await promptBox(L('Имя файла программы', 'Program file name'), 'game.exe', L('Добавить', 'Add')); if (n) addRule(n); };
  v.querySelector('#radd').onclick = async () => {
    const list = await run(() => api('processes'));
    if (!list) return;
    modal(`<h2>${L('Запущенные программы', 'Running programs')}</h2><input type="text" id="pf" placeholder="${L('Поиск…', 'Search…')}" style="width:100%"><div class="plist" id="pl"></div><div class="foot"><button id="pc">${L('Закрыть', 'Close')}</button></div>`, box => {
      const draw = f => { box.querySelector('#pl').innerHTML = list.filter(n => n.includes(f)).map(n => `<div data-n="${esc(n)}">${esc(n)}</div>`).join('') || `<span class="muted">${L('ничего не найдено', 'nothing found')}</span>`; };
      draw('');
      const pf = box.querySelector('#pf'); pf.focus();
      pf.oninput = () => draw(pf.value.toLowerCase());
      box.querySelector('#pl').onclick = e => { const d = e.target.closest('[data-n]'); if (d) { closeModal(); addRule(d.dataset.n); } };
      box.querySelector('#pc').onclick = closeModal;
    });
  };
  v.querySelector('#rrev').onclick = () => { rulesDraft = clone(S.data.rules || []); rulesDirty = false; renderView(); };
  v.querySelector('#rsave').onclick = async () => {
    if (await run(() => api('rules', rulesDraft), L('Правила сохранены', 'Rules saved'))) { rulesDirty = false; await reload(); }
  };
}

async function profileAction(act, id) {
  const p = profileById(id);
  switch (act) {
    case 'use':
      await run(() => api('profile/apply', { id, makeBase: true }), L('Выбран профиль ', 'Selected profile ') + Q(p.name));
      break;
    case 'edit':
      if (dirty && editId !== id && !await discardChanges()) return;
      if (editId !== id) { editId = id; draft = clone(p); dirty = false; }
      tab = 'buttons'; store.set('tab', tab); renderAll(); return;
    case 'ren': {
      const name = await promptBox(L('Переименовать профиль', 'Rename profile'), p.name, L('Сохранить', 'Save'));
      if (!name) return;
      const c = clone(p); c.name = name;
      await run(() => api('profile/save', c));
      if (editId === id && draft) draft.name = name;
      break;
    }
    case 'dup': {
      const name = await promptBox(L('Копия профиля', 'Duplicate profile'), p.name + L(' (копия)', ' (copy)'), L('Создать', 'Create'));
      if (!name) return;
      await run(() => api('profile/create', { name, copyFrom: id }), L('Копия создана', 'Copy created'));
      break;
    }
    case 'def':
      await run(() => api('profile/default', { id }), L('Основной профиль изменён', 'Main profile changed'));
      break;
    case 'del':
      if (!await confirmBox(L('Удалить профиль', 'Delete profile'), L(`Удалить ${esc(Q(p.name))} и связанные с ним правила?`, `Delete ${esc(Q(p.name))} and its rules?`), L('Удалить', 'Delete'), true)) return;
      await run(() => api('profile/delete', { id }), L('Профиль удалён', 'Profile deleted'));
      break;
  }
  await reload();
}

// ---------------------------------------------------------------------------
// Battery: forecast + history chart

function fmtHours(h) {
  if (h >= 48) { const d = Math.floor(h / 24); return L(`${d} д ${Math.floor(h - d * 24)} ч`, `${d}d ${Math.floor(h - d * 24)}h`); }
  if (h >= 1) return L(`${Math.round(h)} ч`, `${Math.round(h)}h`);
  return L(`${Math.round(h * 60)} мин`, `${Math.round(h * 60)} min`);
}
function fmtDate(t, withTime = true) {
  const d = new Date(t * 1000);
  return d.toLocaleString(LANG === 'ru' ? 'ru-RU' : 'en-GB', withTime ? { day: 'numeric', month: 'short', hour: '2-digit', minute: '2-digit' } : { day: 'numeric', month: 'short' });
}

let batRange = store.get('batRange', 7), batData = null, batSig = '';

async function viewBattery(v) {
  v.innerHTML = `<div class="card" id="batStats"></div>
    <div class="card"><div class="row"><h2 style="margin:0">${L('Заряд', 'Charge')}</h2><span class="grow"></span>
      <div class="seg">${[[1, L('Сутки', 'Day')], [7, L('Неделя', 'Week')], [30, L('Месяц', 'Month')]].map(([d, n]) => `<button data-rg="${d}" class="${d === batRange ? 'on' : ''}">${n}</button>`).join('')}</div></div>
      <div id="batChart" class="batchart"></div>
      <p class="muted small">${L('Точка сохраняется при каждом изменении процента и раз в 30 минут, пока мышь на связи. Разрывы линии — мышь была выключена или не на связи. Процент заряда мышь считает по напряжению, поэтому он меняется неравномерно: обычно медленно вверху и быстрее ниже 20%.',
      'A point is stored on every percentage change and every 30 minutes while the mouse is connected. Gaps mean the mouse was off or disconnected. The mouse derives the percentage from voltage, so it falls unevenly: usually slowly at the top and faster below 20%.')}</p></div>`;
  v.querySelectorAll('[data-rg]').forEach(el => el.onclick = () => { batRange = +el.dataset.rg; store.set('batRange', batRange); renderView(); });
  renderBatStats();
  batData = await run(() => api('battery')) || [];
  if (tab === 'battery') drawBatChart();
}

function batteryLive() {
  renderBatStats();
  const f = st().battery, p = st().primary;
  const sig = p ? `${p.battery}/${p.charge}/${p.link}` : '';
  if (sig !== batSig) {
    batSig = sig;
    api('battery').then(d => { batData = d; if (tab === 'battery') drawBatChart(); }).catch(() => {});
  }
}

function renderBatStats() {
  const box = $('#batStats');
  if (!box) return;
  const s = st(), f = s.battery || {}, p = s.primary;
  const tile = (label, value, sub) => `<div class="tile"><div class="tl">${label}</div><div class="tv">${value}</div>${sub ? `<div class="ts">${sub}</div>` : ''}</div>`;
  let left;
  if (!p || !p.link) left = tile(L('Осталось', 'Time left'), '—', L('мышь не на связи', 'mouse not connected'));
  else if (f.state === 'full') left = tile(L('Осталось', 'Time left'), L('Заряжена', 'Charged'), L('отключите провод, чтобы начать отсчёт', 'unplug to start the countdown'));
  else if (f.state === 'charging') left = tile(L('До полной зарядки', 'Until full'), f.hours > 0 ? '≈ ' + fmtHours(f.hours) : '…', f.hours > 0 ? '' : L('мало данных, нужно хотя бы +3%', 'not enough data yet, needs +3%'));
  else if (f.hours > 0) left = tile(L('Хватит примерно на', 'Roughly left'), fmtHours(f.hours),
    f.basis === 'current' ? L('по расходу с момента отключения зарядки', 'based on use since the last charge') : L('по средним прошлым разрядам — уточнится после −3%', 'based on past discharges; refines after −3%'));
  else left = tile(L('Осталось', 'Time left'), '…', L('собираю данные: нужно хотя бы −3% за час', 'collecting data: needs at least −3% over an hour'));
  const rate = f.state === 'discharging' && f.ratePerDay > 0 ? `${f.ratePerDay.toFixed(f.ratePerDay < 10 ? 1 : 0)}% ${L('в сутки', 'per day')}` : '—';
  box.innerHTML = `<div class="tiles">
    ${tile(L('Заряд', 'Charge'), p && p.link ? p.battery + '%' : '—', p && p.link ? ({ 1: L('заряжается', 'charging'), 2: L('заряжена', 'charged') }[p.charge] || L('от батареи', 'on battery')) : '')}
    ${left}
    ${tile(L('Расход', 'Drain'), rate, '')}
    ${tile(L('Последняя полная зарядка', 'Last full charge'), f.lastFull ? fmtDate(f.lastFull) : '—', '')}</div>`;
}

function drawBatChart() {
  const box = $('#batChart');
  if (!box) return;
  const now = Date.now() / 1000, from = now - batRange * 86400;
  const pts = (batData || []).filter(x => x.t >= from);
  const W = Math.max(320, box.clientWidth), H = 260, ml = 40, mr = 12, mt = 10, mb = 26;
  const pw = W - ml - mr, ph = H - mt - mb;
  const X = t => ml + (t - from) / (now - from) * pw, Y = p => mt + (100 - p) / 100 * ph;
  if (!pts.length) { box.innerHTML = `<div class="muted" style="padding:40px;text-align:center">${L('Данных пока нет — они появятся, пока программа работает и мышь на связи.', 'No data yet — it is collected while the app runs and the mouse is connected.')}</div>`; return; }
  const GAP = 95 * 60; // > 3 missed heartbeats → break the line
  let grid = '';
  for (const p of [0, 25, 50, 75, 100]) grid += `<line x1="${ml}" x2="${W - mr}" y1="${Y(p)}" y2="${Y(p)}" class="gl"/><text x="${ml - 6}" y="${Y(p) + 4}" class="ax" text-anchor="end">${p}%</text>`;
  const ticks = batRange === 1 ? 6 : batRange === 7 ? 7 : 6;
  for (let i = 0; i <= ticks; i++) {
    const t = from + (now - from) * i / ticks;
    const lbl = batRange === 1 ? new Date(t * 1000).toLocaleTimeString(LANG === 'ru' ? 'ru-RU' : 'en-GB', { hour: '2-digit', minute: '2-digit' }) : fmtDate(t, false);
    grid += `<text x="${X(t)}" y="${H - 6}" class="ax" text-anchor="${i === 0 ? 'start' : i === ticks ? 'end' : 'middle'}">${lbl}</text>`;
  }
  // charging bands
  let bands = '';
  for (let i = 0; i < pts.length; i++) {
    if (!pts[i].c) continue;
    let j = i; while (j + 1 < pts.length && pts[j + 1].c && pts[j + 1].t - pts[j].t < GAP) j++;
    const x0 = X(pts[i].t), x1 = j + 1 < pts.length && pts[j + 1].t - pts[j].t < GAP ? X(pts[j + 1].t) : X(Math.min(now, pts[j].t + 1800));
    bands += `<rect x="${x0}" y="${mt}" width="${Math.max(2, x1 - x0)}" height="${ph}" class="band"/>`;
    i = j;
  }
  // line (step-after: the percentage holds until the next report)
  let d = '', prev = null;
  for (const p of pts) {
    if (!prev || p.t - prev.t > GAP) d += `M${X(p.t).toFixed(1)},${Y(p.p).toFixed(1)}`;
    else d += `H${X(p.t).toFixed(1)}V${Y(p.p).toFixed(1)}`;
    prev = p;
  }
  const lastP = pts[pts.length - 1];
  if (now - lastP.t < GAP && st().primary && st().primary.link) d += `H${X(now).toFixed(1)}`;
  box.innerHTML = `<svg width="${W}" height="${H}" role="img" aria-label="${L('График заряда', 'Battery chart')}">
    ${grid}${bands}<path d="${d}" class="ln"/><circle cx="${X(lastP.t)}" cy="${Y(lastP.p)}" r="4" class="dotl"/>
    <line class="xh" y1="${mt}" y2="${mt + ph}" style="display:none"/><circle class="xp" r="5" style="display:none"/></svg>
    <div class="ctip" style="display:none"></div>
    <div class="row small muted" style="margin-top:6px"><span class="lg"><i class="lgl"></i>${L('заряд, %', 'charge, %')}</span><span class="lg"><i class="lgb"></i>${L('на зарядке', 'charging')}</span></div>`;
  const svg = box.querySelector('svg'), xh = svg.querySelector('.xh'), xp = svg.querySelector('.xp'), tip = box.querySelector('.ctip');
  svg.onmousemove = e => {
    const r = svg.getBoundingClientRect(), mx = e.clientX - r.left;
    const t = from + (mx - ml) / pw * (now - from);
    let best = null;
    for (const p of pts) if (p.t <= t) best = p; else break;
    if (!best || mx < ml || mx > W - mr || t - best.t > GAP) { xh.style.display = xp.style.display = tip.style.display = 'none'; return; }
    xh.setAttribute('x1', mx); xh.setAttribute('x2', mx); xh.style.display = '';
    xp.setAttribute('cx', mx); xp.setAttribute('cy', Y(best.p)); xp.style.display = '';
    tip.innerHTML = `<b>${best.p}%</b>${best.c ? ' ⚡' : ''}<br><span class="muted">${fmtDate(t)}</span>`;
    tip.style.display = '';
    tip.style.left = Math.min(W - 150, mx + 12) + 'px';
    tip.style.top = Math.max(0, Y(best.p) - 50) + 'px';
  };
  svg.onmouseleave = () => { xh.style.display = xp.style.display = tip.style.display = 'none'; };
}
window.addEventListener('resize', () => { if (tab === 'battery') drawBatChart(); });

// ---------------------------------------------------------------------------
// Settings

function viewSettings(v) {
  const s = setDraft, ps = s.popup;
  function get(path) { return path.split('.').reduce((o, k) => o[k], s); }
  const chk = (path, t, d) => `<label class="check"><input type="checkbox" data-b="${path}" ${get(path) ? 'checked' : ''}><span>${t}${d ? `<small>${d}</small>` : ''}</span></label>`;
  const num = (path, t, min, max) => `<label class="field"><span>${t}</span><input type="number" data-n="${path}" min="${min}" max="${max}" value="${get(path)}"></label>`;
  const col = (path, t) => `<div class="field"><span>${t}</span>${swatch(`data-c="${path}"`, get(path))}</div>`;
  const POS = {
    'bottom-center': L('снизу по центру', 'bottom centre'), 'top-center': L('сверху по центру', 'top centre'), center: L('по центру', 'centre'),
    'top-left': L('слева сверху', 'top left'), 'top-right': L('справа сверху', 'top right'), 'bottom-left': L('слева снизу', 'bottom left'),
    'bottom-right': L('справа снизу', 'bottom right'), cursor: L('у курсора', 'at the cursor'),
  };
  v.innerHTML = `<div class="grid2">
  <div>
  <div class="card"><h2>${L('Язык', 'Language')}</h2>
    <select id="lang"><option value="ru" ${LANG === 'ru' ? 'selected' : ''}>Русский</option><option value="en" ${LANG === 'en' ? 'selected' : ''}>English</option></select>
    <p class="muted small">${L('Язык окна, меню в трее, уведомлений и всплывающих окон.', 'Language of this window, the tray menu, notifications and popups.')}</p></div>
  <div class="card"><h2>${L('О программе', 'About')}</h2>
    <p>SM809 Control ${esc(S.version)} · ${L('открытый исходный код, лицензия MIT', 'open source, MIT licence')}</p>
    <div class="row"><button data-url="releases">${L('Скачать новую версию', 'Download the latest version')}</button><button data-url="repo">${L('Проект на GitHub', 'Project on GitHub')}</button><button class="small" data-url="issues">${L('Сообщить о проблеме', 'Report a problem')}</button></div></div>
  <div class="card"><h2>${L('Кнопки', 'Buttons')}</h2>
    <div class="row"><span>${L('Режим', 'Mode')}: <b>${modeTitle(S.data.settings.buttonMode)}</b></span><span class="grow"></span><button id="modeBtn">${L('Изменить режим…', 'Change mode…')}</button></div>
    ${chk('keyRepeat', L('Автоповтор клавиши, пока программная кнопка зажата', 'Repeat the key while a software button is held'), L('для одной клавиши без модификаторов', 'single keys without modifiers'))}
    ${chk('restoreOnExit', L('При выходе из программы возвращать аппаратную раскладку', 'Restore the hardware layout when the app exits'), L('программные кнопки будут работать и без программы (как в аппаратном режиме), но каждый выход и запуск — запись в мышь', 'software buttons then work without the app (as in hardware mode), but every exit and start writes the mouse'))}</div>
  <div class="card" id="autoCard"><h2>${L('Переключение профилей по играм', 'Switching profiles by games')}</h2>
    <label class="check"><input type="checkbox" id="autosw" ${S.data.settings.autoSwitch ? 'checked' : ''}><span>${L('Разрешить', 'Allow')}<small>${L('профиль включается при запуске exe и выключается при его закрытии (вкладка «Профили и игры»)', 'a profile is applied when an exe starts and reverted when it exits (Profiles & games tab)')}</small></span></label>
    <p class="muted small">${L('Выключено по умолчанию: каждое переключение перезаписывает флеш-память мыши, у которой ограниченный ресурс.', 'Off by default: every switch rewrites the mouse flash memory, which has limited endurance.')}</p></div>
  <div class="card"><h2>${L('Всплывающие окна', 'Popups')}</h2>
    ${chk('osdProfile', L('При смене профиля', 'On profile change'), L('для отдельных программ можно выключить во вкладке «Профили и игры»', 'can be turned off for individual programs on the Profiles & games tab'))}
    ${chk('osdDpi', L('При смене DPI', 'On DPI change'))}
    ${chk('popup.useDpiColors', L('Цвет окна DPI = цвет уровня', 'DPI popup uses the level colour'))}
    <h3>${L('Внешний вид', 'Appearance')}</h3>
    <div class="row"><label class="field"><span>${L('Положение', 'Position')}</span><select data-s="popup.position">${Object.entries(POS).map(([k, n]) => `<option value="${k}" ${ps.position === k ? 'selected' : ''}>${n}</option>`).join('')}</select></label>
      ${num('popup.offsetX', L('Отступ X', 'Offset X'), -2000, 2000)}${num('popup.offsetY', L('Отступ Y', 'Offset Y'), -2000, 2000)}</div>
    <div class="row">${num('popup.width', L('Ширина', 'Width'), 60, 2000)}${num('popup.height', L('Высота', 'Height'), 30, 2000)}${num('popup.cornerRadius', L('Скругление', 'Corner radius'), 0, 500)}</div>
    <div class="row">${col('popup.background', L('Фон', 'Background'))}<label class="field"><span>${L('Непрозрачность', 'Opacity')}</span><input type="range" data-n="popup.opacity" min="0" max="255" value="${ps.opacity}" style="width:120px"></label>
      ${col('popup.textColor', L('Текст', 'Text'))}${col('popup.captionColor', L('Подпись', 'Caption'))}${col('popup.accentColor', L('Акцент', 'Accent'))}${col('popup.dotColor', L('Точки', 'Dots'))}</div>
    <div class="row"><label class="field"><span>${L('Шрифт', 'Font')}</span><input type="text" data-s="popup.font" value="${esc(ps.font)}" style="width:150px"></label>
      ${num('popup.fontSize', L('Размер', 'Size'), 6, 300)}${num('popup.captionSize', L('Подпись', 'Caption'), 6, 200)}${chk('popup.bold', L('Жирный', 'Bold'))}</div>
    <div class="row">${num('popup.durationMs', L('Показывать, мс', 'Show for, ms'), 100, 60000)}${num('popup.fadeMs', L('Затухание, мс', 'Fade, ms'), 0, 10000)}</div>
    <div class="row" style="margin-top:12px"><button id="tDpi">${L('Показать окно DPI', 'Preview DPI popup')}</button><button id="tProf">${L('Показать окно профиля', 'Preview profile popup')}</button><button class="small" id="pDef">${L('Стиль по умолчанию', 'Default style')}</button></div>
  </div></div>
  <div>
  <div class="card"><h2>${L('Батарея', 'Battery')}</h2>
    <div class="row">${num('lowPercent', L('Предупреждать при, %', 'Warn at, %'), 0, 100)}${num('criticalPercent', L('Срочно при, %', 'Critical at, %'), 0, 100)}</div>
    ${chk('notifyFull', L('Сообщать, когда мышь зарядилась', 'Notify when fully charged'))}
    ${chk('notifyLink', L('Сообщать о потере и восстановлении связи', 'Notify when the connection is lost or restored'))}
    <p class="muted small">${L('0 — не предупреждать.', '0 = no warning.')}</p></div>
  <div class="card"><h2>${L('Программа', 'Application')}</h2>
    <label class="check"><input type="checkbox" id="auto" ${autostartDraft ? 'checked' : ''} ${!S.elevated && S.autostartMode === 'task' ? 'disabled' : ''}><span>${L('Запускать вместе с Windows', 'Start with Windows')}<small>${L('в трее, без окна', 'in the tray, without the window')}</small></span></label>
    ${S.elevated ? `<p class="muted small">🛡 ${L('Программа запущена с правами администратора, поэтому автозапуск создаётся командой schtasks как задача «SM809Control» в Планировщике заданий (с наивысшими правами): обычный автозапуск Windows такие программы пропускает.',
      'The app runs as administrator, so autostart is created with schtasks as the task "SM809Control" in Task Scheduler (highest privileges): the normal Windows autostart skips such programs.')}</p>`
    : S.autostartMode === 'task' ? `<p class="muted small">🛡 ${L('Автозапуск настроен через Планировщик заданий с правами администратора. Изменить его можно, только запустив программу от имени администратора.',
      'Autostart is set up as a Task Scheduler task with administrator rights. To change it, run the app as administrator.')}</p>` : ''}
    ${num('scanSeconds', L('Проверять запущенные программы раз в, с', 'Check running programs every, s'), 1, 30)}
    ${chk('debug', L('Вести журнал', 'Write a log'), L('файл sm809control.log в %APPDATA%\\SM809Control', 'file sm809control.log in %APPDATA%\\SM809Control'))}
</div>
  <div class="card"><h2>${L('Данные', 'Data')}</h2>
    <div class="row"><button id="exp">${L('Экспорт профилей и макросов…', 'Export profiles and macros…')}</button><button id="imp">${L('Импорт…', 'Import…')}</button><input type="file" id="impf" accept=".json" hidden></div>
    <p class="muted small">${L('Экспорт сохраняет JSON-файл. При импорте профили и макросы добавляются как новые, ничего не перезаписывается.', 'Export saves a JSON file. Import adds profiles and macros as new ones and overwrites nothing.')}</p>
    <div class="sep"></div>
    <div class="row"><button id="bkp">${L('Полная резервная копия…', 'Full backup…')}</button><button id="rst">${L('Восстановить из копии…', 'Restore from backup…')}</button><input type="file" id="rstf" accept=".json" hidden></div>
    <p class="muted small">${L('Всё сразу — профили, макросы, правила игр, настройки, счётчик записей во флеш, что уже записано в мышь, история заряда. Для переноса программы на другой компьютер. Восстановление заменяет текущие данные; их копия сохраняется в %APPDATA%\\SM809Control.',
      'Everything at once — profiles, macros, game rules, settings, flash write counter, what is already on the mouse, battery history. Use it to move the app to another PC. Restoring replaces the current data; a copy of it is kept in %APPDATA%\\SM809Control.')}</p></div>
  <div class="card"><h2>${L('Мышь', 'Mouse')}</h2>
    <div class="row"><button id="force">${L('Записать текущий профиль целиком', 'Write the current profile in full')}</button><button class="danger" id="reset">${L('Сброс к заводским…', 'Factory reset…')}</button></div>
    <div class="row" style="margin-top:10px"><button class="primary" id="mread">${L('Прочитать настройки из мыши', 'Read settings from the mouse')}</button><button class="small" id="fdump">${L('Сохранить дамп памяти', 'Save memory dump')}</button><button class="small" id="diag">${L('Диагностика подключения…', 'Connection diagnostics…')}</button></div>
    <p class="muted small">${L('Только чтение, ничего не записывает. Мышь должна быть подключена кабелем. Показывает, что реально хранится в мыши, и позволяет сделать из этого профиль.', 'Read-only, writes nothing. The mouse must be connected by cable. Shows what is actually stored in the mouse and lets you make a profile from it.')}</p>
    <p class="muted small">${L('Обычно программа записывает только изменения. Полная запись нужна, если настройки в мыши поменяли другой программой.', 'Normally only changes are written. A full write is needed if the mouse settings were changed by another program.')}
    ${L('Каждая запись перезаписывает ячейку флеш-памяти мыши; всего записей через эту программу', 'Each write rewrites a flash sector of the mouse; total writes from this app')}: <b>${st().flashWrites}</b> ${L('(ресурс флеша — десятки тысяч циклов).', '(flash endurance is tens of thousands of cycles).')}</p></div>
  <div class="card"><h2>🧪 ${L('Экспериментальные опции', 'Experimental options')}</h2>
    <p class="muted small">${L('Скрытые возможности прошивки, которых нет в фирменных программах. Найдены при разборе прошивки и на практике проверены не до конца — меняйте осознанно.', 'Hidden firmware features not offered by the vendor software. Found by reading the firmware and not fully field-tested — change them deliberately.')}</p>
    <h3>${L('Антидребезг кнопок', 'Button debounce')}</h3>
    ${chk('debounceSet', L('Задавать значение', 'Set the value'), L('скрытая настройка прошивки (команда 9), её нет в фирменных программах', 'a hidden firmware setting (command 9) not offered by the vendor software'))}
    <div class="row">${num('debounce', L('Значение (заводское — 2)', 'Value (factory — 2)'), 0, 20)}<button class="small" id="dbFactory">${L('Заводское', 'Factory')}</button></div>
    <p class="muted small">${L('Сколько тактов опроса кнопка должна оставаться в новом положении, прежде чем мышь примет нажатие. Больше — меньше ложных двойных кликов у изношенных кнопок, но клик срабатывает с небольшой задержкой. Меньше — быстрее, но возможен «дребезг». Увеличивайте понемногу (3, 4, 5…), если кнопка начала даблкликать. Отключение галочки ничего не возвращает — чтобы вернуть как было, поставьте 2. Записывается в мышь при сохранении настроек и не меняется при смене профилей.',
      'How many button scan ticks the new state must hold before the mouse accepts it. Higher — fewer false double clicks on worn switches, but a slightly delayed click. Lower — faster, but bounces may get through. Increase step by step (3, 4, 5…) if a button starts double-clicking. Unticking does not restore anything — set 2 to go back. Written to the mouse when settings are saved; not changed by profile switches.')}</p>
</div>
  </div></div>`;

  const mark = () => { if (!setDirty) { setDirty = true; renderEditbar(); } };
  const put = (path, val) => { const ks = path.split('.'); const last = ks.pop(); ks.reduce((o, k) => o[k], s)[last] = val; mark(); };
  v.querySelectorAll('[data-url]').forEach(el => el.onclick = () => run(() => api('open-url', { which: el.dataset.url })));
  v.querySelector('#lang').onchange = async e => {
    const language = e.target.value;
    // saved immediately, independently of other unsaved settings
    if (await run(() => api('settings', { settings: { ...S.data.settings, language } }))) {
      setDraft.language = language;
      await reload();
    }
  };
  v.querySelectorAll('[data-b]').forEach(el => el.onchange = () => put(el.dataset.b, el.checked));
  v.querySelectorAll('[data-n]').forEach(el => el.onchange = el.oninput = () => { if (el.value !== '') put(el.dataset.n, +el.value | 0); });
  v.querySelectorAll('[data-c]').forEach(el => el.onclick = () => openPalette(el, get(el.dataset.c), c => {
    put(el.dataset.c, c); el.querySelector('i').style.background = c; el.title = c;
  }));
  v.querySelectorAll('[data-s]').forEach(el => el.onchange = () => put(el.dataset.s, el.value));
  v.querySelector('#auto').onchange = e => { autostartDraft = e.target.checked; mark(); };
  v.querySelector('#tDpi').onclick = () => run(() => api('osd/test', { kind: 'dpi', style: s.popup }));
  v.querySelector('#tProf').onclick = () => run(() => api('osd/test', { kind: 'profile', style: s.popup }));
  v.querySelector('#pDef').onclick = () => {
    s.popup = { position: 'bottom-center', offsetX: 0, offsetY: 120, width: 240, height: 120, cornerRadius: 18, background: '#1e1f22', opacity: 225, textColor: '#ffffff', captionColor: '#9aa0a6', accentColor: '#4caf50', dotColor: '#4a4d52', font: 'Segoe UI', fontSize: 40, captionSize: 13, bold: true, durationMs: 1200, fadeMs: 350, useDpiColors: true };
    setDirty = true; renderEditbar(); renderView();
  };
  v.querySelector('#exp').onclick = async () => {
    const d = await run(() => api('export'));
    if (!d) return;
    const a = document.createElement('a');
    a.href = URL.createObjectURL(new Blob([JSON.stringify({ profiles: d.profiles, macros: d.macros, macroGroups: d.macroGroups || [] }, null, 2)], { type: 'application/json' }));
    a.download = 'sm809-profiles.json';
    a.click();
    setTimeout(() => URL.revokeObjectURL(a.href), 5000);
  };
  const f = v.querySelector('#impf');
  v.querySelector('#imp').onclick = () => f.click();
  f.onchange = async () => {
    const file = f.files[0]; if (!file) return;
    let d;
    try { d = JSON.parse(await file.text()); } catch { toast(L('Это не JSON-файл', 'This is not a JSON file'), true); return; }
    const r = await run(() => api('import', { profiles: d.profiles || [], macros: d.macros || [], macroGroups: d.macroGroups || [] }));
    if (r) { toast(L(`Импортировано профилей: ${r.profiles}, макросов: ${r.macros}`, `Imported profiles: ${r.profiles}, macros: ${r.macros}`)); await reload(); }
    f.value = '';
  };
  v.querySelector('#modeBtn').onclick = () => modeDialog(false);
  v.querySelector('#autosw').onchange = async e => {
    const on = e.target.checked;
    e.target.checked = !on;
    if (on && !await autoSwitchWarning()) return;
    if (await run(() => api('settings', { settings: { ...S.data.settings, autoSwitch: on } }),
      on ? L('Переключение по играм включено', 'Switching by games enabled') : L('Переключение по играм выключено', 'Switching by games disabled'))) {
      setDraft.autoSwitch = on;
      await reload();
    }
  };
  v.querySelector('#bkp').onclick = async () => {
    const d = await run(() => api('backup'));
    if (!d) return;
    const a = document.createElement('a');
    a.href = URL.createObjectURL(new Blob([JSON.stringify(d, null, 1)], { type: 'application/json' }));
    a.download = 'sm809control-backup-' + new Date().toISOString().slice(0, 10) + '.json';
    a.click();
    setTimeout(() => URL.revokeObjectURL(a.href), 5000);
  };
  const rf = v.querySelector('#rstf');
  v.querySelector('#rst').onclick = () => rf.click();
  rf.onchange = async () => {
    const file = rf.files[0]; rf.value = '';
    if (!file) return;
    let d;
    try { d = JSON.parse(await file.text()); } catch { toast(L('Это не JSON-файл', 'This is not a JSON file'), true); return; }
    if (d.format !== 'sm809control-backup') { toast(L('Это не полная резервная копия SM809 Control', 'This is not an SM809 Control full backup'), true); return; }
    const when = d.created ? new Date(d.created).toLocaleString(LANG === 'ru' ? 'ru-RU' : 'en-GB') : '?';
    if (!await confirmBox(L('Восстановить из копии', 'Restore from backup'),
      L(`Копия от ${esc(when)}: профилей ${(d.data.profiles || []).length}, макросов ${(d.data.macros || []).length}, записей во флеш ${d.device ? d.device.flashWrites : 0}.<br><br>Все текущие данные программы будут заменены. Их копия сохранится в %APPDATA%\\SM809Control.`,
        `Backup from ${esc(when)}: ${(d.data.profiles || []).length} profiles, ${(d.data.macros || []).length} macros, ${d.device ? d.device.flashWrites : 0} flash writes.<br><br>All current app data will be replaced. A copy of it is kept in %APPDATA%\\SM809Control.`),
      L('Восстановить', 'Restore'), true)) return;
    if (dirty || mDirty || rulesDirty || setDirty) { dirty = mDirty = rulesDirty = setDirty = false; mDraft = null; }
    const r = await run(() => api('restore', d));
    if (r) { toast(L('Данные восстановлены', 'Data restored')); editId = null; await reload(); }
  };
  v.querySelector('#mread').onclick = e => readFromMouse(e.target);
  v.querySelector('#dbFactory').onclick = () => { const i = v.querySelector('[data-n="debounce"]'); i.value = 2; put('debounce', 2); };
  v.querySelector('#diag').onclick = connectionDiag;
  v.querySelector('#fdump').onclick = async e => {
    e.target.disabled = true;
    const r = await run(() => api('flash-dump', {}), L('Память прочитана', 'Memory read'));
    e.target.disabled = false;
    if (r) toast(r.dir);
  };
  v.querySelector('#force').onclick = () => run(() => api('profile/apply', { id: st().current, force: true }), L('Записываю профиль целиком…', 'Writing the profile in full…'));
  v.querySelector('#reset').onclick = async () => {
    if (!await confirmBox(L('Сброс к заводским', 'Factory reset'), L('Мышь вернётся к заводским настройкам (кнопки, DPI, подсветка, макросы). Профили на компьютере останутся — потом можно записать любой из них.',
      'The mouse returns to factory settings (buttons, DPI, lighting, macros). Profiles on the computer are kept — you can write any of them afterwards.'), L('Сбросить', 'Reset'), true)) return;
    run(() => api('factory-reset', {}), L('Сбрасываю мышь…', 'Resetting the mouse…'));
  };
}

// ---------------------------------------------------------------------------

window.addEventListener('beforeunload', e => { if (dirty || mDirty || rulesDirty || setDirty) { e.preventDefault(); e.returnValue = ''; } });
setInterval(() => { if (S) showLastMessage(); }, 700);

(async () => {
  try {
    await reload();
    lastMsgShown = st().lastMessage;
    startEvents();
    if (!S.data.settings.buttonMode) modeDialog(true); // first start of 2.0: choose the button mode
  } catch (e) {
    document.body.innerHTML = `<div style="padding:40px"><h2>${L('Не удалось подключиться к SM809 Control', 'Cannot connect to SM809 Control')}</h2><p class="muted">${esc(e.message)}</p><p class="muted">${L('Откройте окно заново из значка в трее.', 'Reopen the window from the tray icon.')}</p></div>`;
  }
})();

// ---------------------------------------------------------------------------
// Connection diagnostics: every HID collection of the mouse and what its
// status reader got (to find out why a connection is not seen).
async function connectionDiag() {
  const rows = await run(() => api('diag/hid'));
  if (!rows) return;
  const mode = r => ({ wired: L('кабель', 'cable'), dongle: L('донгл', 'dongle'), bluetooth: 'Bluetooth' }[r.mode] || ('PID ' + r.PID.toString(16)));
  const hx = n => n.toString(16).toUpperCase().padStart(4, '0');
  const ago = t => !t || t.startsWith('0001') ? '—' : Math.round((Date.now() - new Date(t)) / 1000) + L(' с назад', ' s ago');
  const body = rows.map(r => `<tr><td>${esc(mode(r))}</td><td>${hx(r.UsagePage)}:${hx(r.Usage)}</td><td>${r.InLen}/${r.FeatLen}</td>
    <td>${r.status ? L('статус', 'status') : ''}${r.command ? (r.status ? ', ' : '') + L('команды', 'commands') : ''}</td>
    <td>${r.status ? `${r.reader.opened ? '●' : '○'} ${r.reader.reports} (${L('опрос', 'polled')} ${r.reader.polled}, ${L('ошибок', 'fails')} ${r.reader.pollFails}, ${L('отчёт 5', 'report 5')} ${r.reader.feat5 || 0}/${r.reader.feat5Bad || 0}) · ${ago(r.reader.last)}` : ''}</td></tr>`
    + (r.status && (r.reader.lastInput || r.reader.lastFeat5) ? `<tr><td></td><td colspan="4" class="muted" style="font-family:Consolas,monospace;font-size:11.5px">${r.reader.lastInput ? '4: ' + esc(r.reader.lastInput) : ''}${r.reader.lastFeat5 ? ' &nbsp; 5: ' + esc(r.reader.lastFeat5) : ''}</td></tr>` : '')).join('');
  const text = JSON.stringify({ version: S.version, state: { devices: st().devices, primary: st().primary }, hid: rows }, null, 1);
  modal(`<h2>${L('Диагностика подключения', 'Connection diagnostics')}</h2>
    <p class="muted small">${L('Все каналы (HID-коллекции) мыши, которые видит Windows. «статус» — канал, из которого программа читает заряд и связь; число — сколько отчётов пришло.', 'Every HID collection of the mouse Windows reports. “status” is the one the app reads battery and link from; the number is how many reports arrived.')}</p>
    <table class="small"><thead><tr><th>${L('Подключение', 'Connection')}</th><th>Usage</th><th>in/feat</th><th>${L('Роль', 'Role')}</th><th>${L('Отчёты', 'Reports')}</th></tr></thead><tbody>${body || `<tr><td colspan="5" class="muted">${L('Мышь не найдена ни по одному подключению', 'The mouse was not found on any connection')}</td></tr>`}</tbody></table>
    <div class="foot"><button id="dcopy">${L('Скопировать для разработчика', 'Copy for the developer')}</button><button class="primary" id="dclose">${L('Закрыть', 'Close')}</button></div>`, box => {
    $('#modalBox').style.maxWidth = '820px';
    box.querySelector('#dclose').onclick = closeModal;
    box.querySelector('#dcopy').onclick = async () => {
      try { await navigator.clipboard.writeText(text); toast(L('Скопировано', 'Copied')); }
      catch { const t = document.createElement('textarea'); t.value = text; document.body.appendChild(t); t.select(); document.execCommand('copy'); t.remove(); toast(L('Скопировано', 'Copied')); }
    };
  });
}

// ---------------------------------------------------------------------------
// Button modes (2.0). Hardware mode: everything is in the mouse. Software
// mode: buttons 1..11 are hardware buttons (their action is in the mouse) or
// software buttons (SM809 Control performs the action of the profile of the
// program in front). Buttons 12..16 are always hardware buttons.

const SOFT_N = 16;
let softKeys = null, softLayouts = [], liveSig = '';
const SOFT_MOUSE = () => [['left', L('Левая кнопка', 'Left click')], ['right', L('Правая кнопка', 'Right click')],
  ['middle', L('Средняя кнопка', 'Middle click')], ['back', L('Назад', 'Back')], ['forward', L('Вперёд', 'Forward')]];
const softMouseName = b => (SOFT_MOUSE().find(x => x[0] === b) || [b, b])[1];
const isSoftMode = () => S.data.settings.buttonMode === 'software';
const isSoftBtn = i => isSoftMode() && i !== LOCKED_BUTTON && i < SOFT_N && !S.data.settings.hwButtons[i];
const softStat = () => st().soft || { carrier: [], carriable: [], held: [] };
const modeTitle = m => ({ hardware: L('аппаратные', 'hardware'), software: L('программные', 'software') }[m] || L('не выбран', 'not chosen'));
function softTouch() { touch(); }

// readable names of system keys in combinations
const KEY_NICE = () => ({
  MyComputer: L('Мой компьютер', 'This PC'), Mail: L('Почта', 'Mail'), Calculator: L('Калькулятор', 'Calculator'), MediaPlayer: L('Проигрыватель', 'Media player'),
  VolumeUp: L('Громкость +', 'Volume +'), VolumeDown: L('Громкость −', 'Volume −'), Mute: L('Без звука', 'Mute'), PlayPause: L('Пуск / пауза', 'Play / pause'),
  NextTrack: L('Следующий трек', 'Next track'), PrevTrack: L('Предыдущий трек', 'Previous track'), StopMedia: L('Стоп', 'Stop'),
  BrowserBack: L('Браузер: назад', 'Browser back'), BrowserForward: L('Браузер: вперёд', 'Browser forward'), BrowserRefresh: L('Браузер: обновить', 'Browser refresh'), BrowserHome: L('Браузер: домой', 'Browser home'),
  Space: L('Пробел', 'Space'),
});
const niceKeys = k => (k || '').split('+').map(x => KEY_NICE()[x] || x).join('+');

function softLabel(b) {
  switch (b.kind) {
    case 'keys': return niceKeys(b.keys) || L('не задано', 'not set');
    case 'mouse': return softMouseName(b.button);
    case 'disable': return L('ничего', 'nothing');
    case 'macro': { const m = macroById(b.macroId); return L('Макрос: ', 'Macro: ') + (m ? m.name : L('(удалён)', '(deleted)')); }
    case 'text': return L('Текст: ', 'Text: ') + (b.secret ? '••••••' : (b.text || '').replace(/\s+/g, ' ').slice(0, 24) + ((b.text || '').length > 24 ? '…' : ''));
    case 'run': return L('Запуск: ', 'Start: ') + (b.path || '').split(/[\\/]/).pop();
  }
  return L('не задано', 'not set');
}
// What button i of the draft does (label).
const btnLabel = i => isSoftBtn(i) ? softLabel(draft.soft[i] || {}) : assignLabel(draft.buttons[i]);

const MODE_INFO = () => ({
  hardware: {
    title: L('Аппаратные кнопки', 'Hardware buttons'),
    text: L('Все назначения хранятся в мыши, как у обычной игровой мыши.', 'All assignments are stored in the mouse, like on any gaming mouse.'),
    pros: [L('кнопки работают и без программы — на любом компьютере', 'buttons work without the app — on any computer'),
      L('огонь, DPI, макросы прошивки — на любой кнопке', 'rapid fire, DPI, firmware macros — on any button')],
    cons: [L('каждое изменение кнопок — запись во флеш-память мыши; переключение профилей по играм её изнашивает', 'every change of the buttons is a write to the mouse flash; switching profiles by games wears it out'),
      L('макросы ограничены прошивкой: длина, паузы до 4 с, без движения курсора', 'macros are limited by the firmware: length, delays up to 4 s, no cursor moves'),
      L('нет ввода текста и запуска программ', 'no text input or starting programs')],
  },
  software: {
    title: L('Программные кнопки', 'Software buttons'),
    text: L('Боковые и другие кнопки обрабатывает SM809 Control; мышь записывается один раз.', 'SM809 Control handles the side and other buttons; the mouse is written once.'),
    pros: [L('свои действия кнопок для каждой программы — переключаются мгновенно и без записи в мышь', 'own button actions for every program — switched instantly and without writing the mouse'),
      L('любые сочетания (Ctrl+Win+→), ввод текста и паролей, запуск программ', 'any combination (Ctrl+Win+→), typing texts and passwords, starting programs'),
      L('макросы без ограничений прошивки, с движением курсора', 'macros without firmware limits, with cursor moves'),
      L('огонь и DPI остаются — на кнопках, отмеченных как аппаратные; программной можно сделать любую кнопку, кроме основной левой', 'rapid fire and DPI stay — on buttons marked as hardware; any button but the main left one can be a software button')],
    cons: [L('программные кнопки работают, только пока запущен SM809 Control', 'software buttons work only while SM809 Control is running'),
      L('программы, запущенные от имени администратора, могут не принимать нажатия', 'programs running as administrator may ignore the presses')],
  },
});

function modeDialog(first) {
  const cur = S.data.settings.buttonMode, info = MODE_INFO();
  const card = m => `<div class="modecard ${cur === m ? 'on' : ''}"><h3>${esc(info[m].title)}</h3><p class="small">${esc(info[m].text)}</p>
    <ul class="pros">${info[m].pros.map(x => `<li>${esc(x)}</li>`).join('')}</ul><ul class="cons">${info[m].cons.map(x => `<li>${esc(x)}</li>`).join('')}</ul>
    <button class="${cur === m ? '' : 'primary'}" data-mode="${m}" ${cur === m ? 'disabled' : ''}>${cur === m ? L('Сейчас включён', 'Current mode') : L('Выбрать', 'Choose')}</button></div>`;
  modal(`<h2>${first ? L('Как будут работать кнопки мыши?', 'How should the mouse buttons work?') : L('Режим кнопок', 'Button mode')}</h2>
    <p class="muted small">${L('Режим можно сменить в любой момент: «Настройки → Кнопки» или значок «Кнопки» вверху. Смена режима — одна запись раскладки кнопок в мышь; назначения обоих режимов сохраняются.',
      'You can change the mode at any time: Settings → Buttons, or the Buttons badge at the top. Switching writes the button layout to the mouse once; the assignments of both modes are kept.')}</p>
    <div class="grid2">${card('hardware')}${card('software')}</div>
    <div class="foot"><button data-r="later">${first ? L('Решить позже', 'Decide later') : L('Отмена', 'Cancel')}</button></div>`, box => {
    $('#modalBox').style.maxWidth = '880px';
    box.querySelector('[data-r=later]').onclick = closeModal;
    box.querySelectorAll('[data-mode]').forEach(b => b.onclick = async () => {
      const m = b.dataset.mode;
      closeModal();
      if (dirty && !await saveDraft()) return;
      const r = await run(() => api('buttons/mode', { mode: m }));
      if (r) {
        if (r.ok) toast(m === 'software' ? L('Программный режим включён', 'Software mode is on') : L('Аппаратный режим включён', 'Hardware mode is on'));
        else toast(L('Режим сохранён, но запись не удалась: ', 'Mode saved, but the write failed: ') + r.error + L('. Раскладка запишется, когда мышь будет на связи.', '. The layout will be written when the mouse is connected.'), true);
      }
      await reload();
    });
  });
}

function buttonsStatusHTML() {
  const s = softStat();
  const prof = profileById(s.profile);
  const parts = [
    `<span class="dot ${s.hook ? 'ok' : 'bad'}"></span> ${s.hook ? L('программные кнопки работают', 'software buttons active') : L('перехват не установлен', 'hook not installed')}`,
    `${L('активное окно', 'foreground')}: <b>${esc(s.exe || '—')}</b>`,
    `${L('кнопки по профилю', 'buttons from profile')}: <b>${esc(prof ? prof.name : '—')}</b>`,
  ];
  if (!s.carrier.every((c, i) => c === s.carriable[i])) parts.push(`<span style="color:var(--warn)">${L('раскладка ещё не записана в мышь', 'layout not written to the mouse yet')}</span>`);
  if (s.error) parts.push(`<span style="color:var(--warn)">⚠ ${esc(s.error)}</span>`);
  return parts.join(' &nbsp;·&nbsp; ');
}

// live update: highlight held software buttons, status line
function buttonsLive() {
  if (!isSoftMode()) return;
  const s = softStat();
  const sig = JSON.stringify([s.carrier, s.carriable, s.profile]);
  if (sig !== liveSig) { liveSig = sig; const st2 = $('#btnStat'); if (st2) st2.innerHTML = buttonsStatusHTML(); }
  document.querySelectorAll('[data-b]').forEach(el => el.classList.toggle('held', (s.held || []).includes('b' + (+el.dataset.b + 1))));
  const stl = $('#btnStat');
  if (stl) stl.innerHTML = buttonsStatusHTML();
}

async function toggleHW(i, makeHW) {
  if (dirty && !await saveDraft()) return renderView();
  const hw = S.data.settings.hwButtons.slice();
  hw[i] = makeHW;
  const ok = await confirmBox(makeHW ? L(`Кнопка ${i + 1} — аппаратная`, `Button ${i + 1} — hardware`) : L(`Кнопка ${i + 1} — программная`, `Button ${i + 1} — software`),
    (makeHW ? L('Действие кнопки будет храниться в мыши и работать без программы. ', 'The action is stored in the mouse and works without the app. ')
      : L('Действие кнопки будет выполнять SM809 Control по профилю активной программы; без программы кнопка ничего не будет делать. ', 'SM809 Control performs the action by the profile of the program in front; without the app the button does nothing. '))
    + L('Это касается всех профилей. В мышь запишется раскладка кнопок (одна запись).', 'This applies to all profiles. The button layout is written to the mouse once.'), L('Изменить', 'Change'));
  if (!ok) return renderView();
  const r = await run(() => api('buttons/hw', { hw }));
  if (r && !r.ok) toast(r.error, true);
  dirty = false;
  await reload();
}

function softButtonEditor(body, i) {
  const b = draft.soft[i] || { kind: '' };
  const set = nb => { draft.soft[i] = nb; touch(); renderView(); };
  const kinds = [['keys', L('Клавиша / сочетание', 'Key / combination')], ['mouse', L('Кнопка мыши', 'Mouse button')], ['macro', L('Макрос', 'Macro')],
    ['text', L('Текст', 'Text')], ['run', L('Запуск', 'Start')], ['disable', L('Ничего', 'Nothing')]];
  body.innerHTML = `<div class="cats">${kinds.map(([k, n]) => `<button data-kind="${k}" class="${k === b.kind ? 'on' : ''}">${esc(n)}</button>`).join('')}</div><div id="skb"></div>
    <p class="muted small">${L(`Это действие профиля ${esc(Q(draft.name))}. Для другой программы — свой профиль и правило на вкладке «Профили и игры»: кнопки переключатся сразу, как только её окно станет активным.`,
      `This is the action of profile ${esc(Q(draft.name))}. For another program use its own profile and a rule on the Profiles & games tab: the buttons switch as soon as its window is in front.`)}</p>`;
  body.querySelectorAll('[data-kind]').forEach(el => el.onclick = () => {
    const k = el.dataset.kind;
    if (k === b.kind) return;
    if (k === 'keys') return set({ kind: 'keys', keys: '' });
    if (k === 'mouse') return set({ kind: 'mouse', button: 'middle' });
    if (k === 'macro') { const ms = S.data.macros || []; return set({ kind: 'macro', macroId: (ms[0] || {}).id || '' }); }
    if (k === 'text') return set({ kind: 'text', text: '', method: 'unicode', after: '' });
    if (k === 'run') return set({ kind: 'run', path: '', args: '' });
    set({ kind: k });
  });
  const sb = body.querySelector('#skb');
  if (b.kind === 'keys') softKeysEditor(sb, b, set);
  if (b.kind === 'mouse') {
    sb.innerHTML = `<div class="catalog">${SOFT_MOUSE().map(([k, n]) => `<button data-mb="${k}" class="${b.button === k ? 'cur' : ''}">${esc(n)}</button>`).join('')}</div>`;
    sb.querySelectorAll('[data-mb]').forEach(el => el.onclick = () => set({ ...b, button: el.dataset.mb }));
  }
  if (b.kind === 'disable') sb.innerHTML = `<p class="muted small">${L('Кнопка ничего не делает.', 'The button does nothing.')}</p>`;
  if (!b.kind) sb.innerHTML = `<p class="muted small">${L('Действие не задано — кнопка ничего не делает.', 'No action — the button does nothing.')}</p>`;
  if (b.kind === 'macro') softMacroEditor(sb, b, set);
  if (b.kind === 'text') softTextEditor(sb, b, draft.soft, i);
  if (b.kind === 'run') softRunEditor(sb, b, draft.soft, i);
}

function softMacroEditor(body, b, set) {
  const ms = S.data.macros || [];
  const go = () => { tab = 'macros'; store.set('tab', tab); mEditId = 'new'; mDraft = newMacro(); mDirty = true; renderAll(); };
  if (!ms.length) {
    body.innerHTML = `<p class="muted">${L('Макросов пока нет.', 'No macros yet.')}</p><button class="primary" id="smgo">${L('Создать макрос', 'Create a macro')}</button>`;
    body.querySelector('#smgo').onclick = go;
    return;
  }
  body.innerHTML = `<div class="mpick"></div>
    <p class="muted small">${L('Макрос проигрывает программа — те же макросы, что на вкладке «Макросы», и тот же режим повтора. Сдвиги курсора здесь выполняются. «До повторного нажатия» останавливается повторным нажатием этой же кнопки.',
      'The app plays the macro — the same macros as on the Macros tab, with the same repeat mode. Cursor moves are performed here. “Until pressed again” stops when the same button is pressed again.')}</p>`;
  mountMacroList(body.querySelector('.mpick'), { key: 'pick', cur: b.macroId, sub: macroSub, onPick: id => set({ ...b, macroId: id }) });
}

// Text fields update the draft directly (no re-render while typing).
function softTextEditor(body, b, map, slot) {
  const upd = patch => { map[slot] = { ...map[slot], ...patch }; softTouch(); };
  const sealed = b.secret && b.enc && !b.text;
  const afterOpts = [['', L('ничего', 'nothing')], ['Enter', 'Enter'], ['Tab', 'Tab'], ['Ctrl+Enter', 'Ctrl+Enter'], ['Shift+Enter', 'Shift+Enter'], ['NumEnter', 'Num Enter']];
  if (b.after && !afterOpts.some(o => o[0] === b.after)) afterOpts.push([b.after, b.after]);
  body.innerHTML = `
    <label class="check"><input type="checkbox" id="stsec" ${b.secret ? 'checked' : ''}><span>${L('Пароль или секрет', 'Password or secret')}<small>${L('хранится зашифрованным и больше не показывается', 'stored encrypted and never shown again')}</small></span></label>
    <div class="row">${b.secret
      ? `<input type="password" id="sttext" class="grow" autocomplete="new-password" placeholder="${sealed ? L('сохранён — введите новый, чтобы заменить', 'stored — type a new one to replace it') : L('введите текст', 'enter the text')}">`
      : `<textarea id="sttext" class="grow" rows="3" style="resize:vertical" placeholder="${L('текст; перевод строки = Enter', 'text; a line break = Enter')}">${esc(b.text || '')}</textarea>`}</div>
    <div class="row"><span class="muted">${L('Ввод', 'Input')}:</span>
      <label class="check"><input type="radio" name="stm" value="unicode" ${b.method !== 'keys' ? 'checked' : ''}>${L('символы (не зависит от раскладки)', 'characters (any layout)')}</label>
      <label class="check"><input type="radio" name="stm" value="keys" ${b.method === 'keys' ? 'checked' : ''}>${L('нажатия клавиш', 'key presses')}</label>
      ${b.method === 'keys' ? `<select id="stlay"><option value="">${L('в текущей раскладке окна', 'in the window’s current layout')}</option>${softLayouts.map(l => `<option value="${esc(l.id)}" ${l.id === b.layout ? 'selected' : ''}>${esc(l.name)}</option>`).join('')}
        ${b.layout && !softLayouts.some(l => l.id === b.layout) ? `<option value="${esc(b.layout)}" selected>${L('не установлена', 'not installed')} (${esc(b.layout)})</option>` : ''}</select>` : ''}</div>
    <div class="row"><label class="field"><span>${L('Затем нажать', 'Then press')}</span><select id="staft">${afterOpts.map(([v, n]) => `<option value="${esc(v)}" ${(b.after || '') === v ? 'selected' : ''}>${esc(n)}</option>`).join('')}</select></label>
      <label class="field"><span>${L('Пауза между символами, мс', 'Delay between characters, ms')}</span><input type="number" id="stdel" min="0" max="500" value="${b.charDelay || 0}" style="width:110px"></label></div>
    <p class="muted small">${L('«Символы» вводят текст как есть в любой раскладке — подходит почти везде. Если программа их не принимает (некоторые игры, удалённый рабочий стол, защищённые поля пароля), выберите «нажатия клавиш» и раскладку: на время ввода окно переключится на неё, потом вернётся обратно; Caps Lock учитывается.',
      '“Characters” types the text as is in any layout — works almost everywhere. If a program ignores them (some games, remote desktop, protected password fields), choose “key presses” and a layout: the window switches to it while typing and back afterwards; Caps Lock is handled.')}</p>
    ${b.secret ? `<p class="small" style="color:var(--warn)">${L('Секрет шифруется средствами Windows (DPAPI) и расшифровывается только под вашей учётной записью на этом ПК — после переноса на другой компьютер его нужно ввести заново. Но его введёт любой, кто нажмёт кнопку, пока программа запущена: назначайте пароли в профиле нужной программы, а не в общих.',
      'The secret is encrypted by Windows (DPAPI) and can be decrypted only by your account on this PC — after moving to another computer enter it again. But anyone pressing the button while the app runs will type it: bind passwords in the profile of that program, not in Global.')}</p>` : ''}`;
  const ta = body.querySelector('#sttext');
  ta.oninput = () => upd({ text: ta.value });
  body.querySelector('#stsec').onchange = e => {
    const on = e.target.checked;
    map[slot] = { ...map[slot], secret: on };
    if (!on && map[slot].enc && !map[slot].text) map[slot].enc = ''; // the stored secret is not shown: enter the text again
    softTouch(); renderView();
  };
  body.querySelectorAll('[name=stm]').forEach(r => r.onchange = () => { upd({ method: r.value }); renderView(); });
  const lay = body.querySelector('#stlay'); if (lay) lay.onchange = () => upd({ layout: lay.value });
  body.querySelector('#staft').onchange = e => upd({ after: e.target.value });
  body.querySelector('#stdel').onchange = e => upd({ charDelay: Math.max(0, Math.min(500, +e.target.value | 0)) });
}

let pickResolve = null;
window.smPickResult = p => { if (pickResolve) { pickResolve(p); pickResolve = null; } };

function softRunEditor(body, b, map, slot) {
  const upd = patch => { map[slot] = { ...map[slot], ...patch }; softTouch(); };
  const canPick = typeof window.smPickFile === 'function';
  body.innerHTML = `<div class="row"><label class="field grow"><span>${L('Программа, ярлык, документ, папка или адрес сайта', 'Program, shortcut, document, folder or web address')}</span>
      <input type="text" id="srpath" value="${esc(b.path || '')}" placeholder="C:\\Program Files\\…\\app.exe"></label>
      ${canPick ? `<button id="srpick" style="align-self:flex-end">${L('Обзор…', 'Browse…')}</button>` : ''}</div>
    <div class="row"><label class="field grow"><span>${L('Параметры (необязательно)', 'Arguments (optional)')}</span><input type="text" id="srargs" value="${esc(b.args || '')}"></label></div>
    <p class="muted small">${L('Запускается при нажатии кнопки, с правами обычного пользователя. Переменные вида %USERPROFILE% раскрываются.', 'Started when the button is pressed, with normal user rights. Variables such as %USERPROFILE% are expanded.')}</p>`;
  const path = body.querySelector('#srpath');
  path.oninput = () => upd({ path: path.value });
  body.querySelector('#srargs').oninput = e => upd({ args: e.target.value });
  const pk = body.querySelector('#srpick');
  if (pk) pk.onclick = async () => {
    pk.disabled = true;
    const p = await new Promise(res => { pickResolve = res; window.smPickFile(path.value); });
    pk.disabled = false;
    if (p) { path.value = p; upd({ path: p }); renderView(); }
  };
}

const SOFT_MODS = ['Ctrl', 'Shift', 'Alt', 'Win'];
function softKeysEditor(body, b, set) {
  const parts = (b.keys || '').split('+').filter(Boolean);
  let mods = SOFT_MODS.filter(m => parts.includes(m));
  let key = parts.filter(p => !SOFT_MODS.includes(p)).join('+');
  const keyList = (softKeys || []).filter(k => !k.mod);
  body.innerHTML = `<div class="row">${SOFT_MODS.map(m => `<label class="check"><input type="checkbox" data-mod="${m}" ${mods.includes(m) ? 'checked' : ''}>${m}</label>`).join('')}</div>
    <div class="row"><select id="skey"><option value="">${L('— клавиша —', '— key —')}</option>${keyList.map(k => `<option ${k.name === key ? 'selected' : ''}>${esc(k.name)}</option>`).join('')}</select>
    <span class="muted">${L('или', 'or')}</span><span class="keycap" id="scap" tabindex="0">${L('записать с клавиатуры', 'record from the keyboard')}</span></div>
    <div class="row"><span class="muted">${L('Будет', 'Result')}: <b>${esc(b.keys || '—')}</b></span></div>
    <p class="muted small">${L('Сочетание нажимается, пока зажата кнопка мыши. Запись с клавиатуры ловит и системные сочетания вроде Ctrl+Win+→; Esc — отмена.',
      'The combination is held while the mouse button is held. Recording catches system combinations such as Ctrl+Win+→ too; Esc cancels.')}</p>`;
  const apply = () => {
    const k = mods.concat(key ? [key] : []).join('+');
    set({ ...b, kind: 'keys', keys: k });
  };
  body.querySelectorAll('[data-mod]').forEach(el => el.onchange = () => {
    mods = SOFT_MODS.filter(m => m === el.dataset.mod ? el.checked : mods.includes(m));
    apply();
  });
  body.querySelector('#skey').onchange = e => { key = e.target.value; apply(); };
  const cap = body.querySelector('#scap');
  cap.onclick = async () => {
    if (cap.classList.contains('listen')) return;
    cap.classList.add('listen'); cap.textContent = L('нажмите сочетание…', 'press a combination…');
    const r = await run(() => api('soft/capture', {}));
    cap.classList.remove('listen');
    if (r && r.combo) set({ ...b, kind: 'keys', keys: r.combo });
    else { cap.textContent = L('записать с клавиатуры', 'record from the keyboard'); if (r) toast(L('Запись отменена', 'Recording cancelled')); }
  };
}
