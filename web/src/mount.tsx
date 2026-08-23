import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { StrictMode, type ReactNode } from "react";
import { createRoot } from "react-dom/client";

import { ApiDispatcher } from "@app/api/dispatcher";
import { ApiError } from "@app/api/error";
import { ApiProvider } from "@app/api/provider";
import { FetchTransport } from "@app/api/transport";

/**
 * What every island does at startup, in one place.
 *
 * The dispatcher is built here and handed down through context rather than imported as a
 * module singleton, which is what lets a test render any of these trees against a recorded
 * transport without touching global state.
 */
export function mount(app: ReactNode) {
  const client = new QueryClient({
    defaultOptions: {
      queries: {
        // Retrying a refusal is pointless: a 401 will refuse again, a 404 will still be
        // absent, and a 409 is a statement about the world rather than a hiccup. Only a
        // server error or a dropped connection is worth asking twice about.
        retry: (attempts, error) =>
          error instanceof ApiError
            ? error.status >= 500 && attempts < 2
            : attempts < 2,
        // A page changes when it is regenerated, not while somebody is reading it. Long
        // enough that switching tabs does not re-fetch the whole edition.
        staleTime: 60_000,
        refetchOnWindowFocus: false,
      },
    },
  });

  const root = document.getElementById("root");
  if (!root) throw new Error("no #root to mount into");

  createRoot(root).render(
    <StrictMode>
      <QueryClientProvider client={client}>
        <ApiProvider dispatcher={new ApiDispatcher(new FetchTransport())}>
          {app}
        </ApiProvider>
      </QueryClientProvider>
    </StrictMode>,
  );
}
