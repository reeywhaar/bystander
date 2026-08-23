import { createApiAction, type ApiAction } from "@app/api/request";
import type { AdminInvite, Role, User } from "@app/api/types";

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
