// A clip is asked for piece by piece as it is heard, not all at once. A piece
// is paid for the moment it is voiced, heard or not, so a clip stopped a
// minute in must not have voiced the rest of the answer. A piece is asked for
// only once the audio in hand, counted from the playhead, would run out before
// that piece could be voiced; pieces asked for but not yet here count at the
// pace heard so far. While a clip is paused nothing more is asked for. The
// pieces at the start of a clip are short and little is in hand, so they
// still go out together and the first words come no later than before.

// Seconds a character takes to say, until a clip's own pieces tell.
const SPEECH_PER_CHAR = 0.07;
// Seconds a piece takes to voice: a fixed part and a part per character (see
// pcm.js), scaled by how long this clip's pieces actually took.
const SPEECH_GEN_BASE = 2;
const SPEECH_GEN_PER_CHAR = 0.035;
const SPEECH_MARGIN = 5; // seconds of slack on top, for a slow request
const SPEECH_RECHECK = 250; // ms, the soonest a playing clip looks again

class Feed {
  constructor(pieces, onGeneration) {
    this.pieces = pieces;
    this.onGeneration = onGeneration; // told what each piece was billed as
    this.voice = null; // set once the first piece has told its sample rate
    this.slots = []; // per piece: the promise of it and how to settle that
    this.next = 0; // the first piece not asked for yet
    this.poured = 0; // pieces laid into the voice in full
    this.pouredChars = 0;
    this.pouredSecs = 0;
    this.guessed = 0; // seconds the pieces voiced so far were expected to take
    this.took = 0; // and what they did take
    this.timer = 0;
  }

  slot(i) {
    if (!this.slots[i]) {
      const s = {};
      s.promise = new Promise((resolve, reject) => { s.resolve = resolve; s.reject = reject; });
      s.promise.catch(() => {}); // it is answered for where it is awaited
      this.slots[i] = s;
    }
    return this.slots[i];
  }

  // check asks for every piece needed by now and, while the clip plays, looks
  // again when the next one will be.
  check() {
    clearTimeout(this.timer);
    this.timer = 0;
    const voice = this.voice;
    if (voice && voice.held) return;
    while (this.next < this.pieces.length && this.ahead() < this.lead(this.next)) this.ask(this.next++);
    if (this.next < this.pieces.length && voice && voice.playing) {
      const wait = (this.ahead() - this.lead(this.next)) * 1000;
      this.timer = setTimeout(() => this.check(), Math.max(SPEECH_RECHECK, wait));
    }
  }

  // ahead is how many seconds of listening the pieces asked for hold beyond
  // the playhead, at the speed the clip plays.
  ahead() {
    const voice = this.voice;
    const pace = this.pouredChars ? this.pouredSecs / this.pouredChars : SPEECH_PER_CHAR;
    let chars = 0;
    for (let i = this.poured; i < this.next; i++) chars += this.pieces[i].length;
    if (!voice) return chars * pace;
    const arriving = voice.duration - this.pouredSecs; // of the piece being poured
    const pending = Math.max(0, chars * pace - arriving);
    return (voice.duration - voice.currentTime + pending) / voice.playbackRate;
  }

  // lead is how far ahead of need piece i is asked for: the time it takes to
  // voice, and the slack.
  lead(i) {
    const guess = SPEECH_GEN_BASE + this.pieces[i].length * SPEECH_GEN_PER_CHAR;
    return guess * (this.guessed ? this.took / this.guessed : 1) + SPEECH_MARGIN;
  }

  ask(i) {
    const slot = this.slot(i);
    const text = this.pieces[i];
    const started = performance.now();
    askPiece(text, i === 0).then((piece) => {
      piece.started = started;
      if (!piece.reader) this.learn(text, started);
      slot.resolve(piece);
    }, slot.reject);
  }

  learn(text, started) {
    this.guessed += SPEECH_GEN_BASE + text.length * SPEECH_GEN_PER_CHAR;
    this.took += (performance.now() - started) / 1000;
  }

  // pour lays the pieces into the voice in the order they are spoken, each as
  // soon as it is here: a piece that comes back early waits its turn. It is
  // left to run on its own — the caller already has the clip and plays what is
  // in it.
  async pour() {
    const voice = this.voice;
    try {
      for (let i = 0; i < this.pieces.length; i++) {
        const piece = await this.slot(i).promise;
        if (this.onGeneration) this.onGeneration(piece.generation);
        if (piece.reader) {
          await pourPiece(voice, piece.reader);
          this.learn(this.pieces[i], piece.started);
        } else voice.append(piece.samples);
        voice.seam();
        this.poured = i + 1;
        this.pouredChars += this.pieces[i].length;
        this.pouredSecs = voice.duration;
        this.check();
      }
      voice.finish();
    } catch (err) {
      voice.fail(err); // the words that did arrive still play
    }
  }
}

// speakClip has text read aloud and comes back with the clip as soon as its
// first piece is on its way, the rest following it in as they are needed.
// onGeneration is told what each piece was billed as, once per piece.
async function speakClip(text, onGeneration) {
  const feed = new Feed(speechPieces(speakableText(text)), onGeneration);
  feed.check(); // the first piece, and the short ones right behind it
  const first = await feed.slot(0).promise; // a refusal is the caller's to report
  const voice = new Voice(first.rate);
  voice.onfail = (err) => logIssue('warn', 'speak', errText(err)); // a clip that stops early
  voice.feed = feed;
  feed.voice = voice;
  feed.pour();

  return voice;
}

// askPiece has one piece read. The first piece is taken as it arrives,
// because it is the one holding everything up; the ones behind it are read
// out in full, so that the request is finished with and its connection freed
// while their samples wait their turn in memory. The browser only keeps a
// handful of connections to one origin, and the event stream has one of them
// for good.
async function askPiece(text, next) {
  const resp = await fetch('/api/speak', {
    method: 'POST', headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ text, lang: settings.lang }),
  });
  if (!resp.ok) throw await httpError(resp);

  const piece = {
    rate: Number(resp.headers.get('X-Sample-Rate')),
    generation: resp.headers.get('X-Generation-Id'),
  };
  if (next) piece.reader = resp.body.getReader();
  else piece.samples = new Uint8Array(await resp.arrayBuffer());

  return piece;
}

async function pourPiece(voice, reader) {
  for (;;) {
    const { value, done } = await reader.read();
    if (done) return;
    voice.append(value);
  }
}
