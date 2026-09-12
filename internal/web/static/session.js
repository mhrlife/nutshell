// The conversation lives on the server; this page is only a view of it.
// /api/stream replays everything that has happened and then follows along, so
// reloading — even in the middle of a turn — redraws the screen exactly as it
// was. A dropped connection repairs itself: the browser reconnects and tells
// the server the last event it saw, so nothing is missed and nothing repeats.

let synced = false; // false while the first backlog is still replaying
let offline = false; // the stream is down and trying to come back
let running = null; // the turn the agent is working on, if any
let startedAt = 0;
let workTimer = null;

function connect() {
  const stream = new EventSource('/api/stream');
  stream.onmessage = (e) => {
    try {
      apply(JSON.parse(e.data));
    } catch (err) { // a malformed entry must not stop the rest of the stream
      logIssue('error', 'stream', errText(err), e.data);
    }
  };
  stream.addEventListener('synced', () => {
    synced = true;
    resumeThread(); // back to the thread the conversation was in when it stopped
    if (offline) { offline = false; renderState(); }
  });
  stream.onerror = () => {
    if (offline) return;
    offline = true;
    logIssue('warn', 'stream', 'event stream dropped, reconnecting');
    renderState();
  };
}

// apply draws one thing that happened. Replayed and live events take the same
// path, so a page that just reloaded cannot drift from one that never did.
function apply(entry) {
  const { turn: id, kind, data, thread } = entry;
  if (kind === 'thread') { openThread(thread, data); return; }
  if (kind === 'thread_done') { finishThread(thread, data); return; }

  if (kind === 'question') {
    noteThread(thread);
    addTurn(id, thread, data);
    startWork(id, data.started);

    return;
  }

  const turn = turns.find((x) => x.id === id);
  if (!turn) { // the page and the server disagree about what exists
    logIssue('warn', 'stream', `entry for a turn this page never saw (turn ${id}, ${kind})`);
    return;
  }

  if (kind === 'tool') showActivity(turn, data.name || 'tool', data.detail || '');
  else if (kind === 'text') showActivity(turn, 'think', data.text);
  else if (kind === 'prompt') showPrompt(data, (promptId, choices) => answerPrompt(turn, data, promptId, choices));
  else if (kind === 'prompt_done') closePrompt(data.id);
  else if (kind === 'result') { finishTurn(turn, data); stopWork(id); }
  else if (kind === 'error') { failTurn(turn, data.code === 'cancelled' ? t('cancelled') : data.message); stopWork(id); }
}

// startWork shows the turn in progress — a new one, or the one a reload
// landed in the middle of, which is why the clock starts from the server's
// own timestamp rather than from now.
function startWork(id, started) {
  running = id;
  startedAt = started;
  state = 'working';
  renderState();
  clearInterval(workTimer);
  tickWork();
  workTimer = setInterval(tickWork, 1000);
}

function stopWork(id) {
  if (running !== id) return;
  running = null;
  clearInterval(workTimer);
  state = 'idle';
  renderState();
}

function tickWork() {
  if (promptPending()) { renderState(); return; }
  const elapsed = fmtTime(Date.now() - startedAt);
  setStatus(`${t('working')} ${elapsed}`, true);
  setFootNote(t('workingNote').replace('{time}', elapsed));
}

// ask sends a question, together with the passage it is about when one is
// quoted. It goes to the thread on screen, unless the side-thread button is
// armed, in which case it opens one. The turn it starts comes back on the
// stream like any other, so this tab draws it exactly the way a second tab
// would — and so does the side thread, if one was opened.
async function ask(question) {
  question = question.trim();
  if (!question) return; // an empty box is not a failure, just nothing to send
  if (state === 'working') { warn('ask', 'stillWorking'); return; }
  if (inClosedThread()) { warn('ask', 'threadIsClosed'); return; }
  stopPlayback();
  const about = quote;
  const fork = forking();
  el.input.value = '';
  setQuote('');
  spendFork();
  resizeInput();
  try {
    const resp = await fetch('/api/ask', {
      method: 'POST', headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ text: question, selection: about, lang: settings.lang, thread: current, fork }),
    });
    if (!resp.ok) throw await httpError(resp);
  } catch (err) { // the question never reached the agent: put it back as it was
    el.input.value = question;
    setQuote(about);
    setFork(fork);
    resizeInput();
    fail('ask', 'askFailed', err);
  }
}

// answerPrompt sends the user's decision back and leaves a line about it in
// the transcript, so the turn still reads as a record of what happened.
async function answerPrompt(turn, prompt, id, choices) {
  showActivity(turn, prompt.kind === 'choice' ? t('promptQuestion') : t('promptPermission'), promptSummary(choices));
  try {
    const resp = await fetch('/api/answer', {
      method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ id, choices }),
    });
    if (!resp.ok) throw await httpError(resp);
  } catch (err) { // usually the prompt was withdrawn while we answered it
    fail('answer', 'answerFailed', err);
  }
}
