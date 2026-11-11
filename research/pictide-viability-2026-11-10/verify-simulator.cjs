// Check the fragment's calculations and event handlers with a minimal DOM.
// This checks behavior, not browser layout or rendered accessibility.
const fs = require('node:fs');
const vm = require('node:vm');
const assert = require('node:assert/strict');
const fragment = fs.readFileSync(process.argv[2], 'utf8');
const elements = new Map();
for (const match of fragment.matchAll(/<[^>]+\bid="([^"]+)"[^>]*>/g)) {
  const tag = match[0];
  const attr = name => tag.match(new RegExp('\\b'+name+'="([^"]*)"'))?.[1];
  elements.set(match[1], {
    value:attr('value') || '', min:attr('min'), max:attr('max'), textContent:'',
    listeners:{}, addEventListener(name, fn){this.listeners[name]=fn;}
  });
}
const root = elements.get('pictide-economics');
root.querySelector = selector => {
  const found = elements.get(selector.slice(1));
  if (!found) throw new Error('Missing element: '+selector);
  return found;
};
const get = name => elements.get('pe-'+name);
get('preset').value='working';
get('bearer').value='platform';
const script = fragment.match(/<script>([\s\S]*?)<\/script>/)[1];
let saved;
vm.runInNewContext(script, {
  document:{getElementById:id => elements.get(id)},
  window:{addEventListener(){},openai:{setWidgetState(state){saved=state;return Promise.resolve();}}}
});
assert.equal(get('contribution').textContent,'₦850');
assert.equal(get('break-even').textContent,'883');
assert.equal(get('result').textContent,'−₦665,000');
get('preset').value='upside';
get('preset').listeners.change();
assert.equal(get('break-even').textContent,'109');
assert.equal(get('result').textContent,'−₦60,000');
get('preset').value='downside';
get('preset').listeners.change();
assert.equal(get('break-even').textContent,'No positive margin');
assert.equal(get('result').textContent,'−₦867,500');
get('preset').value='working';
get('preset').listeners.change();
get('bearer').value='seller';
get('bearer').listeners.change();
assert.equal(get('break-even').textContent,'334');
assert.equal(saved.modelContent.contributionPerActive,2250);
get('fee').value=0;
get('fee').listeners.input();
assert.equal(get('break-even').textContent,'No positive margin');
assert.equal(get('preset').value,'custom');
assert(!fragment.includes('\\"'), 'Escaped quotes in markup');
console.log('Passed: initial state, all scenarios, fee bearer, custom fee, state saving and element references.');
