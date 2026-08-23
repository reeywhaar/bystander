import { createApiAction, type ApiAction } from "@app/api/request";
import type {
  AdminInvite,
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

/** `GET /api/admin/invites` — never carries a token; that is unrecoverable by design. */
export function getAdminInvites(): ApiAction<AdminInvite[]> {
  return createApiAction((d) =>
    d.call({ method: "GET", path: "/api/admin/invites" }),
  );
}

/** `POST /api/admin/invites` — the one response that carries the link. */
export function postAdminInvites(role: Role): ApiAction<AdminInvite> {
  return createApiAction((d) =>
    d.call({ method: "POST", path: "/api/admin/invites", body: { role } }),
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
