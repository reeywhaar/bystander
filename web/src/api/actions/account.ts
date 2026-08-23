import { createApiAction, type ApiAction } from "@app/api/request";
import type { Account } from "@app/api/types";

/** `GET /api/account` — your own account, which is the only one you can see this way. */
export function getAccount(): ApiAction<Account> {
  return createApiAction((d) =>
    d.call({ method: "GET", path: "/api/account" }),
  );
}

/** `PATCH /api/account` — the recovery address. Empty clears it. */
export function patchAccount(changes: {
  recovery_email: string;
}): ApiAction<Account> {
  return createApiAction((d) =>
    d.call({ method: "PATCH", path: "/api/account", body: changes }),
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
