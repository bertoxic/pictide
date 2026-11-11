/* Connect async actions to the button that initiated them. */
(function () {
  let actionButton = null;
  const pending = new WeakMap();

  function remember(button) {
    actionButton = button;
    queueMicrotask(() => { if (actionButton === button) actionButton = null; });
  }

  document.addEventListener('click', event => {
    const button = event.target.closest('button');
    if (button && !button.disabled) remember(button);
  }, true);

  document.addEventListener('submit', event => {
    const button = event.submitter || event.target.querySelector('button[type="submit"], button:not([type])');
    if (button && pending.has(button)) {
      event.preventDefault();
      event.stopImmediatePropagation();
      return;
    }
    if (button && !button.disabled) remember(button);
  }, true);

  function begin(button) {
    const count = pending.get(button) || 0;
    pending.set(button, count + 1);
    if (count) return;
    button.disabled = true;
    button.classList.add('is-loading');
    button.setAttribute('aria-busy', 'true');
  }

  function end(button) {
    const count = pending.get(button);
    if (count > 1) { pending.set(button, count - 1); return; }
    pending.delete(button);
    button.disabled = false;
    button.classList.remove('is-loading');
    button.removeAttribute('aria-busy');
  }

  for (const name of Object.getOwnPropertyNames(window)) {
    const descriptor = Object.getOwnPropertyDescriptor(window, name);
    const fn = descriptor && descriptor.value;
    if (typeof fn !== 'function' || fn.constructor.name !== 'AsyncFunction' || !descriptor.writable || /\[native code\]/.test(Function.prototype.toString.call(fn))) continue;
    window[name] = function (...args) {
      const button = actionButton;
      if (!button) return fn.apply(this, args);
      begin(button);
      try {
        return Promise.resolve(fn.apply(this, args)).finally(() => end(button));
      } catch (error) {
        end(button);
        throw error;
      }
    };
  }
})();
