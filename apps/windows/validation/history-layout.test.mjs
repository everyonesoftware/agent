import assert from 'node:assert/strict';
import fs from 'node:fs';
import test from 'node:test';

const read = name => fs.readFileSync(new URL('../Agent.App/' + name, import.meta.url), 'utf8');
const dialog = read('MainWindow.History.cs');

test('both usage dialogs show the native chart and summary instead of a raw observation list', () => {
  assert.match(dialog, /research \? "Brave Search usage" : "ChatGPT usage"/);
  assert.match(dialog, /new UsageHistoryPlot\(/);
  for (const copy of ['% left', 'Resets in ', 'projected at reset', 'Daily budget:', 'Recent average']) {
    assert.ok(dialog.includes(copy), copy);
  }
  assert.doesNotMatch(dialog, /TakeLast\(200\)|Recorded observations · local time/);
  assert.match(dialog, /ContentDialogMaxWidth/);
  assert.match(dialog, /windows\.Items\.Count > 1 \? Visibility\.Visible : Visibility\.Collapsed/);
});

test('range reads are account-scoped and closing the chart cancels pending work', () => {
  assert.match(dialog, /CreateLinkedTokenSource\(accountLifetime.Token\)/);
  assert.match(dialog, /open && IsCurrentSession\(c, epoch\) && \(research \|\| activeProfile == profile\)/);
  assert.match(dialog, /requestRevision == revision/);
  assert.match(dialog, /finally[\s\S]*lifetime\.Cancel\(\)/);
  assert.match(dialog, /root\.SizeChanged -=/);
});

test('plot distinguishes history, reset markers and predictions with keyboard and pointer inspection', () => {
  const plot = read('UsageHistoryPlot.cs');
  assert.match(plot, /new\[\] \{ 100, 50, 0 \}/);
  for (const element of ['Period start', 'Now', 'Reset', 'BezierSegment', 'StrokeDashArray', 'Approximate threshold crossing']) {
    assert.ok(plot.includes(element), element);
  }
  for (const key of ['Left', 'Right', 'Home', 'End']) assert.ok(plot.includes(`VirtualKey.${key}`), key);
  assert.match(plot, /PointerMoved \+=/);
  assert.match(plot, /Tapped \+=/);
});

test('an open dialog refreshes without a loading flash and stops when closed', () => {
  assert.ok(dialog.includes('Interval = TimeSpan.FromMinutes(1)'));
  assert.ok(dialog.includes('if (pending is null) await Read(refresh: true)'));
  assert.ok(dialog.includes('timer.Stop()'));
});

test('isolated observations have a visible inspection target', () => {
  assert.ok(read('UsageHistoryPlot.cs').includes('DrawReadingMarker(trend[0])'));
});
