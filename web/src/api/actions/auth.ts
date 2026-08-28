import { createApiAction, type ApiAction } from "@app/api/request";
import type { Invite, Me, PublicInstance, Recovery } from "@app/api/types";

/** One path segment, encoded. An invitation token is base64url and safe, but this is not
 *  the place to rely on that staying true. */
const seg = (value: string) => encodeURIComponent(value);

/** `POST /api/login` */
export function postLogin(username: string, password: string): ApiAction<void> {
  return createApiAction((d) =>
    d.call({
      method: "POST",
      path: "/api/login",
      body: { username, password },
    }),
  );
}

/** `POST /api/logout` */
export function postLogout(): ApiAction<void> {
  return createApiAction((d) =>
    d.call({ method: "POST", path: "/api/logout" }),
  );
}

/** `GET /api/me` */
export function getMe(): ApiAction<Me> {
  return createApiAction((d) => d.call({ method: "GET", path: "/api/me" }));
}

/** `GET /api/invites/{token}` — validity only, before a password is typed. */
export function getInvitesByToken(token: string): ApiAction<Invite> {
  return createApiAction((d) =>
    d.call({ method: "GET", path: `/api/invites/${seg(token)}` }),
  );
}

/** `POST /api/invites/{token}/accept` — creates the account and signs it in. */
export function postInvitesByTokenAccept(
  token: string,
  username: string,
  password: string,
): ApiAction<void> {
  return createApiAction((d) =>
    d.call({
      method: "POST",
      path: `/api/invites/${seg(token)}/accept`,
      body: { username, password },
    }),
  );
}

/**
 * `GET /api/instance` — what the login form is allowed to know before anybody signs in.
 *
 * Only whether a way back into an account can be mailed. Asked because a form that takes an
 * address and says "check your inbox" on an instance with no relay is lying.
 */
export function getPublicInstance(): ApiAction<PublicInstance> {
  return createApiAction((d) =>
    d.call({ method: "GET", path: "/api/instance" }),
  );
}

/**
 * `POST /api/recoveries` — asks for a way back into the account at an address.
 *
 * Always answers the same, whether or not an account has that address on file. Anything else
 * would turn this into a way to ask the instance who has an account here. So there is nothing
 * to report on success beyond the fact that the request was accepted.
 */
export function postRecoveries(email: string): ApiAction<void> {
  return createApiAction((d) =>
    d.call({ method: "POST", path: "/api/recoveries", body: { email } }),
  );
}

/** `GET /api/recoveries/{token}` — what state a link is in, before a password is typed. */
export function getRecoveriesByToken(token: string): ApiAction<Recovery> {
  return createApiAction((d) =>
    d.call({ method: "GET", path: `/api/recoveries/${seg(token)}` }),
  );
}

/**
 * `POST /api/recoveries/{token}/accept` — spends the link on a new password.
 *
 * It does not sign anybody in, unlike accepting an invitation. The account already existed
 * and the link may have reached the wrong person, so typing the new password at the login
 * form once is the cheapest confirmation there is that the right one has it.
 */
export function postRecoveriesByTokenAccept(
  token: string,
  password: string,
): ApiAction<void> {
  return createApiAction((d) =>
    d.call({
      method: "POST",
      path: `/api/recoveries/${seg(token)}/accept`,
      body: { password },
    }),
  );
}
