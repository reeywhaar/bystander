import { BrowserRouter, Navigate, Route, Routes } from "react-router";

import { Boundary } from "@app/components/Boundary";
import { RequireSession } from "@app/components/RequireSession";

import { ReaderPage } from "@app/apps/reader/ReaderPage";

/**
 * The reader has two routes, and they are the same page.
 *
 * It had none until a person could keep more than one front page: there was one page and it
 * was the product. Now the main page is at `/` and the rest are at `/f/:slug`, and the routing
 * is client-side rather than a link to another document — which is the opposite of how this
 * application moves between islands, and deliberate. Every page's edition is cached under its
 * own key, so switching tabs shows something already in hand; a full navigation would discard
 * every other page's edition to display one of them.
 */
export function App() {
  return (
    <Boundary>
      <RequireSession>
        {(me) => (
          <BrowserRouter>
            <Routes>
              <Route path="/" element={<ReaderPage me={me} />} />
              <Route path="/f/:slug" element={<ReaderPage me={me} />} />
              {/* A page that has been removed, or an address somebody typed. The main page
                  is always there, which makes it the only honest place to land. */}
              <Route path="*" element={<Navigate to="/" replace />} />
            </Routes>
          </BrowserRouter>
        )}
      </RequireSession>
    </Boundary>
  );
}
