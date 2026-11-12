'use strict';

const CACHE_NAME = 'agent-pwa-offline-{{APP_BUILD_ID}}';
const OFFLINE_URL = '/offline.html';

self.addEventListener('install', event => {
  // Deliberately omit credentials: the only stored response is public/static.
  event.waitUntil((async () => {
    const response = await fetch(OFFLINE_URL, { cache: 'no-store', credentials: 'omit', redirect: 'error' });
    if (!response.ok || response.redirected) throw new Error('Offline page unavailable');
    const cache = await caches.open(CACHE_NAME);
    await cache.put(OFFLINE_URL, response);
  })());
});

self.addEventListener('activate', event => {
  event.waitUntil((async () => {
    const names = await caches.keys();
    await Promise.all(names.filter(name => name.startsWith('agent-pwa-offline-') && name !== CACHE_NAME).map(name => caches.delete(name)));
    await self.clients.claim();
  })());
});

self.addEventListener('message', event => {
  // Installation alone never replaces an active session's worker.
  if (event.data && event.data.type === 'SKIP_WAITING') self.skipWaiting();
});

self.addEventListener('fetch', event => {
  const request = event.request;
  const url = new URL(request.url);
  if (request.method !== 'GET' || request.mode !== 'navigate' || request.destination !== 'document' || url.origin !== self.location.origin) return;
  if (['/api', '/auth', '/version', '/healthz'].some(prefix => url.pathname.startsWith(prefix))) return;
  // No shell, API, asset, mutation, or other runtime response is cached.
  event.respondWith(fetch(request).catch(async () => {
    const cache = await caches.open(CACHE_NAME);
    const offline = await cache.match(OFFLINE_URL);
    if (offline) return offline;
    throw new Error('Agent is offline');
  }));
});
