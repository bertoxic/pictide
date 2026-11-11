const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const vm = require('node:vm');

function setup(action) {
  const listeners = {};
  const classes = new Set();
  const attrs = {};
  const button = {disabled:false, classList:{add:x=>classes.add(x),remove:x=>classes.delete(x)}, setAttribute:(k,v)=>attrs[k]=v, removeAttribute:k=>delete attrs[k]};
  const document = {addEventListener:(type,fn)=>{listeners[type]=fn}};
  const window = {action};
  const context = vm.createContext({window,document,queueMicrotask,Promise});
  vm.runInContext(fs.readFileSync('public/button-loading.js','utf8'),context);
  return {button,classes,attrs,listeners,window};
}

test('async click stays busy until the action settles and recovers on failure', async () => {
  let reject;
  const ui = setup(async () => new Promise((resolve, fail) => { reject=fail; }));
  ui.listeners.click({target:{closest:()=>ui.button}});
  const result = ui.window.action();
  assert.equal(ui.button.disabled,true);
  assert.equal(ui.classes.has('is-loading'),true);
  assert.equal(ui.attrs['aria-busy'],'true');
  reject(new Error('failed'));
  await assert.rejects(result,/failed/);
  assert.equal(ui.button.disabled,false);
  assert.equal(ui.classes.has('is-loading'),false);
  assert.equal(ui.attrs['aria-busy'],undefined);
});

test('form submission uses the submitter and blocks duplicate submission', async () => {
  let finish;
  const ui = setup(async () => new Promise(resolve => { finish=resolve; }));
  const form = {querySelector:()=>ui.button};
  ui.listeners.submit({submitter:ui.button,target:form});
  const result = ui.window.action();
  let prevented=false, stopped=false;
  ui.listeners.submit({submitter:ui.button,target:form,preventDefault(){prevented=true},stopImmediatePropagation(){stopped=true}});
  assert.equal(prevented,true);
  assert.equal(stopped,true);
  finish();
  await result;
  assert.equal(ui.button.disabled,false);
});
