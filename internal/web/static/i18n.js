// Language registry. Each file in lang/ calls registerLanguage() with its
// code, display name, text direction and the UI strings. The settings panel
// lists whatever is registered. What the models are told about a language
// (how to transcribe it, how to speak it, how a reply in it must read) lives
// on the server in internal/lang, keyed by the same code; every request
// carries the code so the server can pick those rules.

const LANGUAGES = [];

function registerLanguage(def) {
  if (!def || !def.code || !def.strings) throw new Error('registerLanguage: code and strings are required');
  LANGUAGES.push(def);
}

function languageByCode(code) {
  return LANGUAGES.find((l) => l.code === code) || null;
}

let activeLanguage = null;

function setActiveLanguage(code) {
  activeLanguage = languageByCode(code) || LANGUAGES[0];
  document.documentElement.lang = activeLanguage.code;
  document.documentElement.dir = activeLanguage.dir || 'ltr';
  return activeLanguage;
}

// t looks a string up in the active language, falling back to the first
// registered language so a missing translation never renders blank.
function t(key) {
  const s = activeLanguage ? activeLanguage.strings[key] : undefined;
  if (s !== undefined) return s;
  const fallback = LANGUAGES[0] && LANGUAGES[0].strings[key];
  return fallback === undefined ? key : fallback;
}

// num renders the digits of an already-formatted number in the active
// language's numerals, so counters and costs read as ۰۱۲۳ in Persian. A
// language without a `digits` string keeps the Latin ones.
function num(value) {
  const lang = activeLanguage || {};
  if (!lang.digits) return String(value);
  let s = String(value);
  if (lang.decimal) s = s.replace(/(\d)\.(?=\d)/g, `$1${lang.decimal}`);
  return s.replace(/\d/g, (d) => lang.digits[Number(d)]);
}

// Icons are inline SVG so they scale and recolor with the text.
const ICONS = {
  mic: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.75" stroke-linecap="round" stroke-linejoin="round"><rect x="9" y="3" width="6" height="11" rx="3"></rect><path d="M5 11a7 7 0 0 0 14 0"></path><path d="M12 18v3"></path></svg>',
  stop: '<svg viewBox="0 0 24 24" fill="currentColor"><rect x="6" y="6" width="12" height="12" rx="1"></rect></svg>',
  play: '<svg viewBox="0 0 24 24" fill="currentColor"><path d="M7 5v14l11-7z"></path></svg>',
  pause: '<svg viewBox="0 0 24 24" fill="currentColor"><rect x="6" y="5" width="4" height="14"></rect><rect x="14" y="5" width="4" height="14"></rect></svg>',
  copy: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.75" stroke-linecap="round" stroke-linejoin="round"><rect x="9" y="9" width="11" height="11" rx="1.5"></rect><path d="M5 15V5a1 1 0 0 1 1-1h10"></path></svg>',
  check: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M5 12l5 5L19 7"></path></svg>',
  close: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.75" stroke-linecap="round"><path d="M6 6l12 12"></path><path d="M18 6L6 18"></path></svg>',
  gear: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="12" r="3"></circle><path d="M19.4 15a1.7 1.7 0 0 0 .3 1.8l.1.1a2 2 0 1 1-2.8 2.8l-.1-.1a1.7 1.7 0 0 0-1.8-.3 1.7 1.7 0 0 0-1 1.5V21a2 2 0 1 1-4 0v-.1a1.7 1.7 0 0 0-1.1-1.5 1.7 1.7 0 0 0-1.8.3l-.1.1a2 2 0 1 1-2.8-2.8l.1-.1a1.7 1.7 0 0 0 .3-1.8 1.7 1.7 0 0 0-1.5-1H3a2 2 0 1 1 0-4h.1a1.7 1.7 0 0 0 1.5-1.1 1.7 1.7 0 0 0-.3-1.8l-.1-.1a2 2 0 1 1 2.8-2.8l.1.1a1.7 1.7 0 0 0 1.8.3h.1a1.7 1.7 0 0 0 1-1.5V3a2 2 0 1 1 4 0v.1a1.7 1.7 0 0 0 1 1.5 1.7 1.7 0 0 0 1.8-.3l.1-.1a2 2 0 1 1 2.8 2.8l-.1.1a1.7 1.7 0 0 0-.3 1.8v.1a1.7 1.7 0 0 0 1.5 1H21a2 2 0 1 1 0 4h-.1a1.7 1.7 0 0 0-1.5 1z"></path></svg>',
  spinner: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><path d="M12 3a9 9 0 1 0 9 9"></path></svg>',
  headphones: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.75" stroke-linecap="round" stroke-linejoin="round"><path d="M4 15v-3a8 8 0 0 1 16 0v3"></path><rect x="3" y="14" width="4.5" height="7" rx="1.5"></rect><rect x="16.5" y="14" width="4.5" height="7" rx="1.5"></rect></svg>',
  // {n} is the number of seconds the button skips
  back: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round"><path d="M3 12a9 9 0 1 0 9-9 9.75 9.75 0 0 0-6.74 2.74L3 8"></path><path d="M3 3v5h5"></path><text x="12" y="15.4" text-anchor="middle" font-size="8.5" font-weight="600" fill="currentColor" stroke="none">{n}</text></svg>',
  forward: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round"><path d="M21 12a9 9 0 1 1-9-9c2.52 0 4.93 1 6.74 2.74L21 8"></path><path d="M21 3v5h-5"></path><text x="12" y="15.4" text-anchor="middle" font-size="8.5" font-weight="600" fill="currentColor" stroke="none">{n}</text></svg>',
};

// True when the text reads right-to-left (Arabic script, which covers Persian).
function isRTL(text) {
  const m = /[؀-ۿݐ-ݿﭐ-﷿ﹰ-﻿]/.exec(text || '');
  const l = /[A-Za-z]/.exec(text || '');
  if (!m) return false;
  if (!l) return true;
  return m.index < l.index;
}

// dirFor picks a direction for a text box: what the text itself says, or the
// interface language while the box is still empty, so the placeholder reads
// the right way round.
function dirFor(text) {
  if (!text) return document.documentElement.dir || 'ltr';
  return isRTL(text) ? 'rtl' : 'ltr';
}
