// The full answer as the narrator reads it. On screen the Markdown's layout
// tells the reader where a section starts; read aloud, that structure travels
// as line breaks: every block on a line of its own, and a blank line on either
// side of a heading. A line break also ends a sentence, so pcm.js never runs
// one block into the next inside a sentence. Whatever is read, the answer or a
// passage or a summary, goes through speakableText on its way to the voice.

// Characters in a segment. A segment is one clip of the player's timeline,
// voiced and kept in memory as a whole (pcm.js cuts it into the pieces that
// are actually asked for), so the size only bounds how much audio is held at
// once: most answers are one segment, and only a long one is split.
const NARRATION_SEGMENT = 10000;

// speakableBlocks is the full answer as the sentences worth hearing, block by
// block, each block led by the break that separates it from the one before:
// code blocks are left out, and every list item and table row becomes a
// sentence of its own. The Markdown is parsed into an inert document, so
// nothing in it loads or runs.
function speakableBlocks(markdown) {
  const doc = new DOMParser().parseFromString(marked.parse(markdown || ''), 'text/html');
  doc.querySelectorAll('code').forEach((code) => { if (!code.closest('pre')) code.textContent = spokenCode(code.textContent); });
  const blocks = [];
  let gap = ''; // the break owed before the next block
  const push = (text, heading) => {
    let s = text.replace(/\s+/g, ' ').trim();
    if (!s) return;
    if (!/[.!?؟…:;,،]$/.test(s)) s = `${s}.`;
    const lead = blocks.length ? (heading || gap === '\n\n' ? '\n\n' : '\n') : '';
    blocks.push(lead + s);
    gap = heading ? '\n\n' : '\n';
  };
  const walk = (parent) => {
    for (const node of parent.children) {
      if (node.matches('pre, hr')) continue;
      if (node.matches('ul, ol, blockquote')) { walk(node); continue; }
      if (node.matches('h1, h2, h3, h4, h5, h6')) { push(node.textContent, true); continue; }
      if (node.matches('table')) {
        for (const row of node.rows) {
          const cells = [...row.cells].map((c) => c.textContent.trim()).filter(Boolean);
          push(cells.join(isRTL(cells.join(' ')) ? '، ' : ', '));
        }
        continue;
      }
      if (node.matches('li')) {
        const own = node.cloneNode(true);
        own.querySelectorAll('ul, ol, pre').forEach((x) => x.remove());
        push(own.textContent);
        node.querySelectorAll(':scope > ul, :scope > ol').forEach(walk);
        continue;
      }
      push(node.textContent);
    }
  };
  walk(doc.body);
  return blocks;
}

// narrationSegments packs blocks into segments. A block is broken up only when
// it is too long for a segment by itself: at its sentences, and a sentence
// still too long at its words.
function narrationSegments(blocks) {
  const texts = [];
  let current = '';
  const add = (piece) => { // piece is led by what separates it from the text before
    if (current && current.length + piece.length > NARRATION_SEGMENT) { texts.push(current); current = ''; }
    current = current ? current + piece : piece.trimStart();
  };
  for (const block of blocks) {
    if (block.length <= NARRATION_SEGMENT) { add(block); continue; }
    const lead = block.match(/^\s*/)[0];
    sentences(block.trim())
      .flatMap((s) => (s.length > NARRATION_SEGMENT ? byWords(s, NARRATION_SEGMENT) : [s]))
      .forEach((s, i) => add((i ? ' ' : lead) + s));
  }
  if (current) texts.push(current);
  return texts.map((text) => ({ text, chars: text.length, status: 'idle', voice: null }));
}

// SENTENCE_END is the space after a sentence: a full stop, question or
// exclamation mark or ellipsis that follows a word (a closing quote or bracket
// may sit in between), or any line break. A mark standing on its own between
// spaces, as in "the marks . and ?", ends nothing.
const SENTENCE_END = /((?<=\S[.!?؟…]["'»”)\]]*)\s+|\s*\n\s*)/;

// sentences cuts text where one sentence ends and the next starts.
const sentences = (text) => text.split(SENTENCE_END).filter((_, i) => i % 2 === 0).filter((s) => s.trim());

// byWords breaks a sentence with nowhere better to break it into runs of at
// most limit characters.
function byWords(sentence, limit) {
  const out = [];
  let cur = '';
  for (const word of sentence.split(' ')) {
    if (cur && cur.length + 1 + word.length > limit) { out.push(cur); cur = ''; }
    cur = cur ? `${cur} ${word}` : word;
  }
  if (cur) out.push(cur);
  return out;
}

// ---- what is not worth hearing ---------------------------------------------

// URL_TEXT is a web address as prose writes it.
const URL_TEXT = /\b(?:https?:\/\/|www\.)[^\s<>"'«»]+/gi;
// PATH_TEXT is a run of parts in Latin letters joined by slashes or
// backslashes, perhaps rooted at /, ~/, ./, ../ or a drive. lastPart decides
// whether it is a path at all.
const PATH_TEXT = /(?<![\w./\\-])(?:[A-Za-z]:\\|~\/|\.{1,2}\/|\/)?[\w.@+~-]+(?:[/\\][\w.@+~-]+)+[/\\]?/g;
// CODE_PATH is inline code that is nothing but a path, spaces allowed after
// its first part (`~/Desktop/folder 1/notes.md`) and nothing a shell command
// would hold.
const CODE_PATH = /^(?:[A-Za-z]:\\|~\/|\.{1,2}\/|\/)?[^\s/\\&|;$<>=`'"]+(?:[/\\][^/\\&|;$<>=`'"]+)+[/\\]?$/;
const PATH_ROOT = /^(?:[A-Za-z]:\\|~\/|\.{1,2}\/|\/)/;

// speakableText is text as it is worth saying: a web address is read as its
// host (github.com) and a file path as its last part (pcm.js), because what
// comes before them is a string of letters nobody listens to.
function speakableText(text) {
  return text.replace(URL_TEXT, hostOf).replace(PATH_TEXT, lastPart);
}

// spokenCode is inline code as it is read: a path, spaces and all, as its last
// part; anything else as speakableText has it.
function spokenCode(code) {
  const s = code.trim();
  return (CODE_PATH.test(s) && pathTail(s)) || speakableText(s);
}

function hostOf(url) {
  const trail = url.match(/[.,;:!?؟،)\]»"']*$/)[0];
  const bare = url.slice(0, url.length - trail.length);
  try {
    return new URL(/^www\./i.test(bare) ? `http://${bare}` : bare).hostname.replace(/^www\./, '') + trail;
  } catch {
    return url;
  }
}

function lastPart(found) {
  const trail = found.match(/\.*$/)[0]; // the full stop of the sentence it ends
  return (pathTail(found.slice(0, found.length - trail.length)) || found.slice(0, found.length - trail.length)) + trail;
}

// pathTail is what of a path is read — the host of one that starts with a
// domain (github.com/mhrlife/nutshell), else its last part — or null when it
// is not a path but words that share a slash: a path has letters, and is
// rooted, ends in a file name or runs three parts deep. and/or, TCP/IP,
// 2026/09/13 and google/gemini-2.5-flash stay as they are.
function pathTail(path) {
  const parts = path.split(/[/\\]/).filter(Boolean);
  const last = parts[parts.length - 1];
  const rooted = PATH_ROOT.test(path);
  if (!/[A-Za-z]/.test(path) || parts.length < 2) return null;
  if (!rooted && /^[\w-]+(?:\.[\w-]+)*\.[a-z]{2,6}$/i.test(parts[0]) && !/\.[A-Za-z]\w*$/.test(last)) return parts[0];
  if (!(rooted || /\.[A-Za-z]\w*$/.test(last) || parts.length >= 3)) return null;
  return last;
}
