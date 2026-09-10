// Nothing here is allowed to fail in silence. A voice interface has almost no
// room to explain itself, so every failure goes to three places: one line in
// the status bar for the person waiting, the console for whoever opens it,
// and the terminal running nutshell — which otherwise cannot see this half of
// the app at all.

// logIssue records a failure without interrupting anyone. level is
// 'error' or 'warn'; event says where it happened, e.g. 'transcribe'.
function logIssue(level, event, message, detail) {
  const line = `nutshell [${event}] ${message}`;
  if (level === 'error') console.error(line, detail || ''); else console.warn(line, detail || '');
  try {
    fetch('/api/log', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      keepalive: true, // survives a reload or a tab closing right after the error
      body: JSON.stringify({ level, event, message: String(message), detail: detail ? String(detail) : '' }),
    }).catch(() => { /* the terminal is unreachable; the console still has it */ });
  } catch { /* reporting must never be the thing that breaks */ }
}

// fail is for a step that broke: the status line gets the translated message
// (key may hold a {error} placeholder), the log gets the raw error too.
function fail(event, key, err) {
  const detail = errText(err);
  const shown = t(key).replace('{error}', detail);
  logIssue('error', event, shown, detail);
  flash(shown, true);
  return shown;
}

// warn is for a step that simply produced nothing — a clip too short to hear,
// a transcript with no words in it. Expected, but never silent.
function warn(event, key) {
  const shown = t(key);
  logIssue('warn', event, shown);
  flash(shown, true);
  return shown;
}

// errText pulls the most useful sentence out of whatever was thrown.
function errText(err) {
  if (!err) return '';
  if (typeof err === 'string') return err;
  if (err.message) return err.name && err.name !== 'Error' ? `${err.name}: ${err.message}` : err.message;
  try { return JSON.stringify(err); } catch { return String(err); }
}

// httpError turns a failed response into the clearest error it can: the
// server's own message when there is one, the status line when there is not.
async function httpError(resp) {
  const body = await resp.text().catch(() => '');
  let message = '';
  try { message = JSON.parse(body).error || ''; } catch { message = body.slice(0, 200); }
  return new Error(`${message || resp.statusText || 'request failed'} (HTTP ${resp.status})`);
}

// Anything that escapes a handler still reaches the terminal.
window.addEventListener('error', (e) => {
  logIssue('error', 'script', e.message || 'script error', `${e.filename || ''}:${e.lineno || 0}`);
});

window.addEventListener('unhandledrejection', (e) => {
  logIssue('error', 'promise', errText(e.reason) || 'unhandled rejection');
});
