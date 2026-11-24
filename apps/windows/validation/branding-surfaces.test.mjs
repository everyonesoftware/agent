import test from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';

const app = new URL('../Agent.App/', import.meta.url);
const read = name => fs.readFileSync(new URL(name, app), 'utf8');

test('title bar, sign-in and sidebar share the deployed Agent artwork, not a text mark', () => {
  const shell = read('Shell.xaml');
  const window = read('MainWindow.cs');
  for (const [name, size] of [['AppIcon', 20], ['SignInIcon', 40], ['SidebarIcon', 26]]) {
    assert.match(shell, new RegExp(`<Image[^>]*x:Name="${name}"[^>]*Width="${size}"[^>]*Height="${size}"[^>]*AutomationProperties.AccessibilityView="Raw"[^>]*/>`));
  }
  assert.match(window, /var brandImage=new BitmapImage\(new Uri\(Path.Combine\(AppContext.BaseDirectory,"Assets","Agent.png"\)\)\);/);
  assert.match(window, /foreach\(string name in new\[\]\{"AppIcon","SignInIcon","SidebarIcon"\}\)C<Image>\(name\).Source=brandImage;/);
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
