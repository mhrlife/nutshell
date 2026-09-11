// nutshell UI, "reader" layout: the rail on one side holds the conversation
// (question, spoken summary, controls); the document pane on the other side
// shows the full answer of the selected turn.

const $ = (s) => document.querySelector(s);
const el = {
  transcript: $('#transcript'), empty: $('#empty'), emptyHint: $('#empty-hint'),
  status: $('#status'), brand: $('#brand'), input: $('#input'), talk: $('#talk'),
  settingsBtn: $('#settings-btn'), foot: $('#foot'), footNote: $('#foot-note'), doc: $('#doc'), docBody: $('#doc-body'),
  docEmpty: $('#doc-empty'), docCost: $('#doc-cost'), copy: $('#copy'), docClose: $('#doc-close'),
};

let cfg = { lang: 'en', project: '', agent: 'agent', voice: false };
let state = 'idle'; // idle | listening | transcribing | working
let recorder = null;
let timer = null;
let turns = [];
let selected = null;
// { audio, turn, clip }; clip is set, and turn null, for a passage's clip, and
// only narration is set while a full answer plays (see narrator.js)
let player = null;
let pendingStt = null; // cost of the last transcription, charged to the next question
let notice = ''; // transient message shown in the status line
let noticeIsError = false;

const agentName = () => cfg.agent || 'agent';

function setStatus(text, live) {
  el.status.textContent = text;
  el.status.classList.toggle('live', !!live);
}

function applyLanguage() {
  setActiveLanguage(settings.lang);
  el.brand.textContent = t('brand');
  el.brand.title = cfg.project || '';
  el.emptyHint.textContent = t('hint');
  el.input.placeholder = t('placeholder');
  el.docEmpty.querySelector('p').textContent = t('docEmpty');
  el.copy.innerHTML = ICONS.copy;
  el.copy.title = t('copy');
  el.docClose.innerHTML = ICONS.close;
  el.settingsBtn.innerHTML = ICONS.gear;
  el.settingsBtn.title = t('settings');
  turns.forEach(renderMeta);
  turns.forEach((turn) => turn.clips.forEach(renderClip));
  renderNarrator();
  renderUnsent();
  renderCosts();
  renderState();
}

function renderState() {
  const live = state !== 'idle' || !!player || clipsPending > 0;
  const waiting = state === 'working' && promptPending();
  const preparing = clipsPending > 0 || (player && player.narration && !player.narration.audio);
  const idleNote = preparing ? t('loading') : player ? t('speaking') : t(state);
  setStatus(offline ? t('offline') : notice || (waiting ? t('promptWaiting') : idleNote), live);
  el.status.classList.toggle('alert', !!notice && noticeIsError);
  el.talk.disabled = state === 'working' || state === 'transcribing';
  el.talk.classList.toggle('listening', state === 'listening');
  el.talk.classList.toggle('transcribing', state === 'transcribing');
  el.talk.innerHTML = { listening: ICONS.stop, transcribing: ICONS.spinner }[state] || ICONS.mic;
  el.talk.title = state === 'listening' ? t('listening') : t('speak');
  el.foot.dataset.state = state;
  setFootNote(waiting ? t('promptNote')
    : { listening: t('listenNote'), transcribing: t('transcribingNote') }[state] || '');
  if (state === 'idle') resizeInput();
}

function setFootNote(text) {
  el.footNote.textContent = text;
  el.footNote.hidden = !text;
}

// flash puts one line in the status bar. Failures stay up longer and are
// coloured, because the only other sign of them is a screen that did nothing.
function flash(message, isError) {
  notice = message;
  noticeIsError = !!isError;
  renderState();
  setTimeout(() => {
    if (notice !== message) return;
    notice = '';
    noticeIsError = false;
    renderState();
  }, isError ? 8000 : 4000);
}

// ---- turns -----------------------------------------------------------------

function addTurn(id, question, selection) {
  const node = document.getElementById('turn-template').content.firstElementChild.cloneNode(true);
  const turn = { id, question, node, answer: null, error: null, audio: null, loadingAudio: false, clips: [], cost: newCost() };
  turn.cost.stt = pendingStt;
  pendingStt = null;
  node.querySelector('.q').textContent = question;
  if (selection) {
    const about = node.querySelector('.q-quote');
    about.textContent = oneLine(selection);
    about.dir = isRTL(selection) ? 'rtl' : 'ltr';
    about.hidden = false;
  }
  el.empty.hidden = true;
  el.transcript.appendChild(node);
  turns.push(turn);
  select(turn);
  return turn;
}

function showActivity(turn, kind, text) {
  const box = turn.node.querySelector('.activity');
  box.hidden = false;
  box.classList.add('live');
  const think = kind === 'think';
  box.innerHTML = `<span class="k"></span><span class="${think ? 'think' : 'v'}"></span>`;
  box.querySelector('.k').textContent = think ? t('think') : kind.toLowerCase();
  box.querySelector(think ? '.think' : '.v').textContent = text;
  if (think) box.querySelector('.think').dir = 'auto';
  scrollToEnd();
}

function finishTurn(turn, answer) {
  turn.answer = answer;
  turn.node.querySelector('.activity').hidden = true;
  const summary = turn.node.querySelector('.summary');
  summary.textContent = plainText(answer.summary);
  summary.hidden = false;
  turn.node.querySelector('.meta').hidden = false;
  turn.cost.agent = answer.cost_known ? answer.cost_usd : null;
  turn.cost.agentKnown = !!answer.cost_known;
  renderMeta(turn);
  renderCosts();
  if (selected === turn) renderDoc();
  scrollToEnd();
  if (synced && settings.autoSpeak && cfg.voice) speak(turn);
}

function failTurn(turn, message) {
  turn.error = message;
  turn.node.querySelector('.activity').hidden = true;
  const box = turn.node.querySelector('.error');
  box.textContent = message;
  box.hidden = false;
  scrollToEnd();
}

function renderMeta(turn) {
  if (!turn.answer) return;
  const speakBtn = turn.node.querySelector('.speak');
  const playing = player && player.turn === turn && !player.audio.paused;
  speakBtn.hidden = !cfg.voice;
  speakBtn.innerHTML = turn.loadingAudio ? ICONS.spinner : (playing ? ICONS.pause : ICONS.play);
  speakBtn.title = turn.loadingAudio ? t('loading') : (playing ? t('pause') : (turn.audio ? t('replay') : t('speak')));
  turn.node.querySelector('.open-full').textContent = t('fullAnswer');
}

function renderCosts() {
  for (const turn of turns) renderCostChip(turn.node.querySelector('.cost'), turn.cost, t('costTurn'), agentName());
  renderSessionCost(turns, agentName());
  if (selected) renderCostChip(el.docCost, selected.cost, t('costTurn'), agentName());
}

function select(turn) {
  if (selected) selected.node.classList.remove('selected');
  selected = turn;
  if (turn) turn.node.classList.add('selected');
  renderDoc();
}

function renderDoc() {
  const has = selected && selected.answer;
  el.docBody.hidden = !has;
  el.docEmpty.hidden = !!has;
  el.copy.hidden = !has;
  el.docCost.hidden = !has;
  renderNarrator();
  if (!has) return;
  el.docBody.innerHTML = marked.parse(selected.answer.full || '');
  el.docBody.dir = isRTL(selected.answer.full) ? 'rtl' : 'ltr';
  renderCostChip(el.docCost, selected.cost, t('costTurn'), agentName());
  el.doc.scrollTop = 0;
}

function scrollToEnd() {
  el.transcript.scrollTop = el.transcript.scrollHeight;
}

// ---- voice in --------------------------------------------------------------

async function startListening() {
  if (!cfg.voice) { flash(t('noVoice')); return; }
  stopPlayback(); // before asking for the mic: the answer must not talk over you
  recorder = new Recorder();
  try {
    await recorder.start();
  } catch (err) {
    recorder = null;
    fail('microphone', 'micDenied', err);
    return;
  }
  state = 'listening';
  renderState();
  const start = Date.now();
  timer = setInterval(() => setStatus(`${t('listening')} ${fmtTime(Date.now() - start)}`, true), 250);
}

async function stopListening() {
  clearInterval(timer);
  const rec = recorder;
  recorder = null;
  if (!rec) { // nothing was being recorded: say so rather than throwing
    logIssue('warn', 'record', 'stop with no recording in progress');
    state = 'idle';
    renderState();
    return;
  }
  state = 'transcribing';
  renderState();
  let wav;
  try {
    wav = await rec.stop();
  } catch (err) { // the browser could not decode its own recording
    state = 'idle';
    fail('record', 'transcribeFailed', err);
    return;
  }
  if (!wav) { state = 'idle'; warn('record', 'tooShort'); return; }
  transcribe(wav); // see transcribe.js, which keeps the recording if this fails
}

function cancelListening() {
  clearInterval(timer);
  if (recorder) recorder.cancel();
  recorder = null;
  state = 'idle';
  renderState();
}

// ---- voice out -------------------------------------------------------------

async function speak(turn) {
  if (!cfg.voice || !turn.answer) return;
  if (player && player.turn === turn) {
    if (player.audio.paused) player.audio.play(); else player.audio.pause();
    renderMeta(turn);
    return;
  }
  stopPlayback();
  if (!turn.audio) {
    turn.loadingAudio = true;
    renderMeta(turn);
    try {
      const resp = await fetch('/api/speak', {
        method: 'POST', headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ text: plainText(turn.answer.summary), lang: settings.lang }),
      });
      if (!resp.ok) throw await httpError(resp);
      turn.audio = URL.createObjectURL(await resp.blob());
      priceClip(turn, resp.headers.get('X-Generation-Id'));
    } catch (err) {
      const detail = errText(err);
      logIssue('error', 'speak', detail);
      failTurn(turn, t('speakFailed').replace('{error}', detail));
      return;
    } finally {
      turn.loadingAudio = false;
    }
  }
  play(turn.audio, turn);
}

// play starts audio for a turn's summary, or for a passage's clip when clip
// is given. Both keep their audio for replays.
function play(url, turn, clip) {
  const audio = new Audio(url);
  player = { audio, turn, clip };
  const refresh = () => { if (turn) renderMeta(turn); if (clip) renderClip(clip); renderState(); };
  audio.onplay = refresh;
  audio.onpause = refresh;
  audio.onended = () => { if (player && player.audio === audio) player = null; refresh(); };
  audio.play();
  refresh();
}

// priceClip fetches the clip's cost once OpenRouter has priced it (a few seconds later).
async function priceClip(turn, id) {
  if (!id) return;
  try {
    const resp = await fetch(`/api/cost?id=${encodeURIComponent(id)}`);
    if (!resp.ok) { logIssue('warn', 'cost', errText(await httpError(resp))); return; }
    addCost(turn, 'tts', (await resp.json()).cost_usd);
  } catch (err) { // the answer is fine; only its price is unknown
    logIssue('warn', 'cost', errText(err));
  }
}

function stopPlayback() {
  if (!player) return;
  if (player.narration) { pauseNarration(player.narration); return; } // keeps its place, to go on later
  const { audio, turn, clip } = player;
  player = null;
  audio.pause();
  if (turn) renderMeta(turn);
  if (clip) renderClip(clip);
  renderState();
}

// ---- input & keys ----------------------------------------------------------

function resizeInput() {
  el.input.dir = dirFor(el.input.value);
  if (el.input.offsetParent === null) { // hidden by the footer state: measuring would lock it at 0px
    el.input.style.height = '';
    return;
  }
  el.input.style.height = 'auto';
  el.input.style.height = `${el.input.scrollHeight}px`;
  el.input.style.overflowY = el.input.scrollHeight > 120 ? 'auto' : 'hidden';
}

function onTalk() {
  if (state === 'listening') stopListening();
  else if (state === 'idle') startListening();
}

function openDoc() { el.doc.classList.add('open'); }

function closeDoc() { el.doc.classList.remove('open'); }

document.addEventListener('keydown', (e) => {
  const typing = e.target === el.input || e.target.tagName === 'SELECT';
  if (e.key === 'Escape') {
    if (promptPending()) dismissPrompt();
    else if (!document.getElementById('settings').hidden) closeSettings();
    else if (state === 'listening') cancelListening();
    else if (state === 'working') fetch('/api/cancel', { method: 'POST' });
    else if (player) stopPlayback();
    else if (dropSelection()) return;
    else if (el.doc.classList.contains('open')) closeDoc();
    else if (typing) el.input.blur();
    return;
  }
  if (typing) {
    if (e.key === 'Enter' && !e.shiftKey && e.target === el.input) { e.preventDefault(); ask(el.input.value); }
    return;
  }
  if (e.key === ' ' && !e.repeat) { e.preventDefault(); onTalk(); }
  else if (e.key === 'f' && selected && selected.answer) openDoc();
  else if (narratorKey(e.key)) e.preventDefault();
});

el.input.addEventListener('input', resizeInput);
el.talk.addEventListener('click', onTalk);
el.settingsBtn.addEventListener('click', toggleSettings);
el.docClose.addEventListener('click', closeDoc);
el.copy.addEventListener('click', () => {
  if (!selected || !selected.answer) return;
  navigator.clipboard.writeText(selected.answer.full).then(() => {
    el.copy.innerHTML = ICONS.check;
    setTimeout(() => { el.copy.innerHTML = ICONS.copy; }, 1500);
  });
});

el.transcript.addEventListener('click', (e) => {
  const node = e.target.closest('.turn');
  const turn = turns.find((x) => x.node === node);
  if (!turn) return;
  if (e.target.closest('.speak')) { speak(turn); return; }
  const clipButton = e.target.closest('.clip-play');
  if (clipButton) {
    const clip = turn.clips.find((c) => c.node.contains(clipButton));
    if (clip) toggleClip(clip);
    return;
  }
  select(turn);
  if (e.target.closest('.open-full')) openDoc();
});

// plainText strips the Markdown a model sometimes leaves in a summary meant to be spoken.
function plainText(s) {
  return (s || '').replace(/```[\s\S]*?```/g, ' ').replace(/[`*_~#>]/g, '').replace(/\s+/g, ' ').trim();
}

function fmtTime(ms) {
  const s = Math.floor(ms / 1000);
  return num(`${Math.floor(s / 60)}:${String(s % 60).padStart(2, '0')}`);
}

// ---- boot ------------------------------------------------------------------

(async () => {
  try {
    const resp = await fetch('/api/config');
    if (!resp.ok) throw await httpError(resp);
    cfg = await resp.json();
  } catch (err) { // the page loaded but the server is not answering
    logIssue('error', 'config', errText(err));
    offline = true;
  }
  await loadSettings(cfg.lang);
  marked.setOptions({ gfm: true, breaks: false });
  applyLanguage();
  renderSettingsPanel(applyLanguage);
  renderDoc();
  connect();
  if (!cfg.voice) flash(t('noVoice'), true);
})();
