const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');

function environment({r2Fails = false} = {}) {
  const calls = [];
  const toastNode = {textContent: '', classList: {add() {}, remove() {}}};
  const document = {querySelector() { return toastNode; }, addEventListener() {}};
  class FormData { constructor() { this.entries = []; } append(name, value) { this.entries.push([name, value]); } }
  const response = body => ({ok: true, status: 200, json: async () => body});
  const fetch = async (url, options = {}) => {
    calls.push({url, options});
    if (url === 'https://r2.example/upload') {
      if (r2Fails) throw new TypeError('CORS or network failure');
      return response({});
    }
    if (url.endsWith('/direct-upload')) return response({upload_id: 'job', url: 'https://r2.example/upload', content_type: 'application/octet-stream'});
    if (url === '/api/direct-uploads/job/complete') return response({ok: true});
    if (url === '/api/galleries/gallery/photos') return response({added: 1, errors: []});
    throw new Error('Unexpected request: ' + url);
  };
  const ctx = vm.createContext({document, FormData, fetch, window: {}, location: {search: ''}, URLSearchParams, Date, Set, Map, JSON, Math, Number, String, Array, Promise, setTimeout() {}, clearTimeout() {}});
  vm.runInContext(fs.readFileSync(path.join(__dirname, '../public/app.js'), 'utf8'), ctx);
  vm.runInContext("current={id:'gallery'};config={r2_uploads:true};refresh=async()=>{};openGallery=async()=>{}",ctx);
  return {ctx, calls, toastNode};
}

test('large photo goes directly to R2 before completion', async () => {
  const {ctx, calls} = environment();
  ctx.input = {files: [{name: 'large.png', size: 6_000_000}], value: 'selected'};
  await vm.runInContext('uploadPhotos(input)', ctx);
  assert.deepEqual(calls.map(call => call.url), ['/api/galleries/gallery/direct-upload', 'https://r2.example/upload', '/api/direct-uploads/job/complete']);
  assert.equal(calls[1].options.method, 'PUT');
  assert.equal(ctx.input.value, '');
});

test('small photo falls back if the browser cannot reach R2', async () => {
  const {ctx, calls, toastNode} = environment({r2Fails: true});
  ctx.input = {files: [{name: 'small.png', size: 1000}], value: 'selected'};
  await vm.runInContext('uploadPhotos(input)', ctx);
  assert.deepEqual(calls.map(call => call.url), ['/api/galleries/gallery/direct-upload', 'https://r2.example/upload', '/api/galleries/gallery/photos']);
  assert.match(toastNode.textContent, /R2 CORS needs attention/);
});

test('large photo reports the CORS problem rather than using the limited fallback', async () => {
  const {ctx, calls, toastNode} = environment({r2Fails: true});
  ctx.input = {files: [{name: 'large.png', size: 6_000_000}], value: 'selected'};
  await vm.runInContext('uploadPhotos(input)', ctx);
  assert.deepEqual(calls.map(call => call.url), ['/api/galleries/gallery/direct-upload', 'https://r2.example/upload']);
  assert.match(toastNode.textContent, /CORS/);
});
