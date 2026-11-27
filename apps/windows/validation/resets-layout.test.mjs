import { readFileSync } from 'node:fs';
import assert from 'node:assert/strict';
import { test } from 'node:test';

const read = name => readFileSync(new URL(`../Agent.App/${name}`, import.meta.url), 'utf8');

test('ChatGPT Settings has a separate reset balance and native collapsed history', () => {
  const shell = read('Shell.xaml');
  const card = shell.slice(shell.indexOf('x:Name="ChatGPTSection"'), shell.indexOf('x:Name="ResearchSection"'));
  assert.match(card, /View usage history[\s\S]*About allowance[\s\S]*x:Name="ResetsSection"[^>]*Visibility="Collapsed"/);
  assert.match(card, /<Button x:Name="UseReset"[^>]*IsEnabled="False"/);
  assert.match(card, /x:Name="ResetBalance"[^>]*AutomationProperties.LiveSetting="Polite"/);
  assert.match(card, /<Expander x:Name="ResetHistory"[^>]*IsExpanded="False"/);
});

test('only explicit confirmation enters the serialized, scope-checked write path', () => {
  const settings = read('MainWindow.Settings.cs');
  assert.match(settings, /WireSettingsLayout\(\)\s*\{\s*WireResets\(\)/);
  assert.match(settings, /RenderAllowance\(\)\s*\{\s*RenderResets\(\)/);
  const resets = read('MainWindow.Resets.cs');
  assert.match(resets, /Hook\("UseReset", ConfirmResetAsync, false\)/);
  assert.match(resets, /CloseButtonText = "Cancel",\s*DefaultButton = ContentDialogButton.Close/);
  assert.match(resets, /await dialog.ShowAsync\(\)/);
  assert.match(resets, /if \(choice != ContentDialogResult.Primary\) return;[\s\S]*await GuardAsync[\s\S]*if \(!OwnsReset\(\)\)\s*\{[^}]*return;[^}]*\}[\s\S]*await ResetCredits.ConsumeAsync/);
  assert.match(resets, /IsCurrentSession\(captured, epoch\).*settingsOpen.*profile == activeProfile.*connection == connectionId/);
});

test('confirmation revalidates and submits the same offer; uncertain writes invalidate it without replay', () => {
  const resets = read('MainWindow.Resets.cs');
  assert.match(resets, /resetCredits\?\.EligibleOffer\(DateTimeOffset\.UtcNow\.ToUnixTimeSeconds\(\)\) == offer/);
  assert.match(resets, /await ResetCredits\.ConsumeAsync\(captured, profile, connection, offer, deadline\.Token\)/);
  assert.match(resets, /catch \(Exception error\)[\s\S]*InvalidateResetBalance\(\);\s*RenderAllowance\(\);/);
  assert.match(resets, /previousUsage\.Remove\("resets"\)/);
  assert.doesNotMatch(resets, /unconfirmedReset/);
  assert.equal([...resets.matchAll(/ResetCredits\.ConsumeAsync\(/g)].length, 1);
});
