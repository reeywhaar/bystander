import { createApiAction, type ApiAction } from "@app/api/request";
import type {
  AdminInvite,
  Proxy,
  ProxyForm,
  ProxyTestResult,
  RecoveryLink,
  Role,
  SmtpConfig,
  SmtpForm,
  User,
} from "@app/api/types";

const seg = (value: string) => encodeURIComponent(value);

/** `GET /api/admin/users` */
export function getAdminUsers(): ApiAction<User[]> {
  return createApiAction((d) =>
    d.call({ method: "GET", path: "/api/admin/users" }),
  );
}

/** `PATCH /api/admin/users/{id}` — refused for your own account and the last administrator. */
export function patchAdminUsersById(
  id: string,
  changes: { disabled: boolean },
): ApiAction<void> {
  return createApiAction((d) =>
    d.call({
      method: "PATCH",
      path: `/api/admin/users/${seg(id)}`,
      body: changes,
    }),
  );
}

/** `DELETE /api/admin/users/{id}` */
export function deleteAdminUsersById(id: string): ApiAction<void> {
  return createApiAction((d) =>
    d.call({ method: "DELETE", path: `/api/admin/users/${seg(id)}` }),
  );
}

/**
 * `POST /api/admin/users/{id}/recovery` — mints a way back into somebody's account.
 *
 * The reply carries the URL, and it is the only time it can be read: what is stored is a hash,
 * so a lost link is reissued rather than recovered.
 *
 * It changes nothing about the account — no session ends, no password moves, and nobody is
 * told. An administrator can answer "I am locked out" without locking out somebody who turns
 * out to have been fine, and an unused link simply lapses.
 */
export function postAdminUsersByIdRecovery(
  id: string,
): ApiAction<RecoveryLink> {
  return createApiAction((d) =>
    d.call({ method: "POST", path: `/api/admin/users/${seg(id)}/recovery` }),
  );
}

/** `GET /api/admin/invites` — never carries a token; that is unrecoverable by design. */
export function getAdminInvites(): ApiAction<AdminInvite[]> {
  return createApiAction((d) =>
    d.call({ method: "GET", path: "/api/admin/invites" }),
  );
}

/** `POST /api/admin/invites` — the one response that carries the link. */
/**
 * `POST /api/admin/invites` — mint a link, or send one.
 *
 * With an address the server sends the link there and the reply carries no URL. Without one it
 * hands the URL back, that once. The two are exclusive: a link an administrator can also read
 * proves nothing about who accepted it, and accepting an emailed invitation is what binds the
 * address to the account.
 */
export function postAdminInvites(
  role: Role,
  email = "",
): ApiAction<AdminInvite> {
  return createApiAction((d) =>
    d.call({
      method: "POST",
      path: "/api/admin/invites",
      body: { role, email },
    }),
  );
}

/** `DELETE /api/admin/invites/{id}` — refused for one already accepted. */
export function deleteAdminInvitesById(id: string): ApiAction<void> {
  return createApiAction((d) =>
    d.call({ method: "DELETE", path: `/api/admin/invites/${seg(id)}` }),
  );
}

/** `GET /api/admin/smtp` — the relay without its password, which is write-only. */
export function getAdminSmtp(): ApiAction<SmtpConfig> {
  return createApiAction((d) =>
    d.call({ method: "GET", path: "/api/admin/smtp" }),
  );
}

/**
 * `PUT /api/admin/smtp` — the whole configuration at once.
 *
 * An empty `password` leaves the stored one alone, so correcting a port does not mean
 * retyping a secret the page never showed.
 */
export function putAdminSmtp(config: SmtpForm): ApiAction<SmtpConfig> {
  return createApiAction((d) =>
    d.call({ method: "PUT", path: "/api/admin/smtp", body: config }),
  );
}

/** `DELETE /api/admin/smtp` — after this, sending is refused rather than attempted. */
export function deleteAdminSmtp(): ApiAction<void> {
  return createApiAction((d) =>
    d.call({ method: "DELETE", path: "/api/admin/smtp" }),
  );
}

/**
 * `POST /api/admin/smtp/test` — sends one real message and reports what the relay said.
 *
 * A `relay` tries settings that have not been saved and writes nothing, which is the only
 * way to find out whether a password works before it replaces one that already did.
 */
export function postAdminSmtpTest(
  to: string,
  relay?: SmtpForm,
): ApiAction<void> {
  return createApiAction((d) =>
    d.call({
      method: "POST",
      path: "/api/admin/smtp/test",
      body: { to, relay },
    }),
  );
}

/** `GET /api/admin/proxies` — the relays without their tokens, in the order they are tried. */
export function getAdminProxies(): ApiAction<{ proxies: Proxy[] }> {
  return createApiAction((d) =>
    d.call({ method: "GET", path: "/api/admin/proxies" }),
  );
}

/** `POST /api/admin/proxies` — add one. */
export function postAdminProxies(form: ProxyForm): ApiAction<Proxy> {
  return createApiAction((d) =>
    d.call({ method: "POST", path: "/api/admin/proxies", body: form }),
  );
}

/**
 * `PUT /api/admin/proxies/{id}` — the whole entry at once.
 *
 * An empty `token` leaves the stored one alone, so correcting a label does not mean retyping
 * a secret the page never showed.
 */
export function putAdminProxiesById(
  id: string,
  form: ProxyForm,
): ApiAction<Proxy> {
  return createApiAction((d) =>
    d.call({
      method: "PUT",
      path: `/api/admin/proxies/${seg(id)}`,
      body: form,
    }),
  );
}

/** `DELETE /api/admin/proxies/{id}` — the token goes with it. */
export function deleteAdminProxiesById(id: string): ApiAction<void> {
  return createApiAction((d) =>
    d.call({ method: "DELETE", path: `/api/admin/proxies/${seg(id)}` }),
  );
}

/**
 * `POST /api/admin/proxies/test` — asks a relay to fetch something and says what came back.
 *
 * No id in the path, because a relay that has not been saved yet has none. `proxy` tries
 * settings as typed and writes nothing, which is the only way to find out whether a token
 * works before it replaces one that already did; `id` beside it fills in a token the browser
 * was never sent. `url` defaults to this instance's own address.
 */
export function postAdminProxyTest(body: {
  id?: string;
  proxy?: ProxyForm;
  url?: string;
}): ApiAction<ProxyTestResult> {
  return createApiAction((d) =>
    d.call({ method: "POST", path: "/api/admin/proxies/test", body }),
  );
}

/**
 * `POST /api/admin/proxies/{id}/reset` — forgets every publisher learned through this relay.
 *
 * The relay itself is untouched. Routes end on their own when a relay fails; this is for what
 * the instance cannot know — a restriction lifted, a relay moved, a setup that was a test.
 */
export function postAdminProxyReset(
  id: string,
): ApiAction<{ forgotten: number }> {
  return createApiAction((d) =>
    d.call({ method: "POST", path: `/api/admin/proxies/${seg(id)}/reset` }),
  );
}
