import { createApiAction, type ApiAction } from "@app/api/request";
import type { Invite, Me } from "@app/api/types";

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
