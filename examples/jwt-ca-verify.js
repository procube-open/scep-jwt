#!/usr/bin/env node

const { X509Certificate, verify: verifySignature, randomBytes } = require('node:crypto');

const baseUrl = process.env.SCEP_BASE_URL || 'http://localhost:3000';
const timeoutMs = Number(process.env.SCEP_TIMEOUT_MS || '10000');

function base64UrlDecode(value) {
  const normalized = value.replace(/-/g, '+').replace(/_/g, '/');
  const padded = normalized.padEnd(Math.ceil(normalized.length / 4) * 4, '=');
  return Buffer.from(padded, 'base64');
}

function base64UrlEncode(buffer) {
  return Buffer.from(buffer)
    .toString('base64')
    .replace(/\+/g, '-')
    .replace(/\//g, '_')
    .replace(/=+$/g, '');
}

function createTimeoutSignal(ms) {
  return AbortSignal.timeout(ms);
}

async function request(method, path, body, responseType = 'json', overrideBaseUrl = baseUrl) {
  const url = new URL(path, overrideBaseUrl);
  const headers = {};
  let payload;
  if (body !== undefined) {
    headers['content-type'] = 'application/json';
    payload = JSON.stringify(body);
  }

  const response = await fetch(url, {
    method,
    headers,
    body: payload,
    signal: createTimeoutSignal(timeoutMs),
  });

  if (!response.ok) {
    const text = await response.text();
    throw new Error(`${method} ${url.pathname} failed: ${response.status} ${text}`.trim());
  }

  if (responseType === 'buffer') {
    return Buffer.from(await response.arrayBuffer());
  }
  if (responseType === 'text') {
    return response.text();
  }
  if (response.status === 204) {
    return null;
  }
  const text = await response.text();
  return text ? JSON.parse(text) : null;
}

async function createClient(uid, origin) {
  await request('POST', '/admin/api/client/add', {
    uid,
    origin,
    attributes: { source: 'examples/jwt-ca-verify.js' },
  });
}

async function createJWTSecret(uid, secret) {
  await request('POST', '/admin/api/jwt/secret/create', {
    target: uid,
    secret,
    available_period: '30m',
    pending_period: '1h',
  });
}

async function issueJWT(uid, secret) {
  return request('POST', '/api/jwt/issue', { uid, secret });
}

async function fetchCACertificate(overrideBaseUrl) {
  return request('GET', '/scep?operation=GetCACert', undefined, 'buffer', overrideBaseUrl);
}

function parseJWT(token) {
  const parts = token.split('.');
  if (parts.length !== 3) {
    throw new Error('JWT must have exactly 3 parts');
  }

  const [encodedHeader, encodedPayload, encodedSignature] = parts;
  const header = JSON.parse(base64UrlDecode(encodedHeader).toString('utf8'));
  const payload = JSON.parse(base64UrlDecode(encodedPayload).toString('utf8'));
  const signature = base64UrlDecode(encodedSignature);

  return {
    encodedHeader,
    encodedPayload,
    encodedSignature,
    header,
    payload,
    signature,
    signingInput: Buffer.from(`${encodedHeader}.${encodedPayload}`, 'utf8'),
  };
}

function verifyJWT(token, caCertDer, options = {}) {
  const { expectedAudience, expectedSubject, now = Math.floor(Date.now() / 1000) } = options;
  const parsed = parseJWT(token);

  if (parsed.header.alg !== 'RS256') {
    throw new Error(`Unexpected alg: ${parsed.header.alg}`);
  }

  const cert = new X509Certificate(caCertDer);
  const ok = verifySignature('RSA-SHA256', parsed.signingInput, cert.publicKey, parsed.signature);
  if (!ok) {
    throw new Error('JWT signature verification failed');
  }

  if (typeof parsed.payload.exp !== 'number') {
    throw new Error('exp claim is missing or invalid');
  }
  if (parsed.payload.exp <= now) {
    throw new Error('JWT is expired');
  }
  if (typeof parsed.payload.nbf === 'number' && parsed.payload.nbf > now) {
    throw new Error('JWT is not valid yet');
  }
  if (expectedAudience && parsed.payload.aud !== expectedAudience) {
    throw new Error(`aud mismatch: expected ${expectedAudience}, got ${parsed.payload.aud}`);
  }
  if (expectedSubject && parsed.payload.sub !== expectedSubject) {
    throw new Error(`sub mismatch: expected ${expectedSubject}, got ${parsed.payload.sub}`);
  }

  return parsed.payload;
}

function tamperJWT(token) {
  const parsed = parseJWT(token);
  const payload = {
    ...parsed.payload,
    aud: `${parsed.payload.aud}-tampered`,
  };
  return [
    parsed.encodedHeader,
    base64UrlEncode(Buffer.from(JSON.stringify(payload))),
    parsed.encodedSignature,
  ].join('.');
}

async function expectFailure(name, fn, matcher) {
  try {
    await fn();
  } catch (error) {
    const message = error instanceof Error ? error.message : String(error);
    if (matcher && !matcher.test(message)) {
      throw new Error(`${name}: unexpected error: ${message}`);
    }
    console.log(`PASS ${name}: ${message}`);
    return;
  }
  throw new Error(`${name}: expected failure but succeeded`);
}

async function main() {
  const uid = `jwt-js-${Date.now()}`;
  const secret = randomBytes(12).toString('hex');
  const origin = `https://auth.example.local/${uid}`;

  console.log(`BASE_URL ${baseUrl}`);
  console.log(`UID ${uid}`);

  console.log('STEP 1 create client');
  await createClient(uid, origin);

  console.log('STEP 2 create JWT secret');
  await createJWTSecret(uid, secret);

  console.log('STEP 3 issue JWT');
  const issued = await issueJWT(uid, secret);
  console.log(`TOKEN ${issued.token}`);
  console.log(`EXPIRES_AT ${issued.expires_at}`);

  console.log('STEP 4 fetch CA certificate from SCEP API');
  const caCertDer = await fetchCACertificate();
  const caCert = new X509Certificate(caCertDer);
  console.log(`CA_SUBJECT ${caCert.subject}`);

  console.log('STEP 5 verify JWT with CA public key');
  const claims = verifyJWT(issued.token, caCertDer, {
    expectedAudience: origin,
    expectedSubject: uid,
  });
  console.log(`PASS valid token: sub=${claims.sub} aud=${claims.aud} exp=${claims.exp}`);

  console.log('STEP 6 verify error cases');
  await expectFailure(
    'tampered JWT',
    async () => verifyJWT(tamperJWT(issued.token), caCertDer, { expectedAudience: origin, expectedSubject: uid }),
    /signature verification failed/
  );

  await expectFailure(
    'aud mismatch',
    async () => verifyJWT(issued.token, caCertDer, { expectedAudience: `${origin}/unexpected`, expectedSubject: uid }),
    /aud mismatch/
  );

  const validClaims = parseJWT(issued.token).payload;
  await expectFailure(
    'expired JWT',
    async () => verifyJWT(issued.token, caCertDer, {
      expectedAudience: origin,
      expectedSubject: uid,
      now: Number(validClaims.exp) + 1,
    }),
    /expired/
  );

  await expectFailure(
    'CA certificate fetch failure',
    async () => fetchCACertificate('http://127.0.0.1:3999'),
    /fetch failed|ECONNREFUSED|connect/
  );

  console.log('RESULT all checks passed');
}

main().catch((error) => {
  console.error('FAILED', error instanceof Error ? error.message : error);
  process.exitCode = 1;
});