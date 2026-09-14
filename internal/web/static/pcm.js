// Speech is played as it is spoken. A clip is not one speech request but a
// row of small ones, stitched back together: the Gemini voice says nothing
// until it has generated nearly everything, so the silence before the first
// word grows with the length of the passage — about two seconds plus three or
// four hundredths of a second a character, and streaming the reply does not
// change that. A seven-hundred-character answer asked for in one piece is
// twenty seconds of silence; with a short first piece, the first words arrive
// in a few seconds, and the pieces behind it arrive while it is being heard,
// each asked for only when it is about to be needed (see feed.js).
// So a Voice takes the pieces in the order they are spoken and lays them end
// to end on the Web Audio clock, and what the players in app.js, clips.js and
// narrator.js hold is one clip that grows while it plays — play, pause,
// currentTime, playbackRate, onended, as an <audio> element would.
//
// Each piece of the stream becomes a buffer that starts exactly where the one
// before it ended, which is what keeps the seams inaudible. When nothing is
// there to play the playhead runs dry; the next piece is then picked up from
// where the clock is now rather than played late, which is what .waiting says.

const PCM_RATE = 24000; // what the server sends, when it does not say
const PCM_LEAD = 0.08; // seconds of headroom taken when playback (re)starts
const PCM_FULL_SCALE = 0x8000; // a 16-bit sample at full deflection

// Characters in a piece. The voice starts afresh at every piece, and its tone
// with it, so a piece is only ever cut where a sentence ends, and where a
// paragraph ends when one is near. The first piece is kept short, because
// nothing at all can be heard until it is here: sentences are gathered until
// they reach its min. Every piece after it is filled for as long as it can be
// voiced before the pieces ahead of it have been heard: a voice takes about
// half as long to generate a character as to say it, so a piece may hold as
// much as all the pieces before it together plus one first piece, which holds
// even at double speed — 100, 200, 400 characters, and no more than
// SPEECH_LONGEST. The cap is low because a piece is voiced, and paid for, in
// full once asked for: a clip stopped part way has voiced at most one piece
// nobody hears. A last piece shorter than SPEECH_TAIL goes with the one before
// it.
const SPEECH_FIRST = { min: 30, max: 100 };
const SPEECH_LONGEST = 400;
const SPEECH_TAIL = 70;

let voiceCtx = null;

// voiceContext is the one output every clip plays through. It is asked for the
// sample rate of the audio itself so nothing is resampled: buffers scheduled
// back to back then line up to the sample.
function voiceContext(rate) {
  if (!voiceCtx) {
    try { voiceCtx = new AudioContext({ sampleRate: rate }); } catch { voiceCtx = new AudioContext(); }
  }
  return voiceCtx;
}

class Voice {
  constructor(rate) {
    this.rate = rate || PCM_RATE;
    this.samples = new Int16Array(this.rate * 8); // grows with the clip
    this.received = 0; // samples held
    this.spare = -1; // the odd byte of a sample split across two pieces
    this.complete = false;
    this.error = null;

    this.playing = false;
    this.at = 0; // the playhead, in samples, while nothing is playing
    this.sent = 0; // samples handed to the output
    this.anchorSample = 0; // where the output is at anchorTime
    this.anchorTime = 0; // on the audio clock
    this.sources = new Set(); // scheduled, not yet played out
    this.speed = 1;
    this.held = false; // paused by the listener: nothing more is asked for
    this.feed = null; // what asks for the pieces still to come (feed.js)

    this.onplay = null;
    this.onpause = null;
    this.onended = null;
    this.onprogress = null; // more of the clip arrived, or it is all there
    this.onfail = null; // the stream broke off part way
  }

  // ---- the stream ----------------------------------------------------------

  // append takes the next bytes: whole little-endian 16-bit samples, and at
  // most one byte of a sample the network split in two.
  append(bytes) {
    if (!bytes.length) return;
    this.grow(((this.spare >= 0 ? 1 : 0) + bytes.length) >> 1);
    let out = this.received;
    let i = 0;
    if (this.spare >= 0) {
      this.samples[out++] = this.spare | (bytes[0] << 8);
      this.spare = -1;
      i = 1;
    }
    for (; i + 1 < bytes.length; i += 2) this.samples[out++] = bytes[i] | (bytes[i + 1] << 8);
    if (i < bytes.length) this.spare = bytes[i];
    this.received = out;
    this.pump();
    this.nudge();
    if (this.onprogress) this.onprogress();
  }

  // seam closes one piece of the clip: a piece ends on a whole sample, and a
  // stray byte must not run into the first sample of the next one.
  seam() { this.spare = -1; }

  // finish is the last piece having been read: the clip is all here.
  finish() {
    this.complete = true;
    this.spare = -1;
    this.pump(); // the tail may still be waiting for a buffer
    if (this.onprogress) this.onprogress();
  }

  // fail is the stream breaking off: the words that did arrive still play.
  fail(err) {
    this.error = err;
    this.finish();
    if (this.onfail) this.onfail(err);
  }

  grow(more) {
    if (this.received + more <= this.samples.length) return;
    let size = this.samples.length * 2;
    while (size < this.received + more) size *= 2;
    const bigger = new Int16Array(size);
    bigger.set(this.samples.subarray(0, this.received));
    this.samples = bigger;
  }

  // ---- playing it ----------------------------------------------------------

  // play starts where the playhead stands, and answers like an <audio>
  // element's play(): a promise, rejected when the browser will make no sound.
  async play() {
    const ctx = voiceContext(this.rate);
    if (ctx.state === 'suspended') await ctx.resume(); // a click is what allows this
    if (this.playing) return;
    if (this.complete && this.at >= this.received) this.at = 0; // it had finished: from the top
    this.playing = true;
    this.held = false;
    this.sent = this.at;
    this.anchor(this.at);
    this.pump();
    this.nudge();
    if (this.onplay) this.onplay();
  }

  pause() {
    this.held = true;
    this.nudge(); // stops it asking for more
    if (!this.playing) return;
    this.at = Math.round(this.currentTime * this.rate);
    this.playing = false;
    this.silence();
    if (this.onpause) this.onpause();
  }

  get paused() { return !this.playing; }

  // duration is how much of the clip is here; it grows until the clip is
  // complete, which is the one thing an <audio> element does not do.
  get duration() { return this.received / this.rate; }

  // waiting is the playhead having caught up with the stream: nothing can be
  // heard until more of the clip arrives.
  get waiting() { return this.playing && !this.sources.size && !this.complete; }

  get currentTime() {
    if (!this.playing) return this.at / this.rate;
    const ctx = voiceContext(this.rate);
    const played = this.anchorSample + (ctx.currentTime - this.anchorTime) * this.rate * this.speed;
    return Math.min(this.received, Math.max(this.anchorSample, played)) / this.rate;
  }

  set currentTime(seconds) {
    const to = Math.round(Math.max(0, Math.min(this.duration, seconds)) * this.rate);
    this.silence();
    this.at = to;
    this.sent = to;
    if (this.playing) { this.anchor(to); this.pump(); }
    this.nudge();
  }

  // nudge has the feed look again at what is needed: the playhead, the speed
  // or what is in hand changed.
  nudge() {
    if (this.feed) this.feed.check();
  }

  get playbackRate() { return this.speed; }

  set playbackRate(speed) {
    if (speed === this.speed) return;
    const at = this.currentTime;
    this.speed = speed;
    this.currentTime = at; // what was scheduled ran at the old speed
  }

  // pump hands everything that has arrived since the last call to the output,
  // as one buffer starting where the previous one ends.
  pump() {
    if (!this.playing) return;
    const ctx = voiceContext(this.rate);
    if (this.sent >= this.received) {
      if (this.complete && !this.sources.size) this.ended();
      return;
    }

    const from = this.sent;
    const count = this.received - from;
    const buffer = ctx.createBuffer(1, count, this.rate);
    const channel = buffer.getChannelData(0);
    for (let i = 0; i < count; i++) channel[i] = this.samples[from + i] / PCM_FULL_SCALE;

    let at = this.timeOf(from);
    if (at < ctx.currentTime) { // the playhead ran dry: carry on from here
      at = ctx.currentTime + PCM_LEAD;
      this.anchorSample = from;
      this.anchorTime = at;
    }

    const source = ctx.createBufferSource();
    source.buffer = buffer;
    source.playbackRate.value = this.speed;
    source.connect(ctx.destination);
    source.onended = () => { this.sources.delete(source); this.pump(); };
    source.start(at);
    this.sources.add(source);
    this.sent = this.received;
  }

  // timeOf is when a sample is due on the audio clock.
  timeOf(sample) {
    return this.anchorTime + (sample - this.anchorSample) / (this.rate * this.speed);
  }

  anchor(sample) {
    this.anchorSample = sample;
    this.anchorTime = voiceContext(this.rate).currentTime + PCM_LEAD;
  }

  // silence drops what is scheduled but not yet heard, so the playhead can be
  // put somewhere else.
  silence() {
    for (const source of this.sources) {
      source.onended = null;
      try { source.stop(); } catch { /* it never started */ }
    }
    this.sources.clear();
  }

  ended() {
    this.playing = false;
    this.at = this.received;
    if (this.onended) this.onended();
  }
}

// speechPieces cuts text into the pieces that are each read on their own, at
// sentence ends and line breaks (see SENTENCE_END in speakable.js). A sentence
// longer than a whole piece is the one thing cut inside a sentence: at its last
// comma that fits, or failing that its last space. A piece that runs out of
// room ends at its last line break instead, when that keeps at least half of
// it (min), and the rest of that paragraph starts the next piece.
function speechPieces(text) {
  const pieces = [];
  let spoken = 0; // characters in pieces
  let current = '';
  let lead = ''; // what separated current from the piece before it
  let paragraph = 0; // where in current its last line break is, 0 for none
  let paragraphGap = '';
  const limits = () => {
    if (!pieces.length) return SPEECH_FIRST;
    const max = Math.min(SPEECH_LONGEST, spoken + SPEECH_FIRST.max);
    return { min: Math.floor(max / 2), max };
  };
  const push = (piece) => { pieces.push(piece); spoken += piece.length; };
  const close = () => { if (current) push(current); current = ''; paragraph = 0; };

  const parts = text.trim().split(SENTENCE_END);
  for (let i = 0; i < parts.length; i += 2) {
    let sentence = parts[i].trim();
    if (!sentence) continue;
    const gap = i && /\n/.test(parts[i - 1]) ? (/\n\s*\n/.test(parts[i - 1]) ? '\n\n' : '\n') : ' ';
    const overflows = () => current && current.length + gap.length + sentence.length > limits().max;
    if (overflows() && paragraph >= limits().min) {
      const rest = current.slice(paragraph + paragraphGap.length);
      current = current.slice(0, paragraph);
      close();
      current = rest;
      lead = paragraphGap;
    }
    if (overflows()) close();
    while (!current && sentence.length > limits().max) {
      const cut = cutWithin(sentence, limits());
      push(sentence.slice(0, cut).trim());
      sentence = sentence.slice(cut).trim();
    }
    if (!current) lead = gap;
    else if (gap !== ' ') { paragraph = current.length; paragraphGap = gap; }
    current = current ? current + gap + sentence : sentence;
    if (!pieces.length && current.length >= SPEECH_FIRST.min) close();
  }

  // what is left is too short to stand alone: it goes with the piece before
  const last = pieces[pieces.length - 1];
  const joined = last && last.length + lead.length + current.length;
  if (current && last && current.length < SPEECH_TAIL && joined <= SPEECH_LONGEST) pieces[pieces.length - 1] = last + lead + current;
  else close();
  return pieces.length ? pieces : [text];
}

// cutWithin is where to cut a sentence so the part before holds between min
// and max characters: after its last comma, semicolon or colon in that range,
// else at its last space, else at max.
function cutWithin(sentence, { min, max }) {
  const reach = sentence.slice(0, max + 1);
  const clause = Math.max(0, ...[...reach.matchAll(/[،,;:](?=\s)/g)].map((m) => m.index + 1));
  if (clause >= min) return clause;
  const space = reach.lastIndexOf(' ');
  return space > 0 ? space : max;
}
