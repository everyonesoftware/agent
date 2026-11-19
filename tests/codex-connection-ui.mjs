// Exercise the production connection controller without provider/network access.
import assert from 'node:assert/strict';
import {readFile} from 'node:fs/promises';
import vm from 'node:vm';
const source = await readFile(new URL('../page.go', import.meta.url), 'utf8');
const startSource = source.slice(source.indexOf('      async function startChatGPTAuthorization('), source.indexOf("      connectButton.addEventListener('click'"));
const calls = [], renders = [];
let result={profile_id:'new',login_id:'device-1',verification_url:'https://auth.openai.com/codex/device',user_code:'TEST-CODE',expires_at:2000000000}, refreshes=0;
const button = {disabled:false,hidden:false,focus(){},getClientRects(){return [1];}};
const context = vm.createContext({
  localHost:false, connectionBusy:false, connectionRefresh:null, activeProfileID:'active', deviceLogin:null,deviceEpoch:0,deviceController:null,authorizationController:null,chatApp:{hidden:false},AbortController,setTimeout,clearTimeout,
  providerError:{hidden:true}, connectionFeedback(){}, updateConnectionControls(){},
  fetch:async (path, options) => {calls.push({path, options}); return {ok:true,headers:{get:()=> 'application/json'},json:async()=>result};},
  beginDeviceLogin: login => renders.push(login),
  refreshConnectionState:async()=>{refreshes++;}, settingsPanel:{hidden:false}, connectButton:button, checkConnectionButton:button,
  window:{location:{assign(){assert.fail('Device sign-in must never navigate away from Agent');}}},
});
vm.runInContext(startSource,context);
await vm.runInContext("startChatGPTAuthorization('/auth/start?mode=add', connectButton)",context);
assert.equal(calls.length,1,'Remote browsers must be able to start the same Codex device connection');
assert.equal(calls[0].options.method,'POST');
assert.ok(calls[0].options.signal,'Device login startup must have a bounded, abortable request');
assert.deepEqual(JSON.parse(calls[0].options.body),{expected_active_profile_id:'active'});
assert.equal(renders[0]?.profile_id,'new','Adding an account polls its pending profile, not the currently active account');
assert.equal(renders[0]?.user_code,'TEST-CODE');
assert.equal(context.connectionBusy,false);
result={profile_id:'active',connection_id:'existing',connected:true};
await vm.runInContext("startChatGPTAuthorization('/auth/start', connectButton)",context);
assert.equal(renders.length,1,'An already connected Codex profile must not request a second device sign-in');
assert.equal(refreshes,1,'An already connected profile refreshes accounts and models directly');
let resolveStart;
context.fetch=()=>new Promise(resolve=>{resolveStart=resolve;});
const staleStart=vm.runInContext("startChatGPTAuthorization('/auth/start', connectButton)",context);
context.authorizationController=new AbortController(); // A newer account action owns the lock.
context.connectionBusy=true;
resolveStart({ok:true,headers:{get:()=> 'application/json'},json:async()=>result});
await staleStart;
assert.equal(context.connectionBusy,true,'A late authorization response cannot unlock a newer account action');
assert.equal(refreshes,1,'A replaced startup request cannot publish its obsolete connection result');
assert.doesNotMatch(source,/SIWC_REDIRECT_URI|Connect usage account|Reconnect usage account|separate Codex sign-in|usage account is different/,'Only one connection and account identity should be exposed');
assert.doesNotMatch(source,/id="remote-connection-setup"|id="local-connection-address"/,'Device sign-in must not retain localhost setup guidance');
console.log('PASS remote unified device authorization and account-target contract');
const signInSource=source.match(/async function requireSignIn\(\) \{[\s\S]*?\n      \}/)[0];
let stopped=0;
const signInContext=vm.createContext({
  modelStorageKey:'everyoneagent.model.old-user',authorizationController:null,resetMemory(){},fetch:async()=>({ok:true,json:async()=>({id:'new-user',username:'New user'})}),
  document:{getElementById:()=>({})},authLoading:{hidden:false},authGate:{hidden:true},chatApp:{hidden:true},restorePage(){},connectionStatus:{},
  refreshConnectionState:async()=>{},refreshConversations:async()=>{},stopUsage(){},stopDeviceLogin(){stopped++;},connectionBusy:false,
});
vm.runInContext(signInSource,signInContext);
await vm.runInContext('requireSignIn()',signInContext);
assert.equal(signInContext.authLoading.hidden,true,'A resolved session must hide neutral loading');
assert.equal(signInContext.authGate.hidden,true,'A valid session must keep the login form hidden');
assert.equal(signInContext.chatApp.hidden,false,'A valid session opens the conversation workspace');
assert.equal(stopped,1,'Changing the authenticated Agent user must discard the previous user’s device code and pending responses');
console.log('PASS pending device login is isolated across Agent accounts');
const controlsSource=source.match(/function updateConnectionControls\(\) \{[\s\S]*?\n      \}/)[0];
const controlsContext=vm.createContext({connectionBusy:false,connectionRefresh:null,deviceLogin:{login_id:'pending'},
  connectButton:{},addAccountButton:{},disconnectButton:{},checkConnectionButton:{setAttribute(){}},accountSelect:{},profileCount:0,activeProfileID:'',connectionBadge:{},connectionStateLabel:'Not connected'});
vm.runInContext(controlsSource+'\nupdateConnectionControls();',controlsContext);
assert.equal(controlsContext.connectionBadge.textContent,'Awaiting approval','The primary badge must describe the unified pending connection');
assert.equal(controlsContext.connectButton.disabled,true);
assert.equal(controlsContext.addAccountButton.disabled,true);
assert.equal(controlsContext.disconnectButton.disabled,true);
assert.equal(controlsContext.accountSelect.disabled,true);
console.log('PASS pending connection badge and overlapping account mutation lock');
const deviceSource=source.slice(source.indexOf('      function stopDeviceLogin()'),source.indexOf("      usageElement('cancel').addEventListener"));
const elements=new Map();
const element=id=>{if(!elements.has(id))elements.set(id,{hidden:false,textContent:'',removeAttribute(name){delete this[name];}});return elements.get(id);};
let timerID=0;
const timers=new Map(), deviceCalls=[];
let deviceResult={profile_id:'pending-profile',state:'pending',login:{login_id:'device-2'}};
let deviceRefreshes=0;
const deviceContext=vm.createContext({
  deviceLogin:null,deviceEpoch:0,deviceTimer:null,deviceController:null,deviceBusy:false,
  usageElement:element,document:{hidden:false,getElementById:element},chatApp:{hidden:false},settingsPanel:{hidden:false},
  usageDate:()=>'',URL,AbortController,
  setTimeout(fn,delay){const id=++timerID;timers.set(id,{fn,delay});return id;},clearTimeout(id){timers.delete(id);},
  updateConnectionControls(){},connectionFeedback(){},
  refreshConnectionState:async()=>{deviceRefreshes++;},connectButton:button,checkConnectionButton:button,
  fetch:async(path,options)=>{deviceCalls.push({path,options});return {ok:true,json:async()=>deviceResult};},
});
vm.runInContext(deviceSource,deviceContext);
const login={profile_id:'pending-profile',connection_id:'connection-2',login_id:'device-2',user_code:'SAFE-CODE',verification_url:'https://auth.openai.com/codex/device'};
deviceContext.login=login;
vm.runInContext('beginDeviceLogin(login)',deviceContext);
assert.equal(element('device').hidden,false);
assert.equal(element('code').textContent,'SAFE-CODE');
assert.equal(element('verification').href,login.verification_url);
for(const url of ['http://auth.openai.com/codex/device','https://auth.openai.com.evil.test/codex/device','https://evil.test/codex/device','https://auth.openai.com:444/codex/device','https://user@auth.openai.com/codex/device','https://auth.openai.com/codex/device?next=evil','https://auth.openai.com/codex/device#fragment','https://auth.openai.com/other','javascript:alert(1)']){
  deviceContext.login={...login,verification_url:url};
  vm.runInContext('beginDeviceLogin(login)',deviceContext);
  assert.equal(element('verification').hidden,true,url);
  assert.equal(element('verification').href,undefined,url);
}
deviceContext.login=login;vm.runInContext('beginDeviceLogin(login)',deviceContext);
await vm.runInContext("deviceRequest('status')",deviceContext);
assert.equal(deviceCalls.at(-1).path,'/api/usage/status?profile_id=pending-profile&connection_id=connection-2','Polling binds the captured connection, including approval observed by another tab');
assert.ok([...timers.values()].some(timer=>timer.delay===2000),'Pending authorization reschedules polling');
await vm.runInContext("deviceRequest('cancel')",deviceContext);
assert.deepEqual(JSON.parse(deviceCalls.at(-1).options.body),{profile_id:'pending-profile',connection_id:'connection-2',login_id:'device-2'});
assert.equal(deviceContext.deviceLogin,null);
assert.equal(element('code').textContent,'');
assert.equal(element('verification').href,undefined);
assert.equal(deviceRefreshes,1);
vm.runInContext('beginDeviceLogin(login)',deviceContext);
let resolveDevice;
deviceContext.fetch=()=>new Promise(resolve=>{resolveDevice=resolve;});
const stalePoll=vm.runInContext("deviceRequest('status')",deviceContext);
vm.runInContext("stopDeviceLogin();beginDeviceLogin({...login,login_id:'new-login',user_code:'NEW-CODE'})",deviceContext);
resolveDevice({ok:true,json:async()=>({profile_id:'pending-profile',state:'connected'})});
await stalePoll;
assert.equal(deviceRefreshes,1,'Late approval cannot publish into a replacement login');
assert.equal(element('code').textContent,'NEW-CODE');
assert.equal(deviceContext.deviceBusy,false);
console.log('PASS exact verification URL allowlist, immutable cancellation and stale polling protection');
for (const action of ['status','cancel']) {
  deviceContext.login=login;
  vm.runInContext('beginDeviceLogin(login)',deviceContext);
  deviceContext.fetch=async()=>({ok:false,status:409,json:async()=>({error:'The captured connection changed.'})});
  const before=deviceRefreshes;
  await vm.runInContext(`deviceRequest('${action}')`,deviceContext);
  assert.equal(deviceContext.deviceLogin,null,'A terminal connection conflict must release the device-login lock');
  assert.equal(element('device').hidden,true);
  assert.equal(deviceRefreshes,before+1,'Reconcile account state after a terminal conflict');
  assert.equal(deviceContext.deviceTimer,null,'A terminal conflict must not keep retrying the stale challenge');
}
console.log('PASS terminal connection conflicts reconcile accounts and release stale device locks');
