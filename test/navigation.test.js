const test=require('node:test');
const assert=require('node:assert/strict');
const fs=require('node:fs');
const vm=require('node:vm');
const path=require('node:path');

test('wallet navigation renders a loading state and starts requests together',async()=>{
  const calls=[],pending=[];const nodes=new Map();
  const document={querySelector(s){if(!nodes.has(s))nodes.set(s,{innerHTML:'',textContent:'',classList:{add(){},remove(){}}});return nodes.get(s)},addEventListener(){}};
  const fetch=url=>{calls.push(url);return new Promise(resolve=>pending.push({url,resolve}))};
  const ctx=vm.createContext({document,fetch,window:{},location:{search:''},URLSearchParams,Date,Set,Map,JSON,Math,Number,String,Array,Promise,setTimeout(){},clearTimeout(){}});
  vm.runInContext(fs.readFileSync(path.join(__dirname,'../public/app.js'),'utf8'),ctx);
  vm.runInContext("let galleryRequest=0,orderPageRequest=0,galleryPageOffset=0,sharePageOffset=0,orderPageOffset=0,orderFilters=null,galleryPageData=null,orderPageData=null;function galleryPageURL(){return '/api/galleries'} function orderPageURL(){return '/api/orders'}",ctx);
  const task=vm.runInContext("navigate('wallet')",ctx);
  assert.match(nodes.get('#app').innerHTML,/Loading/);
  assert.deepEqual(calls,['/api/galleries','/api/orders','/api/settings','/api/wallet']);
  for(const p of pending){const data=p.url==='/api/galleries'?{items:[],total:0}:p.url==='/api/orders'?{items:[],insights:{gross:0}}:p.url==='/api/wallet'?{earned:0,available:0,withdrawals:[],configured:false}:{studio_name:'Studio',default_price:0};p.resolve({ok:true,status:200,json:async()=>data})}
  await task;assert.match(nodes.get('#app').innerHTML,/<h1>Wallet<\/h1>/);
});
