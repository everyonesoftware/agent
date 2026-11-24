import test from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';

const app = new URL('../Agent.App/', import.meta.url);
const shell = fs.readFileSync(new URL('Shell.xaml', app), 'utf8');
const composer = () => shell.split('<Border x:Name="ComposerCard"')[1]?.split('</Border>')[0];

test('the chat toolbar is part of the rounded composer, not the conversation header', () => {
  const card = composer();
  assert.ok(card, 'missing composer card');
  assert.match(card, /CornerRadius="8"/);
  for (const name of ['Composer', 'Models', 'ResearchToggle', 'Send']) assert.ok(card.includes(`x:Name="${name}"`), name);
  assert.match(card, /<CheckBox x:Name="ResearchToggle"[^>]*Content="Research public web"/);
});

test('the signed-in shell has the web welcome, searchable history and compact account footer', () => {
  assert.match(shell, /x:Name="Welcome"[\s\S]*?What can I help with\?[\s\S]*?Ask a question to start a conversation\./);
  assert.match(shell, /x:Name="HistorySearch"[^>]*PlaceholderText="Search conversations"/);
  const footer = shell.split('<Border x:Name="AccountFooter"')[1]?.split('</Border>')[0];
  assert.ok(footer, 'missing account footer');
  for (const name of ['SignedInAs', 'AccountInitial', 'SettingsNav']) assert.ok(footer.includes(`x:Name="${name}"`), name);
  assert.doesNotMatch(footer, /Your account|SidebarVersion/);
  assert.match(shell, /x:Name="UpdatesSection"[\s\S]*?<TextBlock x:Name="InstalledVersion"/);
  assert.doesNotMatch(shell, /x:Name="ChatNav"/);
});

test('compact drawer disables the covered workspace and guards async composer refocus', () => {
  const layout = fs.readFileSync(new URL('MainWindow.ChatLayout.cs', app), 'utf8');
  assert.match(layout, /C<ContentControl>\("WorkspaceContent"\)\.IsEnabled = !sidebarOpen/);
  assert.match(layout, /!settingsOpen && !sidebarOpen\) composer\.Focus/);
  const content = shell.split('<ContentControl x:Name="WorkspaceContent"')[1]?.split('</ContentControl>')[0];
  assert.ok(content?.includes('x:Name="ChatView"') && content.includes('x:Name="SettingsView"'));
  assert.ok(!content.includes('x:Name="Sidebar"'));
});
