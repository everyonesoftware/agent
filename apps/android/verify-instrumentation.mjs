import { readFileSync } from 'node:fs';
import { pathToFileURL } from 'node:url';

export function verifyInstrumentation(text, expected) {
 if (!Number.isSafeInteger(expected) || expected < 1) throw new Error('Expected positive test count');
 if (/FAILURES!!!|shortMsg=|INSTRUMENTATION_FAILED|Process crashed|INSTRUMENTATION_STATUS_CODE: -[1-9]/.test(text)) {
  throw new Error('Instrumentation failed, crashed, or skipped a test');
 }
 const match = [...text.matchAll(/^OK \((\d+) tests?\)\s*$/gm)];
 if (match.length !== 1 || Number(match[0][1]) !== expected || !/^INSTRUMENTATION_CODE: -1\s*$/m.test(text)) {
  throw new Error(`Missing complete successful JUnit result for ${expected} tests`);
 }
 return expected;
}
if (process.argv[1] && import.meta.url === pathToFileURL(process.argv[1]).href) {
 const count = verifyInstrumentation(readFileSync(process.argv[2], 'utf8'), Number(process.argv[3]));
 console.log(`Verified ${count} instrumentation tests: no failures or skips`);
}
