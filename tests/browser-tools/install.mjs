// Linux CI provisioning only; existing local suites can still use UI_BROWSER.
import assert from 'node:assert/strict';
import { appendFile, readFile } from 'node:fs/promises';
import { resolve } from 'node:path';
import { pathToFileURL } from 'node:url';
import { Browser, BrowserPlatform, install, computeExecutablePath } from '@puppeteer/browsers';

export const manifest = JSON.parse(await readFile(new URL('./package.json', import.meta.url), 'utf8'));
export function installationOptions(cacheDirectory, installDeps = false) {
  assert.match(manifest.config.chrome, /^\d+\.\d+\.\d+\.\d+$/);
  assert.match(manifest.config.linuxArchiveSha256, /^[a-f0-9]{64}$/);
  return { browser: Browser.CHROME, platform: BrowserPlatform.LINUX, buildId: manifest.config.chrome,
    expectedHash: manifest.config.linuxArchiveSha256, cacheDir: resolve(cacheDirectory), installDeps };
}
export async function installBrowser(cacheDirectory, installDeps = false) {
  const options = installationOptions(cacheDirectory, installDeps);
  const installed = await install(options);
  const executable = computeExecutablePath(options);
  assert.equal(installed.executablePath, executable);
  console.log('Installed Chrome for Testing ' + options.buildId + ': ' + executable);
  if (process.env.GITHUB_ENV) {
    assert.ok(!/[\r\n]/.test(executable), 'Executable path cannot contain a newline');
    await appendFile(process.env.GITHUB_ENV, 'UI_BROWSER=' + executable + '\nAGENT_TEST_BROWSER_VERSION=' + options.buildId + '\n');
  }
  return executable;
}
if (process.argv[1] && import.meta.url === pathToFileURL(resolve(process.argv[1])).href) {
  assert.ok(process.argv[2], 'Supply a dedicated browser cache directory');
  assert.ok(process.argv.slice(3).every(arg => arg === '--install-deps'), 'Unknown installer argument');
  await installBrowser(process.argv[2], process.argv.includes('--install-deps'));
}
