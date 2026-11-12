package main

// pwaUpdateScript is served separately so update support cannot interrupt chat startup.
const pwaUpdateScript = `(() => {
  'use strict';
  const element = id => document.getElementById('pwa-' + id);
  const panel = element('updates');
  if (!panel) return;
  const supported = window.isSecureContext && navigator.serviceWorker;
  const currentBuild = document.querySelector('meta[name="app-build-id"]').content;
  const currentVersion = document.querySelector('meta[name="app-version"]').content;
  element('version').textContent = 'Current version: ' + currentVersion;
  let registration, starting, checking = false, applying = false, epoch = 0;
  function publish(state, message) {
    panel.dataset.state = state;
    element('status').textContent = message;
    element('check').disabled = !supported || checking || applying;
    element('apply').hidden = element('later').hidden = state !== 'available';
    element('apply').disabled = element('later').disabled = checking || applying;
    panel.setAttribute('aria-busy', String(checking || applying));
  }
  function within(promise) {
    return new Promise((resolve, reject) => {
      const timer = setTimeout(() => reject(new Error('Update operation timed out. Try again.')), 15000);
      Promise.resolve(promise).then(value => { clearTimeout(timer); resolve(value); }, error => { clearTimeout(timer); reject(error); });
    });
  }
  function available() { publish('available', 'An update is available. Finish unsaved changes before reloading.'); }
  function register() {
    if (registration) return Promise.resolve(registration);
    if (!starting) starting = within(Promise.resolve().then(() => navigator.serviceWorker.register('/sw.js', {scope:'/',updateViaCache:'none'}))).then(value => {
      registration = value;
      const observe = () => {
        const worker = value.installing;
        worker?.addEventListener('statechange', () => {
          if (worker.state === 'installed' && value.waiting && !checking && !applying) available();
        });
      };
      value.addEventListener('updatefound', observe);
      observe();
      return value;
    }).finally(() => { starting = null; });
    return starting;
  }
  async function check() {
    if (!supported || checking || applying) return;
    if (!navigator.onLine) { publish('offline', 'You are offline. Reconnect and check again.'); return; }
    checking = true;
    const token = ++epoch;
    publish('checking', 'Checking for updates…');
    const abort = new AbortController();
    try {
      const reg = await register();
      const response = await within(fetch('/version.json', {cache:'no-store',credentials:'same-origin',signal:abort.signal}));
      if (!response.ok) throw new Error('Update metadata unavailable.');
      const metadata = await within(response.json());
      if (!metadata || typeof metadata.version !== 'string' || !metadata.version.trim() || typeof metadata.buildId !== 'string' || !metadata.buildId.trim()) throw new Error('Invalid update metadata.');
      await within(reg.update());
      if (reg.installing) await new Promise((resolve, reject) => {
        const worker = reg.installing;
        const cleanup = () => { clearTimeout(timer); worker.removeEventListener('statechange', changed); };
        const changed = () => {
          if (worker.state === 'redundant') { cleanup(); reject(new Error('Update installation failed.')); }
          else if (worker.state === 'installed' || worker.state === 'activated') { cleanup(); resolve(); }
        };
        const timer = setTimeout(() => { cleanup(); reject(new Error('Update download timed out.')); }, 15000);
        worker.addEventListener('statechange', changed);
        changed();
      });
      if (token !== epoch) return;
      checking = false;
      if (reg.waiting || metadata.buildId !== currentBuild) available();
      else publish('current', 'You are using the latest version. Checked just now.');
    } catch (_) {
      if (token !== epoch) return;
      checking = false;
      if (registration?.waiting) available();
      else publish(navigator.onLine ? 'error' : 'offline', 'Could not check for updates. Check your connection and try again.');
    } finally { abort.abort(); }
  }
  let applyTimer, approvedRevision, approvedUnsaved;
  function safeToReload() {
    const safety = window.agentUpdateSafety?.();
    if (!safety || safety.pending) {
      publish('available', 'Finish the active request before updating. Your work has not been changed.');
      return false;
    }
    if (safety.unsaved && (!approvedUnsaved || safety.revision !== approvedRevision)) {
      if (!window.confirm('Discard unsaved draft, password entries or open dialog changes and update Agent?')) {
        publish('available', 'Update postponed. Your unsaved changes have been kept.');
        return false;
      }
    }
    approvedRevision = safety.revision;
    approvedUnsaved = safety.unsaved;
    return true;
  }
  function apply() {
    if (checking || applying || panel.dataset.state !== 'available') return;
    approvedRevision = undefined;
    approvedUnsaved = false;
    if (!safeToReload()) return;
    applying = true;
    ++epoch;
    publish('available', 'Applying update… Agent will reload when ready.');
    if (!registration?.waiting) { window.location.reload(); return; }
    applyTimer = setTimeout(() => {
      applying = false;
      publish('error', 'The update could not be applied. Check for updates and try again.');
    }, 15000);
    try { registration.waiting.postMessage({type:'SKIP_WAITING'}); }
    catch (_) { clearTimeout(applyTimer); applying = false; publish('error', 'The update could not be applied. Check for updates and try again.'); }
  }
  navigator.serviceWorker?.addEventListener('controllerchange', () => {
    if (!applying) return;
    clearTimeout(applyTimer);
    applying = false;
    if (safeToReload()) { applying = true; publish('available', 'Reloading Agent…'); window.location.reload(); applying = false; }
  });
  element('apply').addEventListener('click', apply);
  element('check').addEventListener('click', () => { void check(); });
  element('later').addEventListener('click', () => {
    if (!checking && !applying && panel.dataset.state === 'available') publish('available', 'Update postponed. Keep using Agent and update when ready.');
  });
  if (!supported) publish('unsupported', 'Web app updates require a supported browser over HTTPS (or localhost).');
  else {
    publish('idle', 'Updates are applied only when you choose to reload.');
    const token = epoch;
    void register().then(reg => { if (token === epoch && reg.waiting) available(); }).catch(() => {
      if (token === epoch) publish('error', 'App update support could not start. Check your connection and try again.');
    });
  }
})();`
