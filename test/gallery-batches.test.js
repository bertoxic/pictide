const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const vm = require('node:vm');
const path = require('node:path');
function environment(){
 const nodes=new Map();const node=()=>({innerHTML:'',hidden:true,offsetWidth:200,classList:{add(){},remove(){},toggle(){}},isConnected:true});
 const document={documentElement:{dataset:{}},querySelector(s){if(!nodes.has(s))nodes.set(s,node());return nodes.get(s)},querySelectorAll(){return []},addEventListener(){}};
 const localStorage={getItem(){return null},setItem(){}};const ctx=vm.createContext({document,localStorage,window:{},location:{search:'',origin:'http://example.test'},URLSearchParams,Date,Set,Map,JSON,Math,Number,String,Array,Promise,setTimeout,clearTimeout,console});
 for(const f of ['app.js','enhancements.js','order-insights.js'])vm.runInContext(fs.readFileSync(path.join(__dirname,'../public',f),'utf8'),ctx,{filename:f});
 return {ctx,nodes,run:code=>vm.runInContext(code,ctx)};
}
test('guest photo batches keep selections and select only photos shown',()=>{
 const {run,nodes}=environment();run(`current={id:'gallery',price:100,photos:Array.from({length:125},(_,i)=>({id:'p'+i,name:'Photo '+i,created:'2026-01-01',price:i%2?null:0}))};guestSort='oldest';guestPhotoSignature='';updateGuestGrid()`);
 assert.equal((nodes.get('#guest-photos').innerHTML.match(/class="photo /g)||[]).length,60);
 assert.match(nodes.get('#guest-photo-more').innerHTML,/60 of 125/);
 run('selectAllVisible()');assert.equal(run('selected.size'),60);
 run('loadMoreGuestPhotos()');assert.equal((nodes.get('#guest-photos').innerHTML.match(/class="photo /g)||[]).length,120);assert.equal(run('selected.size'),60);
 run("guestQuery='Photo 124';updateGuestGrid()");assert.equal((nodes.get('#guest-photos').innerHTML.match(/class="photo /g)||[]).length,1);assert.equal(run('selected.size'),60);
});
test('owner photo batches keep filters and increase progressively',()=>{
 const {run,nodes}=environment();run(`current={id:'gallery',price:100,photos:Array.from({length:125},(_,i)=>({id:'p'+i,name:'Photo '+i,created:'2026-01-01',price:null}))};ownerPhotoSignature=JSON.stringify(['gallery','','all','newest']);updateOwnerPhotos()`);
 assert.equal((nodes.get('#owner-photos').innerHTML.match(/class="photo"/g)||[]).length,60);
 run('loadMoreOwnerPhotos()');assert.equal((nodes.get('#owner-photos').innerHTML.match(/class="photo"/g)||[]).length,120);
 run("photoQuery='Photo 124';updateOwnerPhotos()");assert.equal((nodes.get('#owner-photos').innerHTML.match(/class="photo"/g)||[]).length,1);
});
