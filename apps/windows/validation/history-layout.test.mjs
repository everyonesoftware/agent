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

test('history range opens below a persistent header button', () => {
  assert.match(dialog, /var range = new DropDownButton\s*\{[^}]*Flyout = rangeMenu/);
  assert.match(dialog, /var rangeMenu = new MenuFlyout\s*\{[^}]*Placement = FlyoutPlacementMode\.BottomEdgeAlignedLeft/);
  assert.match(dialog, /HistoryRow\(heading, range, close\)/);
  assert.doesNotMatch(dialog, /range\.Visibility|range\.Opacity/);
  assert.match(dialog, /if \(rangeMenu\.IsOpen\) rangeMenu\.Hide\(\);\s*else if \(info\.IsExpanded\)/);
});

test('range menu keeps the three choices and updates the button and request on selection', () => {
  assert.deepEqual([...dialog.matchAll(/\("(24h|week|period)", "([^"]+)"\)/g)].map(([, id, text]) => [id, text]), [
    ['24h', '24 hours'], ['week', '1 week'], ['period', 'Entire period'],
  ]);
  assert.match(dialog, /new RadioMenuFlyoutItem\s*\{[^}]*IsChecked = option == selectedRange/);
  const onSelect = dialog.match(/item\.Click \+= async[^\n]*\s*\{([\s\S]*?)\r?\n\s*\};/)?.[1] ?? '';
  assert.match(onSelect, /selectedRange = option;\s*range\.Content = option\.Label;[\s\S]*await Read\(\);/);
  assert.match(dialog, /string rangeId = selectedRange\.Id;/);
  assert.match(dialog, /"range=" \+ Wire\.Segment\(rangeId\)/);
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

test('the Now guide uses the same snapshot as recorded history, including after failed refreshes', () => {
  const plot = read('UsageHistoryPlot.cs');
  assert.equal([...dialog.matchAll(/DispatcherQueue\.CreateTimer\(\)/g)].length, 1);
  assert.doesNotMatch(plot, /DateTimeOffset\.UtcNow/);
  assert.match(plot, /double now = X\(model\.Now\);/);
  assert.match(plot, /Guide\(now, Ink, \[\]\);\s*Mark\("Now", now,/);
  assert.match(dialog, /UsageHistoryChart\.Create\(result, rangeId, DateTimeOffset\.UtcNow\.ToUnixTimeSeconds\(\)\)/);
  assert.match(dialog, /cached\["stale"\] = true;[\s\S]*RenderHistory\(display, detail, UsageHistoryChart\.Create\(cached, rangeId, DateTimeOffset\.UtcNow\.ToUnixTimeSeconds\(\)\)/);
});

test('isolated observations have a visible inspection target', () => {
  assert.ok(read('UsageHistoryPlot.cs').includes('DrawReadingMarker(trend[0])'));
});
