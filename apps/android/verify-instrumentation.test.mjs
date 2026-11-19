import test from 'node:test';
import assert from 'node:assert/strict';
import { verifyInstrumentation } from './verify-instrumentation.mjs';
const success = '\nOK (3 tests)\n\nINSTRUMENTATION_CODE: -1\n';
test('requires the completed exact JUnit count, not adb exit status', () => {
 assert.equal(verifyInstrumentation(success, 3), 3);
 for (const output of ['', 'OK (3 tests)', success.replace('3 tests', '2 tests'), `${success}FAILURES!!!`, `${success}INSTRUMENTATION_STATUS_CODE: -2`, `${success}INSTRUMENTATION_STATUS_CODE: -4`, `${success}shortMsg=Process crashed`]) {
  assert.throws(() => verifyInstrumentation(output, 3));
 }
});
