const test = require('node:test');
const assert = require('node:assert/strict');
const {filteredOrders, orderInsights, localOrderDay} = require('../public/order-insights.js');
const time = (day) => new Date(2026, 9, day, 12).toISOString();
const rows = [
 {id:'1',gallery_id:'a',gallery_name:'Wedding',created:time(1),status:'Paid',amount:10000,platform_fee:1000},
 {id:'2',gallery_id:'a',gallery_name:'Wedding',created:time(2),status:'Pending',amount:5000,platform_fee:500},
 {id:'3',gallery_id:'b',gallery_name:'Portraits',created:time(3),status:'Paid',amount:20000,platform_fee:2000},
 {id:'4',gallery_id:'a',gallery_name:'Wedding',created:time(3),status:'Paid',amount:5000,platform_fee:500}
];
test('date boundaries, status and gallery filters combine in local time',()=>{
 assert.equal(localOrderDay(time(2)),'2026-10-02');
 assert.deepEqual(filteredOrders(rows,{from:'2026-10-02',to:'2026-10-03',gallery:'a',status:'Pending'}).map(x=>x.id),['2']);
 assert.deepEqual(filteredOrders(rows,{from:'2026-10-03',to:'2026-10-02',gallery:'all',status:'all'}),[]);
});
test('insights count only paid sales and reflect the filtered set',()=>{
 let x=orderInsights(rows);assert.equal(x.gross,35000);assert.equal(x.net,31500);assert.equal(x.paid,3);assert.equal(x.pending,1);assert.equal(x.average,11667);assert.deepEqual(x.days,[['2026-10-01',10000],['2026-10-03',25000]]);assert.equal(x.galleries[0].name,'Portraits');
 x=orderInsights(filteredOrders(rows,{from:'',to:'',gallery:'a',status:'all'}));assert.equal(x.gross,15000);assert.equal(x.pending,1);assert.equal(x.galleries.length,1);
});
