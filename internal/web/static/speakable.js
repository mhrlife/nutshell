// The full answer as the narrator reads it. On screen the Markdown's layout
// tells the reader where a section starts and what matters; read aloud, that
// structure travels as speech tags x-ai/grok-voice-tts-1.0 performs: a pause
// between blocks, a longer one before a heading, and emphasis on a heading and
// on a short bold phrase. /api/speak strips the tags for a voice that would
// read them out.

const NARRATION_FIRST = 400; // characters in the first segment
const NARRATION_SEGMENT = 1400; // characters in every later one
const EMPHASIS_WORDS = 5; // bold text longer than this is read plainly

const SPEECH_TAGS = /\[(?:pause|long-pause)\]|<\/?emphasis>/g;

// untagged is text as the voice says it, without the tags.
const untagged = (text) => text.replace(SPEECH_TAGS, '');

// spokenText is a block's text with its short bold phrases emphasized.
function spokenText(node) {
  let out = '';
  for (const child of node.childNodes) {
    if (child.nodeType === Node.TEXT_NODE) out += child.textContent;
    if (child.nodeType !== Node.ELEMENT_NODE) continue;
    const inner = child.textContent.trim();
    const short = inner && inner.split(/\s+/).length <= EMPHASIS_WORDS;
    if (child.matches('strong, b') && short) out += `<emphasis>${inner}</emphasis>`;
    else out += spokenText(child);
  }
  return out;
}

// speakableBlocks is the full answer as the sentences worth hearing, block by
// block: code blocks are left out, and every list item and table row becomes
// a sentence of its own. The Markdown is parsed into an inert document, so
// nothing in it loads or runs.
function speakableBlocks(markdown) {
  const doc = new DOMParser().parseFromString(marked.parse(markdown || ''), 'text/html');
  const blocks = [];
  let gap = ''; // the pause owed before the next block
  const push = (text) => {
    let s = text.replace(/\s+/g, ' ').trim();
    const said = untagged(s).trim();
    if (!said) return;
    if (!/[.!?؟…:;,،]$/.test(said)) s = `${s}.`;
    blocks.push(gap ? `${gap} ${s}` : s);
    gap = '';
  };
  const walk = (parent) => {
    for (const node of parent.children) {
      if (parent === doc.body && blocks.length) gap = node.matches('h1, h2, h3, h4, h5, h6') ? '[long-pause]' : '[pause]';
      if (node.matches('pre, hr')) continue;
      if (node.matches('ul, ol, blockquote')) { walk(node); continue; }
      if (node.matches('h1, h2, h3, h4, h5, h6')) {
        const title = node.textContent.replace(/\s+/g, ' ').trim();
        if (title) push(`<emphasis>${title}</emphasis>`);
        continue;
      }
      if (node.matches('table')) {
        for (const row of node.rows) {
          const cells = [...row.cells].map((c) => spokenText(c).trim()).filter(Boolean);
          push(cells.join(isRTL(untagged(cells.join(' '))) ? '، ' : ', '));
        }
        continue;
      }
      if (node.matches('li')) {
        const own = node.cloneNode(true);
        own.querySelectorAll('ul, ol, pre').forEach((x) => x.remove());
        push(spokenText(own));
        node.querySelectorAll(':scope > ul, :scope > ol').forEach(walk);
        continue;
      }
      push(spokenText(node));
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
  const limit = () => (texts.length ? NARRATION_SEGMENT : NARRATION_FIRST);
  const add = (piece) => {
    if (current && current.length + 1 + piece.length > limit()) { texts.push(current); current = ''; }
    current = current ? `${current} ${piece}` : piece;
  };
  for (const block of blocks) {
    const sentences = block.length > limit() ? block.split(/(?<=[.!?؟…])\s+/) : [block];
    sentences.flatMap((s) => (s.length > NARRATION_SEGMENT ? byWords(s) : [s])).forEach(add);
  }
  if (current) texts.push(current);
  return balanceEmphasis(texts).map((text) => ({ text, chars: untagged(text).length, status: 'idle', url: null, duration: 0 }));
}

function byWords(sentence) {
  const out = [];
  let cur = '';
  for (const word of sentence.split(' ')) {
    if (cur && cur.length + 1 + word.length > NARRATION_SEGMENT) { out.push(cur); cur = ''; }
    cur = cur ? `${cur} ${word}` : word;
  }
  if (cur) out.push(cur);
  return out;
}

// balanceEmphasis closes an emphasis that a split left open at the end of one
// segment and opens it again at the start of the next: every segment is its
// own speech request, and a tag left open would be read out.
function balanceEmphasis(texts) {
  let open = false;
  return texts.map((text) => {
    const s = open ? `<emphasis>${text}` : text;
    const tags = s.match(/<\/?emphasis>/g) || [];
    open = tags[tags.length - 1] === '<emphasis>';
    return open ? `${s}</emphasis>` : s;
  });
}
