import type { ReactNode } from "react";

import { useApiCall } from "@app/api/provider";
import { postLogout } from "@app/api/actions/auth";
import type { Me } from "@app/api/types";

/**
 * The band across the top of every island.
 *
 * The links between islands are ordinary anchors, not client-side routes: each island is a
 * separate document, and that separation is the point.
 */
export function Masthead({
  me,
  subtitle,
  children,
}: {
  me: Me;
  subtitle?: ReactNode;
  children?: ReactNode;
}) {
  const callApi = useApiCall();

  async function signOut() {
    try {
      await callApi(postLogout());
    } finally {
      // Whatever the server said, the cookie is gone or was never valid. Sending them to
      // the login island either way beats leaving them on a page that cannot load.
      window.location.href = "/login";
    }
  }

  return (
    <header className="border-b border-rule">
      <div className="mx-auto flex max-w-[1400px] flex-wrap items-baseline gap-x-6 gap-y-2 px-6 py-5">
        <a
          href="/"
          className="font-serif text-3xl leading-none tracking-tight text-ink hover:text-accent"
        >
          bystander
        </a>
        {subtitle ? <p className="text-sm text-ink-muted">{subtitle}</p> : null}

        <div className="ml-auto flex items-center gap-4 text-sm">
          {children}
          <a href="/manage" className="text-ink-muted hover:text-ink">
            Settings
          </a>
          {me.role === "admin" ? (
            <a href="/admin" className="text-ink-muted hover:text-ink">
              Admin
            </a>
          ) : null}
          <button
            type="button"
            onClick={() => void signOut()}
            className="text-ink-muted hover:text-ink"
          >
            Sign out
          </button>
        </div>
      </div>
    </header>
  );
}
