// The full answer read aloud, with the controls of any audio player: play and
// pause, ten seconds back or forward, a timeline to click or drag, and the
// playback speed. A long answer is too much for one speech request, so it is
// voiced in segments of a few paragraphs. The first is short, so the voice
// starts soon, and only the next few are voiced ahead of the one playing, so
// an answer left after a minute is not paid for in full. The timeline covers
// the whole answer from the start; how long the part not voiced yet will run
// is estimated from the part that is. What is read comes from speakable.js.

const NARRATION_AHEAD = 2; // segments voiced beyond the one playing
const NARRATION_SKIP = 10; // seconds the back and forward buttons move
const NARRATION_RATES = [1, 1.25, 1.5, 2, 0.75];
const SECONDS_PER_CHAR = 0.07; // the guess until a segment has been voiced

const narrator = document.getElementById('narrator');
const listenBtn = document.getElementById('listen');
const npTrack = narrator.querySelector('.np-track');

let narrationRate = 1;
let dragAt = null; // where on the timeline, 0..1, a drag in progress is
let narrationFrame = 0;

// ---- the timeline ----------------------------------------------------------

// narrationSpans places every segment on the timeline, in seconds: voiced
// segments at their real length, the rest at the pace heard so far.
function narrationSpans(n) {
  let secs = 0;
  let chars = 0;
  for (const seg of n.segments) if (seg.status === 'ready') { secs += seg.duration; chars += seg.chars; }
  const pace = chars ? secs / chars : SECONDS_PER_CHAR;
  let at = 0;
  return n.segments.map((seg) => {
    const span = { start: at, len: seg.status === 'ready' ? seg.duration : seg.chars * pace, ready: seg.status === 'ready' };
    at += span.len;
    return span;
  });
}

function narrationPosition(n, spans) {
  const span = spans[n.index];
  const offset = n.audio && n.audio.readyState > 0 ? n.audio.currentTime : n.at * span.len;
  return span.start + Math.min(offset, span.len);
}

const spansEnd = (spans) => spans[spans.length - 1].start + spans[spans.length - 1].len;

function seekNarration(n, seconds) {
  const spans = narrationSpans(n);
  const target = Math.max(0, Math.min(seconds, spansEnd(spans) - 0.05));
  let i = spans.findIndex((s) => target < s.start + s.len);
  if (i < 0) i = spans.length - 1;
  const at = spans[i].len ? (target - spans[i].start) / spans[i].len : 0;
  if (i === n.index && n.audio) {
    n.audio.currentTime = at * spans[i].len;
  } else {
    unmountNarration(n);
    n.index = i;
    n.at = at;
    resumeNarration(n);
    prepareNarration(n);
  }
  renderNarration(n);
}

function skipNarration(n, seconds) {
  seekNarration(n, narrationPosition(n, narrationSpans(n)) + seconds);
}

// ---- playback --------------------------------------------------------------

function playNarration(n) {
  if (player && player.narration !== n) stopPlayback();
  const last = n.segments.length - 1;
  if (n.index === last && n.at >= 1 && !n.audio) { n.index = 0; n.at = 0; } // it had finished: from the top
  n.playing = true;
  player = { audio: null, turn: null, clip: null, narration: n };
  resumeNarration(n);
  prepareNarration(n);
  renderNarration(n);
  renderState();
  tickNarration();
}

function pauseNarration(n) {
  n.playing = false;
  if (n.audio) n.audio.pause();
  if (player && player.narration === n) player = null;
  renderNarration(n);
  renderState();
}

// resumeNarration puts the current segment's audio in place once it is voiced,
// and starts it when the narration is meant to be playing.
function resumeNarration(n) {
  const seg = n.segments[n.index];
  if (!n.audio && seg.status === 'ready') {
    const audio = new Audio(seg.url);
    audio.defaultPlaybackRate = narrationRate;
    audio.playbackRate = narrationRate;
    audio.currentTime = n.at * seg.duration;
    audio.onended = () => advanceNarration(n);
    // paused or started from outside the page, e.g. by a media key
    audio.onpause = () => { if (!audio.ended && n.playing) pauseNarration(n); };
    audio.onplay = () => { if (!n.playing) playNarration(n); };
    n.audio = audio;
  }
  if (n.playing && n.audio && n.audio.paused) {
    n.audio.play().catch((err) => {
      if (err.name === 'AbortError') return; // paused again before it started
      pauseNarration(n);
      fail('speak', 'speakFailed', err);
    });
  }
}

function unmountNarration(n) {
  const audio = n.audio;
  if (!audio) return;
  const seg = n.segments[n.index];
  if (audio.readyState > 0 && seg.duration) n.at = Math.min(1, audio.currentTime / seg.duration);
  audio.onended = null;
  audio.onpause = null;
  audio.onplay = null;
  audio.pause();
  n.audio = null;
}

function advanceNarration(n) {
  unmountNarration(n);
  if (n.index === n.segments.length - 1) { // the end: stay there until played again
    n.at = 1;
    pauseNarration(n);
    return;
  }
  n.index++;
  n.at = 0;
  resumeNarration(n);
  prepareNarration(n);
  renderNarration(n);
  renderState();
}

// prepareNarration voices the current segment, and the few after it while the
// narration plays. A segment that failed gets another go once it is current.
function prepareNarration(n) {
  const last = n.playing ? Math.min(n.segments.length - 1, n.index + NARRATION_AHEAD) : n.index;
  for (let i = n.index; i <= last; i++) {
    const seg = n.segments[i];
    if (seg.status === 'failed' && i === n.index && n.playing) seg.status = 'idle';
    if (seg.status === 'idle') voiceSegment(n, seg);
  }
}

async function voiceSegment(n, seg) {
  seg.status = 'loading';
  try {
    const resp = await postJSON('/api/speak', { text: seg.text, lang: settings.lang });
    const blob = await resp.blob();
    seg.duration = await wavSeconds(blob);
    seg.url = URL.createObjectURL(blob);
    seg.status = 'ready';
    priceClip(n.turn, resp.headers.get('X-Generation-Id'));
  } catch (err) {
    seg.status = 'failed';
    const current = n.segments[n.index] === seg;
    if (current && n.playing) { pauseNarration(n); fail('speak', 'speakFailed', err); } else logIssue('warn', 'speak', errText(err));
  }
  if (n.segments[n.index] === seg) resumeNarration(n);
  renderNarration(n);
  renderState();
}

// wavSeconds reads a clip's length from its WAV header, so the timeline knows
// it before the clip is ever loaded into a player.
async function wavSeconds(blob) {
  const head = new DataView(await blob.slice(0, 44).arrayBuffer());
  const byteRate = head.byteLength === 44 ? head.getUint32(28, true) : 0;
  if (!byteRate) throw new Error('the speech server did not return a WAV clip');
  return (blob.size - 44) / byteRate;
}

// ---- the player ------------------------------------------------------------

const shownNarration = () => (selected && selected.narration && selected.narration.open ? selected.narration : null);

// listenToAnswer is the listen button: it opens the player on the selected
// answer and starts it, or pauses it when it is already playing.
function listenToAnswer() {
  const turn = selected;
  if (!turn || !turn.answer) return;
  if (!cfg.voice) { flash(t('noVoice')); return; }
  if (!turn.narration) {
    const segments = narrationSegments(speakableBlocks(turn.answer.full));
    if (!segments.length) { warn('speak', 'playerEmpty'); return; }
    turn.narration = { turn, segments, index: 0, at: 0, audio: null, playing: false, open: false };
  }
  const n = turn.narration;
  n.open = true;
  if (n.playing) pauseNarration(n); else playNarration(n);
}

function closeNarration(n) {
  pauseNarration(n);
  unmountNarration(n);
  n.index = 0;
  n.at = 0;
  n.open = false;
  renderNarrator();
}

// narratorKey is what the arrow keys do while an answer is being listened to.
// It reports whether the key was used.
function narratorKey(key) {
  const n = shownNarration();
  if (!n || (key !== 'ArrowLeft' && key !== 'ArrowRight')) return false;
  skipNarration(n, key === 'ArrowLeft' ? -NARRATION_SKIP : NARRATION_SKIP);
  return true;
}

function renderNarration(n) {
  if (shownNarration() === n) renderNarrator();
}

// setHTML and setText touch the page only when something changed: the player
// is redrawn on every animation frame while it plays, and a button whose icon
// is replaced under the pointer can lose the click.
function setHTML(node, html) {
  if (node.narratorHTML === html) return; // innerHTML reads back reserialized, so compare what was set
  node.narratorHTML = html;
  node.innerHTML = html;
}

function setText(node, text) {
  if (node.textContent !== text) node.textContent = text;
}

function renderNarrator() {
  const turn = selected;
  listenBtn.hidden = !cfg.voice || !turn || !turn.answer;
  setHTML(listenBtn, ICONS.headphones);
  listenBtn.title = t('listenFull');
  listenBtn.classList.toggle('on', !!(turn && turn.narration && turn.narration.playing));
  const n = shownNarration();
  narrator.hidden = !n;
  if (!n) return;

  const spans = narrationSpans(n);
  const total = spansEnd(spans);
  const pos = dragAt === null ? narrationPosition(n, spans) : dragAt * total;
  const waiting = n.playing && !n.audio;
  const pct = (v) => `${total ? Math.min(100, (v / total) * 100) : 0}%`;

  const toggle = narrator.querySelector('.np-toggle');
  setHTML(toggle, waiting ? ICONS.spinner : n.playing ? ICONS.pause : ICONS.play);
  toggle.title = waiting ? t('loading') : n.playing ? t('pause') : t('playerPlay');
  toggle.classList.toggle('waiting', waiting);
  narrator.classList.toggle('playing', n.playing);
  narrator.classList.toggle('dragging', dragAt !== null);

  const back = narrator.querySelector('.np-back');
  setHTML(back, ICONS.back.replace('{n}', num(NARRATION_SKIP)));
  back.title = t('playerBack').replace('{n}', num(NARRATION_SKIP));
  const forward = narrator.querySelector('.np-forward');
  setHTML(forward, ICONS.forward.replace('{n}', num(NARRATION_SKIP)));
  forward.title = t('playerForward').replace('{n}', num(NARRATION_SKIP));

  setText(narrator.querySelector('.np-elapsed'), fmtTime(pos * 1000));
  setText(narrator.querySelector('.np-total'), (spans.every((s) => s.ready) ? '' : '~') + fmtTime(total * 1000));

  // the voiced stretch around what is playing, the way a player shows what it has buffered
  let from = n.index;
  let to = n.index;
  while (from > 0 && spans[from - 1].ready) from--;
  while (to < spans.length - 1 && spans[to + 1].ready) to++;
  const ready = narrator.querySelector('.np-ready');
  ready.hidden = !spans[n.index].ready;
  ready.style.left = pct(spans[from].start);
  ready.style.width = pct(spans[to].start + spans[to].len - spans[from].start);
  narrator.querySelector('.np-played').style.width = pct(pos);
  narrator.querySelector('.np-knob').style.left = pct(pos);
  npTrack.setAttribute('aria-valuenow', String(Math.round(pos)));
  npTrack.setAttribute('aria-valuemax', String(Math.round(total)));

  const rate = narrator.querySelector('.np-rate');
  setText(rate, `${num(narrationRate)}×`);
  rate.title = t('playerSpeed');
  const close = narrator.querySelector('.np-close');
  setHTML(close, ICONS.close);
  close.title = t('playerClose');

  const note = narrator.querySelector('.np-note');
  setText(note, t('clipVoicing'));
  note.dir = document.documentElement.dir; // the player itself is always left to right
  note.hidden = !waiting;
}

// tickNarration moves the timeline smoothly while something is playing.
function tickNarration() {
  if (narrationFrame) return;
  const step = () => {
    const n = player && player.narration;
    if (!n) { narrationFrame = 0; return; }
    renderNarration(n);
    narrationFrame = requestAnimationFrame(step);
  };
  narrationFrame = requestAnimationFrame(step);
}

// ---- wiring ----------------------------------------------------------------

listenBtn.addEventListener('click', listenToAnswer);

narrator.addEventListener('click', (e) => {
  const n = shownNarration();
  const button = e.target.closest('button');
  if (!n || !button) return;
  if (button.matches('.np-toggle')) {
    if (n.playing) pauseNarration(n); else playNarration(n);
  } else if (button.matches('.np-back')) skipNarration(n, -NARRATION_SKIP);
  else if (button.matches('.np-forward')) skipNarration(n, NARRATION_SKIP);
  else if (button.matches('.np-close')) closeNarration(n);
  else if (button.matches('.np-rate')) {
    narrationRate = NARRATION_RATES[(NARRATION_RATES.indexOf(narrationRate) + 1) % NARRATION_RATES.length];
    for (const turn of turns) {
      const audio = turn.narration && turn.narration.audio;
      if (audio) { audio.defaultPlaybackRate = narrationRate; audio.playbackRate = narrationRate; }
    }
    renderNarrator();
  }
});

const trackFraction = (e) => {
  const r = npTrack.getBoundingClientRect();
  return r.width ? Math.max(0, Math.min(1, (e.clientX - r.left) / r.width)) : 0;
};

npTrack.addEventListener('pointerdown', (e) => {
  if (!shownNarration()) return;
  npTrack.setPointerCapture(e.pointerId);
  dragAt = trackFraction(e);
  renderNarrator();
});

npTrack.addEventListener('pointermove', (e) => {
  if (dragAt === null) return;
  dragAt = trackFraction(e);
  renderNarrator();
});

npTrack.addEventListener('pointerup', (e) => {
  if (dragAt === null) return;
  const at = trackFraction(e);
  dragAt = null;
  const n = shownNarration();
  if (n) seekNarration(n, at * spansEnd(narrationSpans(n))); else renderNarrator();
});

npTrack.addEventListener('pointercancel', () => {
  dragAt = null;
  renderNarrator();
});
