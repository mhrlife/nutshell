// A passage heard aloud, read as it is or summarized first, becomes a clip
// under the turn whose answer it came from. The clip shows while it is being
// prepared, so a click on the selection bar is never met with silence, and it
// stays there to be played again. Clips live as long as the page does, like
// a turn's own audio.

const CLIP_LOADING = ['summarizing', 'voicing'];

let clipsPending = 0; // clips still being summarized or turned into speech

// speakPassage adds a clip for passage under turn, fills it in and plays it.
// What it costs is charged to turn.
async function speakPassage(turn, passage, summarize) {
  if (!cfg.voice) { flash(t('noVoice')); return; }
  stopPlayback();
  const clip = addClip(turn, passage, summarize ? 'summary' : 'read');
  clipsPending++;
  renderState();
  try {
    if (summarize) {
      const resp = await postJSON('/api/summarize', { text: passage, lang: settings.lang });
      const out = await resp.json();
      addCost(turn, 'summary', out.cost_usd);
      clip.text = plainText(out.text);
      clip.speech = spokenText(out.speech || out.text);
      setClipStatus(clip, 'voicing');
    }
    const spoken = summarize ? clip.speech : plainText(passage);
    const resp = await postJSON('/api/speak', { text: spoken, lang: settings.lang });
    clip.audio = URL.createObjectURL(await resp.blob());
    priceClip(turn, resp.headers.get('X-Generation-Id'));
    setClipStatus(clip, 'ready');
    // never talk over the mic, or over something started while this loaded
    if (!player && state !== 'listening' && state !== 'transcribing') play(clip.audio, null, clip);
  } catch (err) {
    clip.error = fail('speak', summarize && !clip.text ? 'summaryFailed' : 'speakFailed', err);
    setClipStatus(clip, 'failed');
  } finally {
    clipsPending--;
    renderState();
  }
}

// spokenText is plainText for the voice: it keeps the [pause] and <slow>…</slow>
// speech tags a summary is written with, which plainText would break.
function spokenText(s) {
  return (s || '').replace(/```[\s\S]*?```/g, ' ').replace(/[`*_~#]/g, '').replace(/\s+/g, ' ').trim();
}

async function postJSON(path, body) {
  const resp = await fetch(path, {
    method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(body),
  });
  if (!resp.ok) throw await httpError(resp);
  return resp;
}

function addClip(turn, passage, kind) {
  const node = document.getElementById('clip-template').content.firstElementChild.cloneNode(true);
  const clip = { turn, kind, passage, text: '', speech: '', audio: null, error: '', status: kind === 'summary' ? 'summarizing' : 'voicing', node };
  const quoted = node.querySelector('.clip-quote');
  quoted.textContent = oneLine(passage);
  quoted.dir = isRTL(passage) ? 'rtl' : 'ltr';
  turn.clips.push(clip);
  const box = turn.node.querySelector('.clips');
  box.appendChild(node);
  box.hidden = false;
  renderClip(clip);
  node.scrollIntoView({ block: 'nearest', behavior: 'smooth' });
  return clip;
}

function setClipStatus(clip, status) {
  clip.status = status;
  renderClip(clip);
}

function renderClip(clip) {
  const { node, status } = clip;
  const loading = CLIP_LOADING.includes(status);
  const failed = status === 'failed';
  const playing = !!player && player.clip === clip && !player.audio.paused;
  node.dataset.status = status;
  node.classList.toggle('playing', playing);

  const button = node.querySelector('.clip-play');
  button.innerHTML = loading ? ICONS.spinner : failed ? ICONS.close : playing ? ICONS.pause : ICONS.play;
  button.title = loading ? t('loading') : failed ? t('clipRemove') : playing ? t('pause') : t('replay');

  const label = { summarizing: t('clipSummarizing'), voicing: t('clipVoicing'), failed: clip.error }[status];
  node.querySelector('.clip-label').textContent = label || t(clip.kind === 'summary' ? 'clipSummary' : 'clipRead');

  const text = node.querySelector('.clip-text');
  text.textContent = clip.text;
  text.dir = isRTL(clip.text) ? 'rtl' : 'ltr';
  text.hidden = !clip.text;
}

// toggleClip is the clip's button: play or pause a ready clip, or take away
// one that failed.
function toggleClip(clip) {
  if (clip.status === 'failed') { removeClip(clip); return; }
  if (clip.status !== 'ready') return;
  if (player && player.clip === clip) {
    if (player.audio.paused) player.audio.play(); else player.audio.pause();
    return;
  }
  stopPlayback();
  play(clip.audio, null, clip);
}

function removeClip(clip) {
  const { turn, node } = clip;
  turn.clips = turn.clips.filter((c) => c !== clip);
  node.remove();
  turn.node.querySelector('.clips').hidden = !turn.clips.length;
}
