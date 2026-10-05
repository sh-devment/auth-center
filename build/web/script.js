// ── tile selection ────────────────────────────────────────────────────────

let activeMethod = null;

function selectMethod(method) {
  const prev = activeMethod;

  document.querySelectorAll('.tile').forEach(t => t.classList.remove('active'));
  document.querySelectorAll('.section').forEach(s => s.classList.remove('open'));

  // google has no section — go straight to the oauth redirect.
  // must return before the section lookup below, there is no #section-google.
  if (method === 'google') {
    document.getElementById('tile-google').classList.add('active');
    const r = window.REDIRECT_URL || '';
    window.location.href = '/google/login' + (r ? '?redirect=' + encodeURIComponent(r) : '');
    return;
  }

  if (prev === method) {
    activeMethod = null;
    return;
  }

  activeMethod = method;
  document.getElementById(`tile-${method}`).classList.add('active');
  document.getElementById(`section-${method}`).classList.add('open');

  if (method === 'tg') startSession();
}

// ── shared ────────────────────────────────────────────────────────────────

function showResult(type, text) {
  const el = document.getElementById('result');
  el.className = 'result ' + type;
  el.textContent = text;
}

function lockAll() {
  document.querySelectorAll('.tile').forEach(t => t.disabled = true);
}

function navigateWithCode(redirectUrl, code) {
  const sep = redirectUrl.includes('?') ? '&' : '?';
  window.location.href = `${redirectUrl}${sep}code=${code}`;
}

// ── telegram QR ───────────────────────────────────────────────────────────

// How long to keep listening for a login. auth-center keeps a session for five
// minutes (sessionTTL), so anything shorter here stops listening while the link
// the user is holding still works — they finish in Telegram and this tab has
// already given up. Matching the server is the honest number.
const SESSION_TTL_MS = 5 * 60 * 1000;

let pollInterval = null;
let pollTimeout  = null;

function sessionExpired() {
  clearInterval(pollInterval);
  clearTimeout(pollTimeout);
  document.getElementById('qr-area').classList.add('hidden');
  document.getElementById('tg-refresh').style.display = 'block';
}

async function startSession() {
  clearInterval(pollInterval);
  clearTimeout(pollTimeout);

  document.getElementById('qr-area').classList.remove('hidden');
  document.getElementById('tg-refresh').style.display = 'none';
  document.getElementById('result').className = 'result';

  const { token, qr, url } = await fetch('/qr-session', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ redirect: window.REDIRECT_URL || '' }),
  }).then(r => r.json());

  document.getElementById('qr-img').src = `data:image/png;base64,${qr}`;
  document.getElementById('open-btn').href = url;

  pollInterval = setInterval(() => poll(token), 2000);

  pollTimeout = setTimeout(sessionExpired, SESSION_TTL_MS);
}

async function poll(token) {
  const res  = await fetch(`/poll/${token}`);
  const data = await res.json();

  // A gone session answers 404 {"error":"expired"} — there is no status field
  // to read, so the response code is what says to stop.
  if (res.status === 404) {
    sessionExpired();
    return;
  }

  if (data.status === 'authenticated') {
    clearInterval(pollInterval);
    clearTimeout(pollTimeout);

    if (data.redirect && data.code) {
      navigateWithCode(data.redirect, data.code);
      return;
    }

    lockAll();
    const u    = data.user;
    const name = [u.first_name, u.last_name].filter(Boolean).join(' ');
    showResult('success',
      `telegram\n` +
      `${name}${u.username ? ' (@' + u.username + ')' : ''}\n` +
      `id: ${u.id}`
    );
  }

}


// ── solana ────────────────────────────────────────────────────────────────

const IS_MOBILE  = /Android|iPhone|iPad/i.test(navigator.userAgent);
const IS_ANDROID = /Android/i.test(navigator.userAgent);

// base58 encode — needed for Wallet Standard (returns raw Uint8Array public keys)
const B58 = '123456789ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz';
function toBase58(bytes) {
  let n = 0n;
  for (const b of bytes) n = (n << 8n) | BigInt(b);
  let s = '';
  while (n > 0n) { s = B58[Number(n % 58n)] + s; n /= 58n; }
  for (const b of bytes) { if (b !== 0) break; s = '1' + s; }
  return s;
}

function uint8ToBase64(bytes) {
  let bin = '';
  for (let i = 0; i < bytes.length; i++) bin += String.fromCharCode(bytes[i]);
  return btoa(bin);
}

async function fetchNonce() {
  return fetch('/solana/nonce', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
  }).then(r => r.json());
}

// ── module preloading ─────────────────────────────────────────────────────
//
// Modules are loaded at page startup, BEFORE any user gesture.
// Chrome on Android has a tight user-gesture window for launching wallet
// intents — if we import() inside the click handler it will expire before
// MWA gets a chance to open the wallet app.

let _wsGetWallets = null; // @wallet-standard/app
let _mwaTransact  = null; // @solana-mobile/mobile-wallet-adapter-protocol

;(async () => {
  const jobs = [
    import('https://esm.sh/@wallet-standard/app@1').catch(() => null),
    IS_ANDROID
      ? import('https://esm.sh/@solana-mobile/mobile-wallet-adapter-protocol@2').catch(() => null)
      : Promise.resolve(null),
  ];
  const [ws, mwa] = await Promise.all(jobs);
  if (ws)  _wsGetWallets = ws.getWallets;
  if (mwa) _mwaTransact  = mwa.transact;
})();

// ── path 1: injected provider (desktop extension / wallet in-app browser) ─

function getInjectedProvider() {
  if (window.phantom?.solana?.isPhantom) return { wallet: window.phantom.solana, name: 'Phantom' };
  if (window.solflare?.isSolflare)        return { wallet: window.solflare,       name: 'Solflare' };
  if (window.solana)                      return { wallet: window.solana,          name: 'Solana Wallet' };
  return null;
}

async function signWithInjected(injected, btn) {
  await injected.wallet.connect();
  const publicKey = injected.wallet.publicKey.toBase58();

  btn.textContent = 'signing...';
  const { nonce, token, error } = await fetchNonce();
  if (error) throw new Error(error);

  const signed = await injected.wallet.signMessage(new TextEncoder().encode(nonce), 'utf8');
  return {
    publicKey,
    signature:   uint8ToBase64(new Uint8Array(signed.signature)),
    nonce,
    nonceToken:  token,
    walletName:  injected.name,
  };
}

// ── path 2: wallet standard ───────────────────────────────────────────────
//
// Uses the preloaded module. Waits briefly for wallets that register async
// (Seeker / other Android wallets may register a moment after page load).
// Short timeout on Android so we don't eat the user-gesture window before MWA.

async function findStandardWallet() {
  if (!_wsGetWallets) return null;

  const { get, on } = _wsGetWallets();
  const check = () => get().find(w =>
    w.chains.some(c => c.startsWith('solana:')) &&
    'standard:connect'   in w.features &&
    'solana:signMessage' in w.features
  ) || null;

  const found = check();
  if (found) return found;

  // Wait for late-registering wallets.
  // Android: short wait to preserve gesture context for MWA fallback.
  const waitMs = IS_ANDROID ? 300 : 1000;
  return new Promise(resolve => {
    const t = setTimeout(() => resolve(null), waitMs);
    on('register', () => {
      const w = check();
      if (w) { clearTimeout(t); resolve(w); }
    });
  });
}

async function signWithStandard(wallet, btn) {
  await wallet.features['standard:connect'].connect();
  const account = wallet.accounts[0];
  if (!account) throw new Error('wallet connected but no account');
  const publicKey = toBase58(account.publicKey);

  btn.textContent = 'signing...';
  const { nonce, token, error } = await fetchNonce();
  if (error) throw new Error(error);

  const [result] = await wallet.features['solana:signMessage'].signMessage({
    account,
    message: new TextEncoder().encode(nonce),
  });
  return {
    publicKey,
    signature:  uint8ToBase64(result.signature),
    nonce,
    nonceToken: token,
    walletName: wallet.name,
  };
}

// ── path 3: mobile wallet adapter (android — saga 2 / seed vault) ─────────
//
// Uses the preloaded module (no import delay at click time).
// MWA opens the wallet app via Android intent over a local WebSocket —
// works from Chrome or any Android browser without an extension.
//
// Signed payload format: message_bytes || signature_bytes
// → signature = last 64 bytes

async function signWithMWA(btn) {
  if (!_mwaTransact) throw new Error('MWA module not loaded');

  // Pre-fetch nonce BEFORE opening transact so there are zero HTTP calls
  // inside the MWA session. Phantom (and some other wallets) time out if the
  // WebSocket sits idle while we wait for a network round-trip.
  const { nonce, token, error: nonceErr } = await fetchNonce();
  if (nonceErr) throw new Error(nonceErr);

  const nonceBytes  = new TextEncoder().encode(nonce);
  const nonceBase64 = btoa(String.fromCharCode(...nonceBytes));

  return await _mwaTransact(async (wallet) => {
    btn.textContent = 'opening wallet...';

    const authResult = await wallet.authorize({
      chain: 'solana:mainnet',
      identity: {
        name: 'Auth Center',
        uri:  window.location.origin,
        icon: '/favicon.svg',
      },
    });

    const rawAddress = authResult.accounts[0]?.address;
    if (!rawAddress) throw new Error(`no address in authResult: ${JSON.stringify(authResult.accounts)}`);

    // MWA may return base64url-encoded public key instead of base58.
    const isBase58 = /^[123456789ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz]+$/.test(rawAddress);
    let publicKey;
    if (isBase58) {
      publicKey = rawAddress;
    } else {
      const b64     = rawAddress.replace(/-/g, '+').replace(/_/g, '/').padEnd(Math.ceil(rawAddress.length / 4) * 4, '=');
      const pkBytes = Uint8Array.from(atob(b64), c => c.charCodeAt(0));
      publicKey = toBase58(pkBytes);
    }

    btn.textContent = 'signing...';

    const { signed_payloads } = await wallet.signMessages({
      addresses: [rawAddress],
      payloads:  [nonceBase64],
    });

    // signed_payloads are base64 strings; decoded = message_bytes || signature (last 64 bytes)
    const signedBytes = Uint8Array.from(atob(signed_payloads[0]), c => c.charCodeAt(0));
    const sigBytes    = signedBytes.slice(-64);

    return {
      publicKey,
      signature:  uint8ToBase64(sigBytes),
      nonce,
      nonceToken: token,
      walletName: 'Seeker / MWA',
    };
  });
}

// ── path 4: no wallet on mobile — open page inside Phantom's in-app browser ─

function showPhantomLink() {
  const url  = window.location.href;
  const link = `https://phantom.app/ul/browse/${encodeURIComponent(url)}?ref=${encodeURIComponent(window.location.origin)}`;
  const el   = document.getElementById('phantom-link');
  el.href    = link;
  el.style.display = 'inline-block';
}

// ── connect wallet ────────────────────────────────────────────────────────

async function connectWallet() {
  const btn       = document.getElementById('solana-btn');
  const noWallet  = document.getElementById('no-wallet');
  const phantomEl = document.getElementById('phantom-link');

  noWallet.classList.remove('visible');
  phantomEl.style.display = 'none';
  btn.classList.remove('invalid');
  btn.disabled    = true;
  btn.textContent = 'connecting...';

  try {
    let result = null;

    // 1. injected provider (desktop extension / wallet in-app browser)
    const injected = getInjectedProvider();
    if (injected) {
      result = await signWithInjected(injected, btn);
    }

    // 2. wallet standard
    if (!result) {
      btn.textContent = 'looking for wallet...';
      const stdWallet = await findStandardWallet();
      if (stdWallet) result = await signWithStandard(stdWallet, btn);
    }

    // 3. MWA — android only (saga 2, seed vault, phantom android in chrome)
    if (!result && IS_ANDROID) {
      result = await signWithMWA(btn);
    }

    // 4. no wallet found (iOS without Phantom, desktop without extension)
    if (!result) {
      btn.classList.add('invalid');
      btn.disabled    = false;
      btn.textContent = 'connect wallet';
      IS_MOBILE ? showPhantomLink() : noWallet.classList.add('visible');
      return;
    }

    // submit
    const data = await fetch('/solana/auth', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        public_key:  result.publicKey,
        signature:   result.signature,
        nonce:       result.nonce,
        nonce_token: result.nonceToken,
        redirect:    window.REDIRECT_URL || '',
      }),
    }).then(r => r.json());

    if (data.ok) {
      if (data.redirect && data.code) {
        navigateWithCode(data.redirect, data.code);
        return;
      }
      lockAll();
      showResult('success', `solana (${result.walletName})\n${data.public_key}`);
      btn.textContent = 'connected';
    } else {
      throw new Error(data.error || 'auth failed');
    }

  } catch (err) {
    showResult('error', err.message);
    btn.disabled    = false;
    btn.textContent = 'connect wallet';
  }
}
