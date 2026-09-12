// Selecting part of a full answer offers three things to do with it: hear it
// read aloud, hear a short summary of it, or ask the agent about it. Asking
// works like replying to a message in a chat app: the start of the passage
// sits above the input until the question is sent, and the question goes out
// with the passage attached.

const selBar = document.getElementById('selection-bar');
const quoteBox = document.getElementById('quote');

let quote = ''; // the passage the next question is about
let selTimer = null;
let pointerDown = false;

// selectedPassage is the text selected inside the full answer, or '' when
// nothing is, or the selection lies somewhere else on the page.
function selectedPassage() {
  const sel = window.getSelection();
  if (!sel || sel.isCollapsed || !sel.rangeCount) return '';
  if (!el.docBody.contains(sel.getRangeAt(0).commonAncestorContainer)) return '';
  return sel.toString().trim();
}

// placeSelectionBar shows the actions just above the selection, or below it
// when there is no room above, and hides them once nothing is selected.
function placeSelectionBar() {
  if (pointerDown || !selectedPassage()) { selBar.hidden = true; return; }
  renderSelectionBar();
  selBar.hidden = false;

  const rect = window.getSelection().getRangeAt(0).getBoundingClientRect();
  const bar = selBar.getBoundingClientRect();
  const gap = 8;
  const top = rect.top - bar.height - gap < gap ? rect.bottom + gap : rect.top - bar.height - gap;
  const left = Math.min(Math.max(rect.left + (rect.width - bar.width) / 2, gap), window.innerWidth - bar.width - gap);
  selBar.style.top = `${top}px`;
  selBar.style.left = `${left}px`;
}

function renderSelectionBar() {
  const labels = { read: t('selRead'), summary: t('selSummary'), ask: t('selAsk'), fork: t('selFork') };
  selBar.querySelectorAll('[data-act]').forEach((button) => {
    button.textContent = labels[button.dataset.act];
    if (button.dataset.act === 'read' || button.dataset.act === 'summary') button.hidden = !cfg.voice;
  });
}

function hideSelection() {
  window.getSelection().removeAllRanges();
  selBar.hidden = true;
}

// dropSelection is what Esc does here: close the actions first, then the
// quote. It reports whether there was anything to drop.
function dropSelection() {
  if (!selBar.hidden) { hideSelection(); return true; }
  if (quote) { setQuote(''); return true; }
  return false;
}

// ---- asking about a passage ------------------------------------------------

function setQuote(passage) {
  quote = passage;
  const text = quoteBox.querySelector('.quote-text');
  text.textContent = oneLine(passage);
  text.dir = isRTL(passage) ? 'rtl' : 'ltr';
  quoteBox.querySelector('.quote-clear').innerHTML = ICONS.close;
  quoteBox.querySelector('.quote-clear').title = t('quoteClear');
  quoteBox.hidden = !passage;
}

// oneLine is the start of a passage as one line; CSS adds the ellipsis.
function oneLine(passage) {
  return (passage || '').replace(/\s+/g, ' ').trim().slice(0, 240);
}

// ---- wiring ----------------------------------------------------------------

// Wait for the mouse to let go before offering anything: a bar that jumps
// around under a selection still being dragged is in the way.
document.addEventListener('selectionchange', () => {
  clearTimeout(selTimer);
  selTimer = setTimeout(placeSelectionBar, 150);
});

document.addEventListener('pointerdown', (e) => {
  if (selBar.contains(e.target)) return;
  pointerDown = true;
  selBar.hidden = true;
});

// A long press that turns into a selection on a touch screen ends in
// pointercancel rather than pointerup.
['pointerup', 'pointercancel'].forEach((type) => document.addEventListener(type, () => {
  pointerDown = false;
  clearTimeout(selTimer);
  selTimer = setTimeout(placeSelectionBar, 10);
}));

document.getElementById('doc').addEventListener('scroll', () => { if (!selBar.hidden) placeSelectionBar(); });
window.addEventListener('resize', () => { if (!selBar.hidden) placeSelectionBar(); });

// Pressing a button must not take the selection away before it is read.
selBar.addEventListener('mousedown', (e) => e.preventDefault());

selBar.addEventListener('click', (e) => {
  const button = e.target.closest('[data-act]');
  const passage = selectedPassage();
  if (!button || !passage) return;
  const act = button.dataset.act;
  const turn = selected;
  hideSelection();
  if (act === 'read' || act === 'summary') { speakPassage(turn, passage, act === 'summary'); return; }
  setQuote(passage);
  setFork(act === 'fork'); // "ask about this" stays here; "ask separately" opens a side thread
  closeDoc(); // on a narrow screen the answer covers the input
});

quoteBox.querySelector('.quote-clear').addEventListener('click', () => setQuote(''));
