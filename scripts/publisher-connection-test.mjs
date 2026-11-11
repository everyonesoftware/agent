import { resolve } from 'node:path';
import { pathToFileURL } from 'node:url';

export async function runCli(args, env, { transport } = {}) {
  if (args.length !== 0) throw new Error('Invalid arguments');
  return sendConnectionTest({ endpoint: env.SERVICES_PUBLISH_URL, token: env.SERVICES_PUBLISH_TOKEN, connectionId: env.SERVICES_CONNECTION_ID, challenge: env.SERVICES_CONNECTION_CHALLENGE, env, transport });
}

if (process.argv[1] && import.meta.url === pathToFileURL(resolve(process.argv[1])).href) {
  try {
    await runCli(process.argv.slice(2), process.env);
    console.log('Services connection test acknowledged');
  } catch {
    // No exception text, server body, URL, input or credential may reach CI logs.
    console.error('Services connection test failed');
    process.exitCode = 1;
  }
}

export async function sendConnectionTest({ endpoint, token, connectionId, challenge, env = process.env, transport = fetch, maxAttempts = 3, timeoutMs = 10_000, baseDelayMs = 500 }) {
  const check = (condition, field) => { if (!condition) throw new Error(`Invalid ${field}`); };
  check(typeof endpoint === 'string' && endpoint.length <= 2048 && /^https:\/\/[^\s\\/?#@]+\/api\/images\/published$/.test(endpoint), 'endpoint');
  let url;
  try { url = new URL(endpoint); } catch { throw new Error('Invalid endpoint'); }
  check(url.protocol === 'https:' && url.hostname && !url.username && !url.password && !url.search && !url.hash && url.pathname === '/api/images/published', 'endpoint');
  check(typeof token === 'string' && /^[\x21-\x7e]{32,4096}$/.test(token), 'token');
  check(typeof connectionId === 'string' && /^[A-Za-z0-9_-]{1,256}$/.test(connectionId), 'connection id');
  check(typeof challenge === 'string' && /^[A-Za-z0-9_-]{16,512}$/.test(challenge), 'challenge');
  const repository = env?.GITHUB_REPOSITORY;
  const ref = env?.GITHUB_REF;
  const runId = env?.GITHUB_RUN_ID;
  check(typeof repository === 'string' && repository.length <= 256 && /^[A-Za-z0-9][A-Za-z0-9-]*\/[A-Za-z0-9_][A-Za-z0-9_.-]*$/.test(repository), 'provenance');
  check(typeof ref === 'string' && ref.length <= 1024 && /^refs\/(heads|tags)\/[^\s\x00-\x1f\x7f~^:?*\[\\]+$/.test(ref) && !ref.includes('..') && !ref.includes('@{') && !ref.endsWith('.') && ref.split('/').every(part => part && !part.startsWith('.') && !part.endsWith('.lock')), 'provenance');
  check(typeof runId === 'string' && /^[1-9]\d{0,31}$/.test(runId), 'provenance');
  url.pathname = '/api/publishers/callback';
  check(Number.isInteger(maxAttempts) && maxAttempts >= 1 && maxAttempts <= 3 && Number.isInteger(timeoutMs) && timeoutMs >= 1 && timeoutMs <= 10_000 && Number.isInteger(baseDelayMs) && baseDelayMs >= 0 && baseDelayMs <= 1000, 'retry options');
  // Capture all metadata and serialize once; retries prove the same challenge.
  const body = JSON.stringify({ connectionId, challenge, sourceRepository: repository, ref, runId });
  const transientCodes = new Set(['ECONNRESET', 'ECONNREFUSED', 'ETIMEDOUT', 'EAI_AGAIN', 'ENETUNREACH', 'EHOSTUNREACH', 'UND_ERR_CONNECT_TIMEOUT', 'UND_ERR_HEADERS_TIMEOUT', 'UND_ERR_BODY_TIMEOUT', 'UND_ERR_SOCKET']);
  for (let attempt = 1; attempt <= maxAttempts; attempt++) {
    const controller = new AbortController();
    let timer;
    let transient = false;
    const deadline = new Promise((_, reject) => {
      timer = setTimeout(() => { controller.abort(); reject(new Error()); }, timeoutMs);
    });
    try {
      const operation = async () => {
        const response = await transport(url.href, { method: 'POST', headers: { Authorization: `Bearer ${token}`, 'Content-Type': 'application/json' }, body, redirect: 'manual', signal: controller.signal });
        if (response.status === 408 || response.status === 429 || response.status >= 500 && response.status <= 599) {
          transient = true;
          await response.body?.cancel();
          throw new Error();
        }
        if (response.status !== 200) { await response.body?.cancel(); throw new Error(); }
        return acknowledgement(response);
      };
      return await Promise.race([operation(), deadline]);
    } catch (error) {
      transient = transient || controller.signal.aborted || transientCodes.has(error?.cause?.code ?? error?.code);
      if (!transient) throw new Error('Connection test rejected or invalid acknowledgement');
    } finally { clearTimeout(timer); }
    if (attempt < maxAttempts) await new Promise(resolve => setTimeout(resolve, baseDelayMs * 2 ** (attempt - 1)));
  }
  throw new Error(`Connection test failed after ${maxAttempts} attempts`);
}

async function acknowledgement(response) {
  // Read incrementally: never buffer an unbounded or attacker-supplied response.
  const reader = response.body?.getReader();
  if (!reader) throw new Error();
  const chunks = [];
  let size = 0;
  try {
    while (true) {
      const { done, value } = await reader.read();
      if (done) break;
      size += value.byteLength;
      if (size > 4096) { await reader.cancel(); throw new Error(); }
      chunks.push(value);
    }
  } finally { reader.releaseLock(); }
  const value = JSON.parse(Buffer.concat(chunks).toString('utf8'));
  if (!value || Object.keys(value).length !== 1 || !['verified', 'duplicate'].includes(value.result)) throw new Error();
  return value.result;
}
