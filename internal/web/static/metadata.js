// Runtime metadata comes from the agent, so aliases and configured defaults
// resolve to the actual model once its first turn starts. SSE replays it on reload.
let sessionModel = '';
let sessionCwd = '';

function updateSessionMetadata(data) {
  if (data.model) sessionModel = data.model;
  if (data.cwd) sessionCwd = data.cwd;
  renderSessionMetadata();
}

function renderSessionMetadata() {
  const agent = cfg.agent === 'codex' ? 'Codex' : cfg.agent === 'claude code' ? 'Claude Code' : agentName();
  const cwd = sessionCwd || cfg.cwd || cfg.project || '';
  const folder = cwd.replace(/[\\/]+$/, '').split(/[\\/]/).pop() || cwd;
  metadataItem('agent', agent, agent, t('metadataAgent'));
  metadataItem('model', sessionModel || t('metadataModel'), sessionModel || t('modelPending'), t('metadataModel'));
  metadataItem('cwd', folder || t('metadataFolder'), cwd, t('metadataFolder'));
  document.title = [cfg.project, agent, sessionModel, t('brand')].filter(Boolean).join(' · ');
}

function metadataItem(key, text, value, label) {
  const item = document.getElementById(`metadata-${key}`);
  document.getElementById(`session-${key}`).textContent = text;
  item.querySelector('summary').setAttribute('aria-label', `${label}: ${value}`);
  item.querySelector('.session-label').textContent = label;
  item.querySelector('.session-value').textContent = value;
}

// Native details support touch and keyboard as well as pointer input.
document.addEventListener('click', (event) => {
  document.querySelectorAll('.session-item[open]').forEach((item) => {
    if (!item.contains(event.target)) item.open = false;
  });
});
document.addEventListener('keydown', (event) => {
  if (event.key !== 'Escape') return;
  document.querySelectorAll('.session-item[open]').forEach((item) => {
    item.open = false;
    item.querySelector('summary').focus();
  });
});
