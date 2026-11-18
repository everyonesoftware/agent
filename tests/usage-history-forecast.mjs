// Available-history forecasts against the embedded production model.
import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
import vm from 'node:vm';
import './usage-history-daily.mjs';
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
for (const age of [61,90,120,180]) {
  const delayed = env.model(short,'24h',now+age);
  assert.ok(delayed.forecast,'A briefly delayed poll must retain the forecast');
  assert.equal(delayed.forecast.seconds,model.forecast.seconds,'Unobserved time is not measured idle time');
  assert.equal(delayed.forecast.rate,model.forecast.rate,'Poll timing must not dilute the measured consumption rate');
  assert.ok(delayed.dailyBudget > 0);
}
for (const change of [{stale:true},{observed_at:now-181},{reset_at:now}]) {
  const unavailable = env.model({...short,...change},'24h',now);
  assert.equal(unavailable.forecast,null,'Real staleness, provider errors and resets still disable estimates');
  assert.equal(unavailable.dailyBudget,null);
}
console.log('PASS polling grace retains measured pace without inventing idle observations');
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
vm.runInContext('this.trend = typeof usageHistoryTrend === "function" ? usageHistoryTrend : null;',env);
assert.equal(typeof env.trend,'function','Display history needs time-window smoothing, not just rounded individual steps');
const stairs=Array.from({length:121},(_,i)=>({at:now-7200+i*60,remaining_percent:80-Math.floor(i/10)}));
const stairsBefore=JSON.stringify(stairs), forecastBefore=JSON.stringify(env.model({...short,points:stairs,remaining_percent:68},'24h',now));
const trend=env.trend(stairs);
assert.ok(trend.length<stairs.length/2,'Render fewer time-based trend knots than minute samples');
assert.deepEqual(JSON.parse(JSON.stringify(trend[0])),stairs[0],'Keep the first exact balance');
assert.deepEqual(JSON.parse(JSON.stringify(trend.at(-1))),stairs.at(-1),'Finish at the exact latest balance');
assert.ok(trend.some(p=>p.remaining_percent%1!==0),'Average across the quantized percentage steps');
for(let i=1;i<trend.length;i++)assert.ok(trend[i].remaining_percent<=trend[i-1].remaining_percent,'Smoothed balances never increase or overshoot');
assert.equal(JSON.stringify(stairs),stairsBefore,'Display smoothing leaves exact readings untouched');
assert.equal(JSON.stringify(env.model({...short,points:stairs,remaining_percent:68},'24h',now)),forecastBefore,'Smoothing cannot alter forecasts, milestones or inspection samples');
const linear=[{at:1000,remaining_percent:90},{at:2800,remaining_percent:60}];
const denseLinear=Array.from({length:31},(_,i)=>({at:1000+i*60,remaining_percent:90-i}));
assert.deepEqual(JSON.parse(JSON.stringify(env.trend(linear))),JSON.parse(JSON.stringify(env.trend(denseLinear))),'Time weighting must not depend on polling density');
const idle=stairs.map(p=>({...p,remaining_percent:80}));
assert.ok(env.trend(idle).every(p=>p.remaining_percent===80),'An entirely idle segment stays flat');
assert.equal(env.trend([]).length,0);
assert.equal(env.trend(stairs.slice(0,1))[0].remaining_percent,80);
console.log('PASS time-window trend averages steps without changing raw evidence or current balance');
const ledgerPoints=[{at:now-3600,remaining_percent:100},{at:now-1800,remaining_percent:80},{at:now,remaining_percent:80}];
const ledgerData={...short,provider:'brave',gap_seconds:0,points:ledgerPoints,remaining_percent:80};
const ledgerBefore=JSON.stringify(env.model(ledgerData,'24h',now));
const ledgerTrend=env.trend(ledgerPoints,true);
assert.equal(ledgerTrend.find(p=>p.at===now-2700).remaining_percent,100,'Brave smoothing averages held ledger balances, not a decline across an idle interval');
assert.equal(ledgerTrend.find(p=>p.at===now-1800).remaining_percent,90,'Center the visual transition on the real ledger event');
assert.equal(ledgerTrend.at(-1).remaining_percent,80,'The latest ledger balance stays exact');
assert.equal(JSON.stringify(env.model(ledgerData,'24h',now)),ledgerBefore,'Ledger smoothing leaves raw events, threshold times and forecast rates unchanged');
assert.equal(env.model(ledgerData,'24h',now).milestones.find(p=>p.remaining_percent===90).at,now-1800,'Brave thresholds retain their actual event timestamp');
console.log('PASS Brave display trend uses exact step-held balances without altering ledger evidence');
assert.equal(typeof env.path,'function','The recorded line needs a shape-preserving path builder');
const sloped=env.path([{at:0,remaining_percent:80},{at:300,remaining_percent:79},{at:600,remaining_percent:78}],t=>t,p=>p);
const curves=Array.from(sloped.matchAll(/C ([\d.]+) ([\d.]+) ([\d.]+) ([\d.]+) ([\d.]+) ([\d.]+)/g),m=>m.slice(1).map(Number));
assert.ok(curves[0][3]>curves[0][5]&&curves[1][1]<curves[0][5],'A falling trend must not flatten at every resampled knot');
const readings=[{at:0,remaining_percent:100},{at:100,remaining_percent:60},{at:160,remaining_percent:60},{at:200,remaining_percent:0}];
const before=JSON.stringify(readings);
const curve=env.path(readings,t=>t,p=>p);
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
assert.match(env.path(ledgerTrend,t=>t,p=>p),/ C /,'Brave display uses the same smooth path renderer');
assert.equal(env.path([],t=>t,p=>p),'');
assert.equal(env.path(readings.slice(0,1),t=>t,p=>p),'M 0 100');
console.log('PASS shared smooth rendering preserves knot bounds, flat intervals and interpolated milestone positions');
