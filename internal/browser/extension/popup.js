const byId = id => document.getElementById(id);

async function render(message) {
  const [tab] = await chrome.tabs.query({active: true, currentWindow: true});
  const state = await chrome.runtime.sendMessage({...message, tabId: tab.id});
  byId('tab').textContent = tab.title || tab.url || `tab ${tab.id}`;
  byId('mode').textContent = state.mode ? `Shared to ${state.mode}.` : 'Not shared.';
  byId('read').disabled = state.mode === 'read';
  byId('drive').disabled = state.mode === 'drive';
  byId('stop').disabled = !state.mode;
  const command = document.createElement('code');
  command.textContent = 'tofu browser install';
  byId('state').replaceChildren(...(state.connected
    ? ['Connected to tofu.']
    : ['Not connected to tofu: run ', command, ', then reload this extension.', state.hostError ? ` (${state.hostError})` : '']));
  byId('failure').textContent = state.failure;
}

byId('read').addEventListener('click', () => void render({t: 'share', mode: 'read'}));
byId('drive').addEventListener('click', () => void render({t: 'share', mode: 'drive'}));
byId('stop').addEventListener('click', () => void render({t: 'stop'}));
void render({t: 'state'});
