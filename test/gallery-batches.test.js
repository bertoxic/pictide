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
test('gallery detail exposes a compact filter control that opens and closes',()=>{
 const {run,ctx,nodes}=environment();
 run("current={id:'gallery',name:'Demo',status:'Draft',price:0,photos:[]}; view='detail'");
 const html=run('detail()');
 assert.match(html,/aria-label="Filter and sort photos"/);
 assert.match(html,/id="owner-photo-filters"/);
 const panel=nodes.get('#owner-photo-filters')||run("document.querySelector('#owner-photo-filters')");
 let visible=false;panel.classList.toggle=(name,value)=>{if(name==='is-open')visible=value};
 const button={value:'false',getAttribute(){return this.value},setAttribute(name,value){this.value=value}};ctx.button=button;
 run('toggleOwnerPhotoFilters(button)');assert.equal(visible,true);assert.equal(button.value,'true');
 run('toggleOwnerPhotoFilters(button)');assert.equal(visible,false);assert.equal(button.value,'false');
});
test('guest gallery retains filters behind an accessible mobile control',async()=>{
 const {run,ctx,nodes}=environment();
 ctx.location.search='?gallery=demo';
 ctx.fetch=async()=>({ok:true,status:200,json:async()=>({id:'demo',name:'Demo',status:'Published',price:100,photos:[],share_color:'#4f5b2a'})});
 await run('renderGuest()');
 const html=nodes.get('#app').innerHTML;
 assert.match(html,/aria-label="Filter and sort photographs"/);
 assert.match(html,/id="guest-photo-filters"/);
 assert.match(html,/aria-label="Filter photographs"/);
 assert.match(html,/aria-label="Sort photographs"/);
 const panel=run("document.querySelector('#guest-photo-filters')");let visible=false;
 panel.classList.toggle=(name,value)=>{if(name==='is-open')visible=value};
 ctx.button={value:'false',getAttribute(){return this.value},setAttribute(name,value){this.value=value}};
 run('toggleGuestPhotoFilters(button)');assert.equal(visible,true);assert.equal(ctx.button.value,'true');
 run('toggleGuestPhotoFilters(button)');assert.equal(visible,false);assert.equal(ctx.button.value,'false');
});
