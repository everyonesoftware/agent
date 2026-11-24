import assert from 'node:assert/strict';
import fs from 'node:fs';
import test from 'node:test';

const app = new URL('../Agent.App/', import.meta.url);
const read = name => fs.readFileSync(new URL(name, app), 'utf8');
const shell = read('Shell.xaml');
const settings = read('MainWindow.Settings.cs');
const main = read('MainWindow.cs');
const layout = read('MainWindow.ChatLayout.cs');

test('Settings fits a bounded vertical viewport and groups every section into a card', () => {
  assert.match(shell, /<ScrollViewer x:Name="SettingsView"[^>]*HorizontalContentAlignment="Stretch"[^>]*HorizontalScrollBarVisibility="Disabled"[^>]*HorizontalScrollMode="Disabled"/);
  assert.match(shell, /<Grid x:Name="SettingsColumn"[^>]*MaxWidth="648"[^>]*Padding=/);
  for (const section of ['Updates', 'Connection', 'ChatGPT', 'Research', 'Memory', 'Password', 'SignOut']) {
    assert.match(shell, new RegExp(`<Border x:Name="${section}Section"[^>]*Style="\\{StaticResource SettingsCard\\}"`), section);
  }
  for (const group of ['ServerActions', 'AccountActions', 'DeviceActions', 'BraveActions', 'MemoryActions']) {
    assert.match(shell, new RegExp(`<StackPanel x:Name="${group}"[^>]*Spacing=`), group);
    assert.ok(settings.includes(`"${group}"`), group);
  }
  assert.match(settings, /SettingsColumn"\)\.SizeChanged/);
  assert.match(settings, /Orientation\.Vertical\s*:\s*Orientation\.Horizontal/);
});

test('Settings has a direct return action without depending on the compact drawer', () => {
  assert.match(shell, /<Button x:Name="SettingsBack"[^>]*Content="←  Back to conversation"/);
  assert.ok(main.includes('Hook("SettingsBack", LeaveSettingsAsync, false)'), 'Back uses the refresh-aware exit');
  assert.match(layout, /if \(show\) C<Button>\("SettingsBack"\)\.Focus/);
  assert.match(layout, /compactSidebar[\s\S]*?C<Button>\("SidebarToggle"\)/);
});

test('Back cancels the opening read and lets its busy gate settle before showing chat', () => {
  assert.ok(settings.includes('await RefreshSettingsAsync(refresh.Token)'));
  const exit = settings.slice(settings.indexOf('private async Task LeaveSettingsAsync()'));
  assert.ok(/openingSettingsCancellation\?\.Cancel\(\);\s*await opening;[\s\S]*?if \(!closed && settingsOpen && !busy\) NavigateSettings\(false\);/.test(exit));
  assert.ok(main.includes('!busy || openingSettingsCancellation is { IsCancellationRequested: false }'));
});

test('empty feedback does not consume rows but nonempty Settings messages remain visible', () => {
  for (const name of ['ProviderMessage', 'AllowanceFreshness', 'AllowanceMessage', 'UpdateStatus', 'OfferedVersion']) {
    assert.match(shell, new RegExp(`<TextBlock x:Name="${name}"[^>]*Visibility="Collapsed"`));
    assert.ok(settings.includes(`SetSettingsText("${name}"`), name);
  }
  assert.match(settings, /node\.Visibility = string\.IsNullOrEmpty\(text\) \? Visibility\.Collapsed : Visibility\.Visible/);
  assert.match(settings, /SetSettingsText\("ProviderMessage",\s*"Couldn’t check/);
  assert.match(settings, /SetSettingsText\("AllowanceMessage",\s*"Refresh failed/);
});
