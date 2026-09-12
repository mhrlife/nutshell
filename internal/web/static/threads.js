// A question that comes out of an answer — what is that term, why that
// library — can be asked without dragging the conversation through it: it
// opens a side thread, which knows everything said so far and keeps what
// follows to itself. Ask, get somewhere, say you are done, and you are back
// where you were, with the conclusion brought along if you wanted it.
//
// The tree that makes is the server's business. Here there is only ever one
// thread on screen, a trail at the top saying how deep you are, and a line in
// the thread above marking where a side thread was opened.

const ROOT = 'root';

const transcript = document.getElementById('transcript');
const emptyHint = document.getElementById('empty');
const trail = document.getElementById('trail');
const threadBar = document.getElementById('thread-bar');

// Every thread the page knows about, by id. The marker is the line drawn in
// the thread above, which is also the way back into this one.
const threadList = new Map([[ROOT, { id: ROOT, parent: null, title: '', closed: false, marker: null }]]);

let current = ROOT; // the thread on screen
let forkNext = false; // the next question opens a side thread instead of being asked here
let lastSeen = ROOT; // where the conversation was when the backlog ended

function currentThread() { return threadList.get(current); }

function inSideThread() { return current !== ROOT; }

// inClosedThread is true while a finished thread is being read. Everything it
// holds stays on screen; nothing more can be asked of it.
function inClosedThread() { return !!currentThread().closed; }

// pathTo is the trail from the main thread down to id.
function pathTo(id) {
  const path = [];
  for (let node = threadList.get(id); node; node = threadList.get(node.parent)) path.unshift(node);
  return path;
}

// ---- what the stream says --------------------------------------------------

// openThread draws the line marking where a side thread was opened. It
// belongs to the thread above, which is the one the user was in at the time.
function openThread(parent, data) {
  if (threadList.has(data.id)) return;
  const node = document.getElementById('thread-template').content.firstElementChild.cloneNode(true);
  const thread = { id: data.id, parent, title: data.title || '', closed: false, marker: node };
  threadList.set(thread.id, thread);
  node.dataset.thread = parent;
  node.dataset.id = thread.id;
  renderMarker(thread);
  transcript.appendChild(node);
  applyThreads();
  if (synced) enterThread(thread.id); // this page, or another tab, just opened it
  else lastSeen = thread.id;
}

// finishThread records that a side thread is over, and what came of it.
function finishThread(parent, data) {
  const thread = threadList.get(data.id);
  if (!thread) { logIssue('warn', 'thread', `a thread this page never saw finished (${data.id})`); return; }
  thread.closed = true;
  thread.conclusion = data.conclusion || '';
  renderMarker(thread);
  if (synced) { if (current === thread.id) enterThread(parent); }
  else if (lastSeen === thread.id) lastSeen = parent;
}

// noteThread follows where the conversation went while the backlog replays,
// so a page that reloads comes back where it left off.
function noteThread(id) { if (!synced) lastSeen = id || ROOT; }

// resumeThread is called once the backlog is on screen.
function resumeThread() {
  const thread = threadList.get(lastSeen);
  enterThread(thread && !thread.closed ? lastSeen : (thread ? thread.parent : ROOT) || ROOT);
}

// ---- moving between threads ------------------------------------------------

function enterThread(id) {
  if (!threadList.has(id)) return;
  current = id;
  stopPlayback();
  applyThreads();
  renderTrail();
  renderThreadBar();
  selectLastTurn();
  scrollToEnd();
  // The footer belongs to the thread you are in — a finished one takes no
  // more questions, the one above it does — so it is redrawn on every move.
  renderState();
}

// applyThreads hides everything that belongs to another thread. The
// transcript holds every turn of every thread in the order they happened, so
// showing one thread is a matter of what is on screen, never of what is kept.
function applyThreads() {
  for (const node of transcript.querySelectorAll('[data-thread]')) {
    node.hidden = node.dataset.thread !== current;
  }
  emptyHint.hidden = !!transcript.querySelector('[data-thread]:not([hidden])');
}

// selectLastTurn puts the newest answer of this thread in the document pane.
function selectLastTurn() {
  const here = turns.filter((x) => x.thread === current);
  const answered = here.filter((x) => x.answer);
  select(answered[answered.length - 1] || here[here.length - 1] || null);
}

// ---- opening and finishing -------------------------------------------------

// setFork arms (or disarms) the next question: with it on, the question opens
// a side thread instead of being asked here.
function setFork(on) {
  forkNext = !!on && !currentThread().closed;
  renderForkButton();
  renderState();
}

function toggleFork() { setFork(!forkNext); }

// forking is what ask() sends, and it is spent on the question that uses it.
function forking() { return forkNext; }

function spendFork() { setFork(false); }

// finishHere closes the thread on screen. With inject the agent is asked what
// the thread settled first, and that answer is the only thing the thread
// above ever hears about it.
async function finishHere(inject) {
  if (!inSideThread()) return;
  const thread = currentThread();
  if (thread.closed) { enterThread(thread.parent); return; }
  if (inject && state === 'working') { warn('close', 'stillWorking'); return; }
  setFork(false);
  try {
    await postJSON('/api/close', { thread: thread.id, inject: !!inject, lang: settings.lang });
  } catch (err) {
    fail('close', 'closeFailed', err);
    return;
  }
  if (!inject) return; // closing without a conclusion is done the moment it is recorded
  renderThreadBar(); // the closing turn is running; the way up opens when it lands
}

// ---- drawing ---------------------------------------------------------------

function renderMarker(thread) {
  const node = thread.marker;
  if (!node) return;
  node.classList.toggle('closed', thread.closed);
  node.querySelector('.side-label').textContent = thread.closed ? t('threadClosed') : t('threadOpen');
  const title = node.querySelector('.side-title');
  title.textContent = oneLine(thread.title) || t('threadUntitled');
  title.dir = isRTL(thread.title) ? 'rtl' : 'ltr';
  const note = node.querySelector('.side-note');
  note.textContent = thread.conclusion || '';
  note.dir = isRTL(thread.conclusion) ? 'rtl' : 'ltr';
  note.hidden = !thread.conclusion;
}

// crumbLabel keeps the trail readable at a glance: a few words of what the
// thread is about, never the whole question it started with.
function crumbLabel(thread) {
  if (thread.id === ROOT) return t('threadMain');
  const title = oneLine(thread.title);
  if (!title) return t('threadUntitled');
  return title.length > 36 ? `${title.slice(0, 36)}…` : title;
}

function renderTrail() {
  const path = pathTo(current);
  trail.hidden = path.length < 2;
  if (trail.hidden) return;
  trail.innerHTML = '';
  path.forEach((thread, i) => {
    if (i) trail.appendChild(document.createElement('i')).className = 'trail-sep';
    const crumb = document.createElement('button');
    crumb.type = 'button';
    crumb.className = 'crumb';
    crumb.dataset.id = thread.id;
    crumb.textContent = crumbLabel(thread);
    crumb.dir = thread.id === ROOT ? (document.documentElement.dir || 'ltr') : (isRTL(thread.title) ? 'rtl' : 'ltr');
    crumb.disabled = i === path.length - 1;
    trail.appendChild(crumb);
  });
}

function renderThreadBar() {
  const thread = currentThread();
  threadBar.hidden = !inSideThread();
  if (threadBar.hidden) return;
  const closed = thread.closed;
  threadBar.querySelector('.thread-note').textContent = closed ? t('threadIsClosed') : t('threadHere');
  const done = threadBar.querySelector('.thread-done');
  const drop = threadBar.querySelector('.thread-drop');
  done.textContent = closed ? t('threadBack') : t('threadDone');
  done.title = closed ? '' : t('threadDoneNote');
  drop.hidden = closed;
  drop.textContent = t('threadDrop');
  drop.title = t('threadDropNote');
}

function renderForkButton() {
  el.fork.innerHTML = ICONS.branch;
  el.fork.classList.toggle('on', forkNext);
  el.fork.title = forkNext ? t('forkOnNote') : t('forkNote');
}

// renderThreads redraws everything here, and is what the language switch calls.
function renderThreads() {
  for (const thread of threadList.values()) renderMarker(thread);
  renderTrail();
  renderThreadBar();
  renderForkButton();
}

// ---- wiring ----------------------------------------------------------------

trail.addEventListener('click', (e) => {
  const crumb = e.target.closest('.crumb');
  if (crumb) enterThread(crumb.dataset.id);
});

threadBar.addEventListener('click', (e) => {
  if (e.target.closest('.thread-done')) finishHere(true);
  else if (e.target.closest('.thread-drop')) finishHere(false);
});

document.getElementById('fork').addEventListener('click', toggleFork);

transcript.addEventListener('click', (e) => {
  const marker = e.target.closest('.side');
  if (marker) enterThread(marker.dataset.id);
});
