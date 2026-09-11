// Speech to text, and what happens when it fails. A recording can be minutes
// of someone talking, and losing it to a dropped connection is the one failure
// they cannot simply repeat. The server already retries OpenRouter, so a
// failure that gets this far is a real one: the recording is kept, and the
// footer says what went wrong and offers to send it again.

const unsentEl = {
  box: document.getElementById('unsent'),
  text: document.querySelector('#unsent .unsent-text'),
  retry: document.querySelector('#unsent .unsent-retry'),
  discard: document.querySelector('#unsent .unsent-discard'),
};

// The message for each way a transcription can come back empty-handed.
const UNSENT_MESSAGES = {
  offline: 'unsentOffline', // nutshell itself did not answer
  unreachable: 'unsentUnreachable', // OpenRouter kept failing through every retry
  failed: 'unsentFailed', // anything else, shown with the error
  empty: 'unsentEmpty', // the transcript came back without a word in it
};

// unsent is the last recording whose transcription failed: { wav, reason, detail }.
let unsent = null;

// transcribe turns a recording into text and hands the text on, or keeps the
// recording when that fails.
async function transcribe(wav) {
  state = 'transcribing';
  renderState();
  let text;
  try {
    text = await requestTranscript(wav);
  } catch (err) {
    keepUnsent(wav, err.reason || 'failed', err);
    return;
  } finally {
    if (state === 'transcribing') { state = 'idle'; renderState(); }
  }
  if (!text) { keepUnsent(wav, 'empty'); return; }
  if (unsent && unsent.wav === wav) clearUnsent();
  if (settings.autoSend) {
    ask(text);
  } else {
    el.input.value = text;
    resizeInput();
    el.input.focus();
    flash(t('hintEdit'));
  }
}

// requestTranscript resolves with the text of a recording. It rejects with an
// error whose reason says which side failed.
async function requestTranscript(wav) {
  let resp;
  try {
    resp = await fetch('/api/transcribe', {
      method: 'POST', headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ audio: wav, format: 'wav', lang: settings.lang }),
    });
  } catch (err) { // fetch only rejects when nutshell itself is out of reach
    throw Object.assign(err, { reason: 'offline' });
  }
  if (!resp.ok) {
    const err = await httpError(resp);
    throw Object.assign(err, { reason: resp.status === 503 ? 'unreachable' : 'failed' });
  }
  const { text, cost_usd: cost } = await resp.json();
  pendingStt = cost || 0;
  return text;
}

function keepUnsent(wav, reason, err) {
  unsent = { wav, reason, detail: err ? errText(err) : '' };
  if (reason === 'empty') logIssue('warn', 'transcribe', 'the transcript has no words in it');
  else logIssue('error', 'transcribe', `transcription failed (${reason})`, unsent.detail);
  renderUnsent();
}

function clearUnsent() {
  unsent = null;
  renderUnsent();
}

// renderUnsent draws the kept recording's notice, in the current language.
// The footer hides it while anything else is going on there.
function renderUnsent() {
  unsentEl.box.hidden = !unsent;
  if (!unsent) return;
  unsentEl.text.textContent = t(UNSENT_MESSAGES[unsent.reason]).replace('{error}', unsent.detail);
  unsentEl.retry.textContent = t('unsentRetry');
  unsentEl.discard.textContent = t('unsentDiscard');
}

unsentEl.retry.addEventListener('click', () => {
  if (unsent && state === 'idle') transcribe(unsent.wav);
});
unsentEl.discard.addEventListener('click', clearUnsent);
