# Auth Layers

## Overview

Three independent auth systems coexist:

| Layer | Scope | Mechanism |
|---|---|---|
| JWT | Dashboard `/api/*` routes | Bearer token, HS256 |
| L1 | CLOB auth endpoints `/clob/auth/*` | EIP-712 Ethereum signature |
| L2 | CLOB trading endpoints `/clob/order`, `/clob/orders`, `/clob/data/*` | HMAC-SHA256 over request components |

Public endpoints (proxy routes like `/clob/book`, `/clob/markets`) require no auth.

## Routing Logic

`CLOBAuthMiddleware` (`internal/auth/clob_middleware.go`) inspects the request path and applies the correct middleware:

```
/clob/auth/*      → L1Middleware
/clob/order       → L2Middleware  (POST)
/clob/orders      → L2Middleware  (POST, DELETE)
/clob/data/*      → L2Middleware
/clob/cancel-*    → L2Middleware
/clob/are-orders-scoring → L2Middleware
everything else   → pass-through (proxy targets)
```

## JWT (Dashboard)

- Issued on `/auth/login` and `/auth/register`
- Standard `Authorization: Bearer <token>` header
- Validated in `JWTMiddleware` → injects `user_id` into context via `auth.UserIDKey`
- 24-hour expiry

## L1 — EIP-712 Signature

Required headers:
```
POLY_ADDRESS    — Ethereum address (checksummed)
POLY_SIGNATURE  — EIP-712 signature over ClobAuth struct
POLY_TIMESTAMP  — Unix timestamp (seconds)
POLY_NONCE      — Integer nonce (defaults to "0" if omitted)
```

Validation (`auth.Service.VerifyL1Auth`):
1. Timestamp must be within ±300s of server time
2. Nonce tracked in `used_nonces` table (replay protection)
3. EIP-712 typed data: domain `CLOB`, type `ClobAuth { address, timestamp, nonce }`
4. Recovered address must match `POLY_ADDRESS`
5. User looked up by `eth_address`; auto-registers if not found

## L2 — HMAC-SHA256

Required headers:
```
POLY_ADDRESS     — Ethereum address
POLY_SIGNATURE   — base64(HMAC-SHA256(api_secret, message))
POLY_TIMESTAMP   — Unix timestamp (seconds)
POLY_API_KEY     — api_keys.id (UUID)
POLY_PASSPHRASE  — stored passphrase (bcrypt-verified)
```

Message construction:
```
message = timestamp + method + path + body
```

**Critical**: `path` is WITHOUT the `/clob` prefix. SDKs sign paths like `/order`, `/data/orders`. The L2 middleware strips `/clob` before verifying (`strings.TrimPrefix(path, "/clob")`).

Validation (`auth.Service.VerifyL2Auth`):
1. Look up API key by `POLY_API_KEY`
2. Verify passphrase
3. Verify timestamp within ±300s
4. Reconstruct message and verify HMAC

## Context Values

After successful auth, user identity is available via:
```go
auth.GetUserID(ctx)      // user UUID
auth.GetEthAddress(ctx)  // Ethereum address (L1/L2 only)
```

## Ethereum Wallet Generation

On registration, an Ethereum keypair is generated automatically:
- Private key encrypted with AES-256-GCM using `ENCRYPTION_KEY`
- Stored as `BYTEA` in `users.eth_private_key_encrypted`
- Public address stored in `users.eth_address`

This allows L1 auth to work without requiring users to bring their own wallet.
