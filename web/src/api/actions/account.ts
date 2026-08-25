import { createApiAction, type ApiAction } from "@app/api/request";
import type { Account } from "@app/api/types";

/** `GET /api/account` — your own account, which is the only one you can see this way. */
export function getAccount(): ApiAction<Account> {
  return createApiAction((d) =>
    d.call({ method: "GET", path: "/api/account" }),
  );
}

/**
 * `POST /api/account/recovery` — sends a code to an address and records nothing.
 *
 * The account has no recovery address until the code comes back. An address nobody has
 * proved they can read is worse than none: a typo sends recovery to a stranger's inbox, and
 * the owner finds out at the one moment they cannot afford to.
 */
export function postAccountRecovery(email: string): ApiAction<void> {
  return createApiAction((d) =>
    d.call({ method: "POST", path: "/api/account/recovery", body: { email } }),
  );
}

/** `POST /api/account/recovery/confirm` — the only step that changes anything. */
export function postAccountRecoveryConfirm(code: string): ApiAction<Account> {
  return createApiAction((d) =>
    d.call({
      method: "POST",
      path: "/api/account/recovery/confirm",
      body: { code },
    }),
  );
}

/** `DELETE /api/account/recovery` — forgets the address and anything in flight. */
export function deleteAccountRecovery(): ApiAction<void> {
  return createApiAction((d) =>
    d.call({ method: "DELETE", path: "/api/account/recovery" }),
  );
}

/**
 * `POST /api/account/password`
 *
 * The current password is required: a session cookie is enough to read somebody's feeds and
 * must not also be enough to take the account. Every other session ends; this one does not.
 */
export function postAccountPassword(passwords: {
  current_password: string;
  new_password: string;
}): ApiAction<void> {
  return createApiAction((d) =>
    d.call({ method: "POST", path: "/api/account/password", body: passwords }),
  );
}

/**
 * `PUT /api/account/public-name` — the name this account's published pages live under.
 *
 * An empty name gives it up. Changing it moves every published page at once, because a public
 * address is built from the name rather than stored beside the page — so the old addresses stop
 * working, which is what changing your name means.
 */
export function putAccountPublicName(name: string): ApiAction<Account> {
  return createApiAction((d) =>
    d.call({ method: "PUT", path: "/api/account/public-name", body: { name } }),
  );
}
