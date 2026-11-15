// Available-history forecasts against the embedded production model.
import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
import vm from 'node:vm';
const page = await readFile(new URL('../page.go', import.meta.url), 'utf8');
const source = page.split('// BEGIN usage history model')[1].split('// END usage history model')[0];
const env = vm.createContext({});
vm.runInContext(source + '\nthis.model = usageHistoryModel; this.path = typeof usageHistoryPath === "function" ? usageHistoryPath : null;', env);
const now = 2000000000;
const fixture = (hours, used, remaining = 31) => ({
  provider:'chatgpt', observed_at:now, reset_at:now+2.9*86400,
  remaining_percent:remaining, stale:false, gap_seconds:180,
  points:Array.from({length:hours*60+1},(_,i)=>({
    at:now-hours*3600+i*60, remaining_percent:remaining+used*(1-i/(hours*60)), reset_at:now+2.9*86400
  }))
});
const short = fixture(6,2);
const model = env.model(short,'24h',now);
assert.equal(model.forecast.seconds,6*3600,'Use the actual available duration instead of requiring 24 hours');
assert.equal(model.forecast.rate,2/(6*3600),'Average includes the entire observed interval');
assert.equal(model.forecast.end.at,short.reset_at);
assert.ok(Math.abs(model.forecast.end.remaining_percent-7.8)<1e-9);
console.log('PASS available six-hour average projects remaining allowance through reset');
assert.ok(Math.abs(model.dailyBudget-31/2.9)<1e-9,'Daily budget comes from allowance and reset, independently of the forecast');
assert.equal(env.model({...short,points:[]},'24h',now).dailyBudget,model.dailyBudget,'A budget is useful even before there are enough observations to measure a rate');
for (const change of [{remaining_percent:null},{remaining_percent:NaN},{reset_at:now},{stale:true}]) {
  assert.equal(env.model({...short,...change},'24h',now).dailyBudget,null,'Do not suggest a current budget from invalid or stale allowance');
}
console.log('PASS daily budget remains useful without history and rejects stale or invalid allowance');
const bucketed={...short,points:short.points.filter((_,i)=>i%5===0),forecast_points:short.points};
for(const range of ['24h','week','period']) {
  const result=env.model(bucketed,range,now);
  assert.equal(result.forecast?.seconds,6*3600,'Display downsampling must not introduce gaps into the forecast evidence');
  assert.equal(result.forecast.rate,model.forecast.rate,'Changing the visible range must not change the forecast');
}
assert.equal(env.model({...bucketed,forecast_points:[]},'week',now).forecast,null,'An explicitly empty forecast series must not fall back to bucketed history');
assert.equal(env.model({...short,points:short.points.slice(-1)},'24h',now).forecast,null,'One instantaneous reading cannot measure a consumption rate');
assert.equal(env.model({...short,observed_at:now-30,points:[{...short.points.at(-1),at:now-30}]},'24h',now).forecast,null,'Time since a single fresh reading is not a measured consumption interval');
assert.equal(env.model({...short,points:short.points.slice(-2)},'24h',now).forecast.seconds,60,'Even one minute of measured history supports an estimate');
console.log('PASS forecast evidence is independent of graph range and bucketed gaps');
assert.equal(typeof env.path,'function','The recorded line needs a shape-preserving path builder');
const readings=[{at:0,remaining_percent:100},{at:100,remaining_percent:60},{at:160,remaining_percent:60},{at:200,remaining_percent:0}];
const before=JSON.stringify(readings);
const curve=env.path(readings,t=>t,p=>p,false);
assert.match(curve,/ C /,'Sampled allowance is rendered as a smooth curve');
assert.equal(JSON.stringify(readings),before,'Smoothing must not mutate observations');
let previous=[0,100];
const endpoints=[];
for (const match of curve.matchAll(/C ([\d.]+) ([\d.]+) ([\d.]+) ([\d.]+) ([\d.]+) ([\d.]+)/g)) {
  const [cx1,cy1,cx2,cy2,x,y]=match.slice(1).map(Number);
  assert.ok(cx1>=previous[0]&&cx2>=cx1&&x>=cx2,'Time must stay monotonic');
  assert.ok(cy1<=previous[1]&&cy1>=y&&cy2<=previous[1]&&cy2>=y,'Bezier controls stay within the recorded balances, so the curve cannot overshoot');
  if(previous[1]===y)assert.equal(cy1,cy2,'An idle interval must stay flat');
  endpoints.push([x,y]);previous=[x,y];
}
for(const p of readings.slice(1))assert.ok(endpoints.some(([x,y])=>x===p.at&&y===p.remaining_percent),'The curve passes through each exact reading');
assert.ok(endpoints.some(([x,y])=>x===25&&y===90),'The curve also passes through approximate threshold markers');
assert.equal(env.path(readings,t=>t,p=>p,true),'M 0 100 H 100 V 60 H 160 V 60 H 200 V 0','The complete Brave ledger remains a truthful step line');
assert.equal(env.path([],t=>t,p=>p,false),'');
assert.equal(env.path(readings.slice(0,1),t=>t,p=>p,false),'M 0 100');
console.log('PASS smooth rendering preserves readings, flat intervals, bounds and milestone positions; ledgers remain stepped');
