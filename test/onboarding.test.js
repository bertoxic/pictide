const test=require('node:test');
const assert=require('node:assert/strict');
const fs=require('node:fs');
const path=require('node:path');
const vm=require('node:vm');

test('returning signed-out visitors see the landing page and its account actions',()=>{
  const app={innerHTML:''};let focused=false,signIns=0;
  const ctx=vm.createContext({app,window:{},localStorage:{getItem(){return '1'}},document:{querySelector(){return {focus(){focused=true}}},addEventListener(){}},signInScreen(){signIns++},registerScreen(){}});
  vm.runInContext(fs.readFileSync(path.join(__dirname,'../public/onboarding.js'),'utf8'),ctx);
  ctx.window.maybeShowPictideIntro();
  assert.match(app.innerHTML,/onclick="signInScreen\(\)"/);
  assert.match(app.innerHTML,/onclick="registerScreen\(\)"/);
  assert.match(app.innerHTML,/Your work deserves a beautiful home/);
  assert.match(app.innerHTML,/onclick="movePictideIntro\(1\)">Continue/);
  assert.equal(focused,true);
  ctx.window.movePictideIntro(1);
  assert.match(app.innerHTML,/From your camera to their hands/);
  assert.match(app.innerHTML,/onclick="signInScreen\(\)"/);
  assert.match(app.innerHTML,/onclick="movePictideIntro\(1\)">Continue/);
  ctx.window.movePictideIntro(1);
  assert.match(app.innerHTML,/The details, taken care of/);
  assert.match(app.innerHTML,/onclick="movePictideIntro\(1\)">Get started/);
  ctx.window.movePictideIntro(1);
  assert.equal(signIns,1);
});
