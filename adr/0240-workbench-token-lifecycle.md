# 0240 — One refresh in flight, and signed in means a token can be had

Issue: #240 · PR: pending · Status: accepted

## Context

The workbench host owns both refresh tokens (`auth.ts`). `token()` and the background timer each posted a refresh with no coordination, so parallel callers inside the expiry window sent the same refresh token several times. That is harmless without rotation. Under rotation all but the first fail, and strict reuse detection can revoke the session.

Failure said nothing about the session. A failed refresh threw and changed no state, so `signedIn` stayed true with a dead token, every call repeated the refresh, and the timer stopped. Refused and unreachable were not told apart. The opposite happened at start-up: `restore()` deleted the stored token on any failure, so a backend that was down signed the analyst out for good.

A grant in flight during sign-out wrote the tokens and the secret back and re-armed the timer, so the next window restored a session the analyst had ended. A response omitting `refresh_token`, which OAuth allows, blanked the held one. `grant` parsed JSON before checking the status, so a gateway's error page surfaced as a `SyntaxError`. `BackendClient` failed on a 401 where the sidecar path refreshed and retried. Signing in over someone else left their delegate tokens in place if the agent flow failed.

## Decision

**One refresh per side.** `token()`, `force` and the timer all go through a `SingleFlight`. A forced caller joining a refresh gets its result, which is the fresh token it wanted.

**The token endpoint's answer decides the session.** A 400 or 401 means the identity provider refused the grant, and the session is over. On the human side that is a full `signOut`, so the identity change fires and everything held for the person goes (adr/0237, adr/0241), and the analyst is told why. On the delegate side only that side ends. Anything else, a network failure or a 5xx, keeps the session and surfaces the error, and the timer retries with backoff from 5 seconds to 5 minutes. `restore()` follows the same rule: a refusal deletes the stored token, and any other failure leaves it for the next window.

**A grant that lands late is discarded.** Each side has a generation, bumped when the session on that side ends or is replaced. A refresh that began before the bump changes nothing, and a `token()` call waiting on it throws rather than return someone else's token. A code exchange replaces the side, and bumps only once it has succeeded, so a failed sign-in leaves the session as it was. The generations are per side because the two sign in one after the other, and the delegate's sign-in must not discard a refresh of the new person's human token. The key of the refresh's `SingleFlight` carries the generation, so nobody joins a discarded refresh.

**A 401 from the backend retries once, as the same person.** `BackendClient` asks `Session.renew` for a fresh token to replace the one refused, and resends once. A 401 comes before any work is done: from the auth middleware, or from a handler finding no claims. So a write is safe to resend. `renew` throws unless the refused token's subject is the session's, so a request begun as one person is never sent as another. Together with the generation check this holds for a change of person during the refresh as well.

**A human sign-in ends the delegate side.** The delegate's tokens are the last session's. The agent flow that follows mints new ones. If it fails, agent turns ask for a sign-in rather than run on the previous tokens.

**Missing `refresh_token` keeps the one used.** **The status is read before the body**, which is parsed tolerantly, so a failure always reports its status.

## Alternatives rejected

- One generation for both sides: the delegate sign-in would discard the human side's refresh and, under rotation, lose the new token.
- Treating every failed refresh as signed out: a network blip would close every panel (adr/0241).
- Keeping the session on a 400: it can never recover, and every call would repeat the refusal.
- Leaving the 401 retry to callers: the same logic at each call site, and the identity check would be easy to miss.
- Retrying a 401 with whatever token the session now holds: after a change of person it would resend a write under someone else.

## Consequences

- A 400 for any reason, such as a misconfigured client, ends the session as a refused token would.
- While the provider is unreachable the UI shows signed in, and calls fail with the error once the access token expires. That is accurate: a token may still be had.
- A token with no readable subject is not retried, as it cannot be shown to be the same person.
- A sign-in whose agent flow fails loses a delegate session that may have been working, even for the same person.
