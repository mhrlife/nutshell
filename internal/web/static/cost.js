// Per-message and per-session cost accounting. Every turn carries
// { stt, tts, agent } in US dollars; null means "no such call happened",
// and agentKnown is false when the agent does not report its cost.

function newCost() {
  return { stt: null, tts: null, agent: null, agentKnown: true };
}

function fmtUSD(v) {
  if (v === null || v === undefined) return '—';
  if (v === 0) return num('$0');
  if (v < 0.001) return num(`$${v.toFixed(5)}`);
  if (v < 0.01) return num(`$${v.toFixed(4)}`);
  return num(`$${v.toFixed(3)}`);
}

function costTotal(c) {
  return (c.stt || 0) + (c.tts || 0) + (c.agent || 0);
}

function costHasData(c) {
  return c.stt !== null || c.tts !== null || c.agent !== null || !c.agentKnown;
}

function breakdownRows(c, agentName) {
  const line = (label, value) => `<div class="row"><span>${label}</span><span class="num">${value}</span></div>`;
  const agentValue = c.agentKnown ? fmtUSD(c.agent === null ? 0 : c.agent) : t('costUnknown');
  return line(t('costStt'), fmtUSD(c.stt === null ? 0 : c.stt))
    + line(t('costTts'), fmtUSD(c.tts === null ? 0 : c.tts))
    + line(agentName, agentValue);
}

function renderCostChip(node, c, title, agentName) {
  if (!node) return;
  node.hidden = !costHasData(c);
  if (node.hidden) return;
  const suffix = c.agentKnown ? '' : '+';
  node.innerHTML = `<span class="num">${fmtUSD(costTotal(c))}${suffix}</span>`
    + `<div class="tip"><div class="row head"><span>${title}</span></div>${breakdownRows(c, agentName)}</div>`;
}

function sessionCost(turns) {
  const sum = newCost();
  for (const turn of turns) {
    const c = turn.cost;
    if (c.stt !== null) sum.stt = (sum.stt || 0) + c.stt;
    if (c.tts !== null) sum.tts = (sum.tts || 0) + c.tts;
    if (c.agent !== null) sum.agent = (sum.agent || 0) + c.agent;
    if (!c.agentKnown) sum.agentKnown = false;
  }
  return sum;
}

function renderSessionCost(turns, agentName) {
  renderCostChip(document.getElementById('session-cost'), sessionCost(turns), t('costSession'), agentName);
}
