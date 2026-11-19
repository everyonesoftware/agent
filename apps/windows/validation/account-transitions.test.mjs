import test from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
const app=new URL('../Agent.App/',import.meta.url);
const main=fs.readFileSync(new URL('MainWindow.cs',app),'utf8');
const settings=fs.readFileSync(new URL('MainWindow.Settings.cs',app),'utf8');
function body(source,name){const start=source.indexOf(` ${name}(`);assert.ok(start>=0,name);const open=source.indexOf('{',start);let depth=1,end=open+1;for(;depth&&end<source.length;end++){if(source[end]==='{')depth++;if(source[end]==='}')depth--;}return source.slice(open+1,end-1);}
test('all account transitions use the sensitive view reset boundary',()=>{
 for(const name of ['ActivateClient','ClearAccount'])assert.match(body(main,name),/ResetAccountView\(\)/,name);
 assert.match(body(settings,'ChangeProfileAsync'),/ResetAccountView\(\)/);
 assert.match(body(settings,'PollDeviceAsync'),/ResetAccountView\(\)/);
 assert.match(body(settings,'StartDeviceAsync'),/ResetAccountView\(\)/);
 assert.match(body(settings,'RefreshProviderAsync'),/activeProfile!=profiles.Text\("active_profile_id"\).*ResetAccountView\(\)/s);
 assert.match(body(main,'ResetAccountView'),/accountView\.Reset\(/);
 assert.match(body(main,'ResetAccountView'),/C<PasswordBox>\(name\)\.Password=""/);
 assert.match(body(main,'ResetAccountView'),/ShowSettings\(false\)/);
 for(const name of ['SignedInAs','ServerDescription','ProviderMessage','ResearchStatus','DeviceCode','AllowanceFreshness','AllowanceMessage','BudgetTimezone','Username','LoginFeedback'])assert.ok(body(main,'ResetAccountView').includes(`"${name}"`),name);
 for(const name of ['DeviceLogin','StopResearch','RevokeResearch'])assert.ok(body(main,'ResetAccountView').includes(`"${name}"`),name);
 assert.match(body(main,'ResetAccountView'),/rendering=true/);
 assert.match(body(main,'ResetAccountView'),/"ResearchToggle","BudgetEnabled","MemoryEnabled"/);
 assert.match(body(main,'ResetAccountView'),/Notice.*IsOpen=false/);
 const login=body(main,'LoginAsync');assert.ok(login.indexOf('username=C<TextBox>("Username").Text')<login.indexOf('ActivateClient(endpoint)'));
 assert.match(login,/SignInAsync\(username,password,/);
 assert.match(body(main,'ActivateClient'),/if\(client==activated\)ClearAccount\(\)/);
});
test('settings mutations require freshly loaded destination state before reading drafts',()=>{
 for(const [name,section] of [['ChangeResearchAsync','Research'],['SaveBudgetAsync','Research'],['StartDeviceAsync','Provider'],['ChangeProfileAsync','Provider'],['EditMemoryAsync','Memory'],['ResetPasswordAsync','Account']]){
  const code=body(settings,name);assert.ok(code.includes(`accountView.Require(SettingsSection.${section})`),name);
  assert.ok(!code.includes('if(researchSettings is null)await RefreshResearchAsync()'));
 }
 assert.match(main,/accountView.Require\(SettingsSection.Memory\);await Need\(\)\.PutAsync/);
 for(const [name,section] of [['RefreshResearchAsync','Research'],['RefreshProviderAsync','Provider'],['RefreshMemoryAsync','Memory']])assert.ok(body(settings,name).includes(`accountView.MarkLoaded(SettingsSection.${section})`),name);
 assert.match(body(settings,'SignOutAsync'),/await c.SignOutAsync.*ClearAccount\(\)/s);
 assert.match(body(settings,'ChangeServerAsync'),/await client.SignOutAsync.*ClearAccount\(\)/s);
 assert.match(body(main,'SetBusy'),/accountView.IsLoaded/);
});
