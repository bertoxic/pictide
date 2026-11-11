const test = require('node:test');
const assert = require('node:assert/strict');

test('photographer registration contract requires valid username, email, and password >= 12 chars', () => {
  const validateRegistration = ({ username, email, password }) => {
    if (!username || !/^[A-Za-z0-9][A-Za-z0-9._-]{2,63}$/.test(username)) {
      return { valid: false, error: 'Invalid username format' };
    }
    if (!email || !/^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(email)) {
      return { valid: false, error: 'Invalid email address' };
    }
    if (!password || password.length < 12 || password.length > 200) {
      return { valid: false, error: 'Password must be between 12 and 200 characters' };
    }
    return { valid: true };
  };

  assert.equal(validateRegistration({ username: 'photog1', email: 'p@studio.com', password: 'securepass123456' }).valid, true);
  assert.equal(validateRegistration({ username: 'ab', email: 'p@studio.com', password: 'securepass123456' }).valid, false);
  assert.equal(validateRegistration({ username: 'photog1', email: 'notanemail', password: 'securepass123456' }).valid, false);
  assert.equal(validateRegistration({ username: 'photog1', email: 'p@studio.com', password: 'short' }).valid, false);
});

test('guest upload customization contract parses allow_guest_upload flag cleanly', () => {
  const parseCustomization = (input) => {
    const fields = {};
    for (const [k, v] of Object.entries(input)) {
      if (k === 'allow_guest_upload') {
        const n = Number(v);
        if (n === 0 || n === 1) fields[k] = n;
        else throw new Error('Invalid customization value');
      }
    }
    return fields;
  };

  assert.deepEqual(parseCustomization({ allow_guest_upload: 1 }), { allow_guest_upload: 1 });
  assert.deepEqual(parseCustomization({ allow_guest_upload: 0 }), { allow_guest_upload: 0 });
  assert.throws(() => parseCustomization({ allow_guest_upload: 2 }));
});

test('withdrawal request contracts enforce kobo amount formatting and 10-digit NUBAN accounts', () => {
  const validateWithdrawal = ({ amount, account, bank, name }) => {
    if (typeof amount !== 'number' || amount <= 0 || !Number.isInteger(amount)) {
      return false;
    }
    if (typeof account !== 'string' || !/^\d{10}$/.test(account)) {
      return false;
    }
    if (typeof bank !== 'string' || !/^\d{2,6}$/.test(bank)) {
      return false;
    }
    if (typeof name !== 'string' || name.trim().length === 0) {
      return false;
    }
    return true;
  };

  assert.equal(validateWithdrawal({ amount: 500000, account: '0123456789', bank: '058', name: 'John Doe' }), true);
  assert.equal(validateWithdrawal({ amount: -100, account: '0123456789', bank: '058', name: 'John Doe' }), false);
  assert.equal(validateWithdrawal({ amount: 500000, account: '12345', bank: '058', name: 'John Doe' }), false);
  assert.equal(validateWithdrawal({ amount: 500000, account: '0123456789', bank: '', name: 'John Doe' }), false);
});

test('guest photo tokens localStorage persistence serialization', () => {
  const galleryID = 'gal_test_123';
  const mockStorage = new Map();
  const saveGuestToken = (galId, token) => {
    const key = 'lumen-uploads-' + galId;
    const existing = JSON.parse(mockStorage.get(key) || '[]');
    existing.push(token);
    mockStorage.set(key, JSON.stringify(existing));
  };
  const getGuestTokens = (galId) => {
    return JSON.parse(mockStorage.get('lumen-uploads-' + galId) || '[]');
  };

  saveGuestToken(galleryID, 'tok_1');
  saveGuestToken(galleryID, 'tok_2');
  assert.deepEqual(getGuestTokens(galleryID), ['tok_1', 'tok_2']);
});
