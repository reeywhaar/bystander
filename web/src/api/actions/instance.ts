import { createApiAction, type ApiAction } from "@app/api/request";
import type { InstanceSettings } from "@app/api/types";

/** `GET /api/admin/instance` — the answers that belong to the instance, not to anybody on it. */
export function getInstance(): ApiAction<InstanceSettings> {
  return createApiAction((d) =>
    d.call({ method: "GET", path: "/api/admin/instance" }),
  );
}

/**
 * `PUT /api/admin/instance` — writes them.
 *
 * Turning publishing off takes every published page down at once rather than only stopping new
 * ones. It is the instance's answer to whether it serves anything to strangers, and an answer
 * that only applied to pages published afterwards would not be one.
 */
export function putInstance(
  settings: InstanceSettings,
): ApiAction<InstanceSettings> {
  return createApiAction((d) =>
    d.call({ method: "PUT", path: "/api/admin/instance", body: settings }),
  );
}
