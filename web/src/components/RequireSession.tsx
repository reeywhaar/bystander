import type { ReactNode } from "react";

import { ApiError } from "@app/api/error";
import type { Me } from "@app/api/types";
import { Spinner } from "@app/components/ui/Spinner";
import { useMe } from "@app/queries/hooks";

/**
 * Renders its children only for somebody signed in, and sends everybody else to the login
 * island.
 *
 * A whole-document navigation rather than a client-side route: `/login` belongs to a
 * different bundle, and the point of splitting them is that this one never has the login
 * screen in it. `replace` so the back button does not bounce between the two.
 *
 * This is a courtesy, not the boundary. The server refuses every request without a
 * session; what this does is turn a wall of failed cards into a login form.
 */
export function RequireSession({
  children,
}: {
  children: (me: Me) => ReactNode;
}) {
  const me = useMe();

  if (me.isPending) return <Spinner label="Signing in" />;

  if (me.error) {
    if (me.error instanceof ApiError && me.error.unauthorized) {
      const next = encodeURIComponent(
        window.location.pathname + window.location.search,
      );
      window.location.replace(`/login?next=${next}`);
      return <Spinner label="Taking you to the login page" />;
    }
    throw me.error;
  }

  return <>{children(me.data)}</>;
}
