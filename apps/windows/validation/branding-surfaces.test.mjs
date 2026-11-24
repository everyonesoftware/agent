import test from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';

const app = new URL('../Agent.App/', import.meta.url);
const read = name => fs.readFileSync(new URL(name, app), 'utf8');

test('title bar and sign-in retain Agent artwork without duplicate sidebar branding', () => {
  const shell = read('Shell.xaml');
  const window = read('MainWindow.cs');
  for (const [name, size] of [['AppIcon', 20], ['SignInIcon', 40]]) {
    assert.match(shell, new RegExp(`<Image[^>]*x:Name="${name}"[^>]*Width="${size}"[^>]*Height="${size}"[^>]*AutomationProperties.AccessibilityView="Raw"[^>]*/>`));
  }
  const titleBar = shell.split('<Border x:Name="TitleBar"')[1]?.split('</Border>')[0];
  assert.match(titleBar, /<Image x:Name="AppIcon"[^>]*\/>\s*<TextBlock Text="Agent"/);
  const sidebar = shell.split('<Border x:Name="Sidebar"')[1]?.split(/<\/Grid>\r?\n  <\/Border>/)[0];
  assert.ok(sidebar, 'missing sidebar');
  assert.doesNotMatch(sidebar, /<Image\b|Text="Agent"/);
  assert.match(window, /var brandImage=new BitmapImage\(new Uri\(Path.Combine\(AppContext.BaseDirectory,"Assets","Agent.png"\)\)\);/);
  assert.match(window, /foreach\(string name in new\[\]\{"AppIcon","SignInIcon"\}\)C<Image>\(name\).Source=brandImage;/);
  assert.doesNotMatch(shell, /(?:Text|Content)="e1"/i);
});

test('executable and running shell use the same deployed icon without duplicate content items', () => {
  const project = read('Agent.App.csproj');
  assert.match(project, /<ApplicationIcon>Assets\/Agent.ico<\/ApplicationIcon>/);
  assert.match(read('MainWindow.cs'), /AppWindow.SetIcon\(Path.Combine\(AppContext.BaseDirectory,"Assets","Agent.ico"\)\)/);
  for (const asset of ['Agent.ico', 'Agent.png']) {
    const escaped = asset.replace('.', '\\.');
    assert.match(project, new RegExp(`<Content Remove="[^"]*Assets/${escaped}`));
    assert.match(project, new RegExp(`<None Remove="[^"]*Assets/${escaped}`));
    assert.equal([...project.matchAll(new RegExp(`<Content Include="Assets/${escaped}" CopyToOutputDirectory="PreserveNewest" CopyToPublishDirectory="PreserveNewest"`, 'g'))].length, 1);
  }
});
