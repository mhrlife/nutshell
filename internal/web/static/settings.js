// User preferences. They live on the server (a JSON file in the user's
// config directory) so they survive the random port each launch gets;
// localStorage is only a fallback when the server cannot be reached.

const DEFAULT_SETTINGS = { lang: null, autoSend: true, autoSpeak: true };
const settings = { ...DEFAULT_SETTINGS };

async function loadSettings(defaultLang) {
  let stored = {};
  try {
    const resp = await fetch('/api/settings');
    if (resp.ok) stored = await resp.json();
  } catch {
    try { stored = JSON.parse(localStorage.getItem('nutshell') || '{}'); } catch { stored = {}; }
  }
  Object.assign(settings, DEFAULT_SETTINGS, stored);
  if (!languageByCode(settings.lang)) settings.lang = languageByCode(defaultLang) ? defaultLang : LANGUAGES[0].code;
  return settings;
}

let saveTimer = null;

function saveSettings() {
  try { localStorage.setItem('nutshell', JSON.stringify(settings)); } catch { /* private mode */ }
  clearTimeout(saveTimer);
  saveTimer = setTimeout(() => {
    fetch('/api/settings', {
      method: 'PUT', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(settings),
    }).catch(() => {});
  }, 150);
}

// Settings panel -------------------------------------------------------------

function renderSettingsPanel(onChange) {
  const sheet = document.getElementById('settings');
  const options = LANGUAGES.map((l) => `<option value="${l.code}"${l.code === settings.lang ? ' selected' : ''}>${l.name}</option>`).join('');
  sheet.innerHTML = `
    <div class="row"><span class="label">${t('settings')}</span><button class="icon-btn" id="settings-close" type="button">${ICONS.close}</button></div>
    <label class="row"><span>${t('language')}</span><select id="set-lang">${options}</select></label>
    <div class="row" data-toggle="autoSend"><span>${t('autoSend')}</span><span class="switch${settings.autoSend ? ' on' : ''}"></span></div>
    <div class="row" data-toggle="autoSpeak"><span>${t('autoSpeak')}</span><span class="switch${settings.autoSpeak ? ' on' : ''}"></span></div>
    <div class="keys">${t('keys')}</div>`;

  sheet.querySelector('#settings-close').addEventListener('click', closeSettings);
  sheet.querySelector('#set-lang').addEventListener('change', (e) => {
    settings.lang = e.target.value;
    saveSettings();
    onChange();
    renderSettingsPanel(onChange);
  });
  sheet.querySelectorAll('[data-toggle]').forEach((row) => {
    row.style.cursor = 'pointer';
    row.addEventListener('click', () => {
      const key = row.dataset.toggle;
      settings[key] = !settings[key];
      saveSettings();
      row.querySelector('.switch').classList.toggle('on', settings[key]);
      onChange();
    });
  });
}

function openSettings() {
  document.getElementById('settings').hidden = false;
}

function closeSettings() {
  document.getElementById('settings').hidden = true;
}

function toggleSettings() {
  const sheet = document.getElementById('settings');
  if (sheet.hidden) openSettings(); else closeSettings();
}

document.addEventListener('click', (e) => {
  const sheet = document.getElementById('settings');
  if (sheet.hidden) return;
  if (sheet.contains(e.target) || e.target.closest('#settings-btn')) return;
  closeSettings();
});
