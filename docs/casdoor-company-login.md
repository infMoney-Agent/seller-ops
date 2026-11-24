# Casdoor shared-company login

Updated 2026-11-23.

- Shared application: application_seller_ops (built-in).
- organization_wqynzf -> existing tenant 4 (Test), company owner user 19 (yijiangli).
- organization_2aw85q -> existing tenant 5 (Test2), company owner user 23 (test2). The test2 subject was explicitly rebound to the replacement Casdoor account; business IDs and history remain unchanged.
- Platform administration: /login/built-in. Only the platform administrator keeps Casdoor IsAdmin; company owners use Seller Ops membership owner and do not receive organization configuration permissions.
- Business entry: /api/auth/login. A shared base client lets Casdoor identify the unique username across organizations whose defaultApplication is application_seller_ops. Explicit organization routes and invitations remain supported.
- OIDC auto flow: an unverified owner only selects a database-allowlisted strict verifier; signature, issuer, exact base-client-org-owner audience, state, nonce, expiry and PKCE are all required before identity is accepted. The signed organization must match the existing membership tenant. Unknown organizations and built-in business sign-ins are rejected.
- Fresh registrations remain unbound until a valid matching-company invitation is accepted. Invitation forms preserve organization; the server validates it independently. Casdoor IsAdmin never grants business owner implicitly.
- Existing sessions were revoked during identity rebinding. Sign in again through the application instead of reusing a stale authorize URL.

## Account names

Casdoor SQLite has the custom seller_ops_user_name_unique expression index and insert/update triggers from apps/auth/casdoor/global-usernames.sql. Username uniqueness is global and case-insensitive for ASCII; display names may repeat. Admin is reserved by the existing platform account. Triggers reject duplicate creation/rename; the unique index covers concurrent writes. Keep this migration when deploying/upgrading Casdoor. A different identity database requires an equivalent migration. The Go auth database and screening business database remain separate.

## Adding another company

Create its Casdoor organization using the platform account and set defaultApplication to application_seller_ops. Create a tenant record and bind casdoor_org uniquely before enabling business login; do not infer a mapping from display names. Explicitly provision its owner membership against the verified Casdoor subject. Additional accounts join through company invitations. Creating an organization alone does not grant access to another company's data.

## Validation

- Go auth tests passed against a disposable PostgreSQL database, covering shared audience/owner mismatch, cross-company identity, automatic organization login and existing state/nonce/PKCE/invitation paths.
- Next production build and TypeScript passed. Existing Google Sans Flex override and metadataBase warnings remain.
- Public API checks: tenant 4 contains 11,069 products; tenant 5 contains zero. Session tenant hints cannot override actual memberships.
- Real password -> Casdoor code -> Go token verification/callback passed for temporary normal accounts in both organizations. Both returned to company-required (no automatic membership). Temporary accounts and sessions were removed.
- Duplicate account creation and rename rejected inside rolled-back SQLite probes; original users preserved.

## Modern Web Guidance

forms: preserved native GET submission for login routing, named the hidden organization input, and kept invitations as POST actions. Hidden input is not an authorization source: "Server-side validation is for Security".

## Rollback

Source backup: org-login-source-1795446882069 under native state/backups. Auth DB/runtime/Casdoor backup: organization-binding-1795447097340. Global username backup: global-usernames-1795447769815. Previous auth binary and .next are in the organization-binding backup. A coordinated rollback must retain the shared client organization audience behavior; do not restore only a pre-shared verifier against the currently shared application. Do not restore the identity database while Casdoor is running. No screening business rows were moved or removed by this change.

## Short login page (2026-11-23)

Native deployment enables SHORT_LOGIN_ENABLED=true, CASDOOR_INTERNAL_URL=http://127.0.0.1:18444, CASDOOR_APPLICATION_NAME=application_seller_ops. GET /api/auth/login now redirects to /login. The public gateway serves /login from Go auth's /api/auth/login-page; POST /api/auth/login-page goes directly to auth, retaining the real forwarding chain.

The server-rendered form uses native POST, username/current-password autofill, visible labels, a password visibility toggle and a nonce-based CSP. Browser cookies hold only an opaque random login-flow handle. auth_login_flows in the auth database stores PKCE/nonce/state/authorization parameters for ten minutes, never passwords. CSRF and claim-before-submit protect form submission; successful callback consumes the flow once and runs the unchanged signature/issuer/organization-audience/nonce/PKCE checks. Generic errors preserve username but not password.

Other authentication methods remain available through /api/auth/login?full=1. Captcha/MFA requirements are not bypassed; the compact password form directs users requiring extra verification to this full flow. Invitation return and organization-bound login are retained.

Validation: disposable PostgreSQL auth tests passed for success, invalid CSRF, incorrect password, replay rejection, full authentication fallback and company isolation. Two temporary users in the actual identity service also completed password -> signed ID token -> short callback, returning to company-required because they had no memberships. All temporary users and auth sessions were removed. The public form was 3,432 bytes and contained no authorization URL query. In-app browser preview timed out, so visual browser inspection was not completed.

Modern Web Guidance: autofill-sign-in-form ("Use autocomplete=\"current-password\" for an existing password") informed the native form, stable field IDs, required constraints and visibility toggle. No script intercepts or prevents native submission, so the form works without JavaScript.

Backup: native state/backups/short-login-1795448831414 contains auth DB, prior runtime and auth/gateway binaries. Restore both binaries/runtime together to undo the short entry; the new auth_login_flows table may remain. Company data and identity passwords were not changed. Platform-console port separation remains a separate deployment step; only the user-facing /login routing was changed here.

### Native browser Origin fix

The short form now sends Referrer-Policy: same-origin. Per the Fetch Standard, no-referrer serializes a native POST Origin as null, causing the strict origin guard to reject legitimate form submissions. Do not relax allowed origins or accept null. Regression covers the response policy. An isolated real Chrome using the same anonymous form reproduced null before and the correct loopback origin after; public-IP browser navigation was blocked by the test browser, so this browser check used a local fixture. The public page serves same-origin and foreign-Origin POSTs remain 403.

### Profit autosave public HTTP fix

ProfitSaveQueue and the shared screening write client now use the existing uuid v4 package rather than calling secure-context-only crypto.randomUUID directly. Browser UUID fallback uses crypto.getRandomValues. Queue re-enqueue of unchanged failed input no longer re-emits failure state; detailed errors are retained, and the autosave hook returns the existing state when unchanged. Tests simulate an HTTP crypto object without randomUUID and cover queue/API writes, stable retry keys, no repeated failed callbacks, serialization and concurrency. Production build/typecheck passed. Only the targeted frontend fix was deployed; pending registration UI remained at its live baseline. Backend profit records and formulas were not changed.
