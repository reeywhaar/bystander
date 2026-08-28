import { BrowserRouter, Navigate, Route, Routes } from "react-router";

import { Boundary } from "@app/components/Boundary";

import { ForgotPage } from "@app/apps/login/ForgotPage";
import { InvitePage } from "@app/apps/login/InvitePage";
import { LandingPage } from "@app/apps/login/LandingPage";
import { LoginPage } from "@app/apps/login/LoginPage";
import { RecoverPage } from "@app/apps/login/RecoverPage";

/**
 * The island an unauthenticated visitor gets: the landing page, a login form, and an
 * invitation.
 *
 * Nothing here reads a session, and nothing here can: the reader's, the manage island's
 * and the admin island's code are three separate bundles that this document never loads.
 */
export function App() {
  return (
    <Boundary>
      <BrowserRouter>
        <Routes>
          {/* The server hands this document to anybody arriving at "/" without a session —
              see shellFor in internal/api/spa.go, where "/" is the one route that reads the
              request rather than only the path. Somebody who does have one never gets here:
              they get the reader. */}
          <Route path="/" element={<LandingPage />} />
          <Route path="/login" element={<LoginPage />} />
          <Route path="/invite/:token" element={<InvitePage />} />
          {/* A truncated link, which the page answers with "this looks incomplete". */}
          <Route path="/invite" element={<InvitePage />} />
          {/* Getting back in without a password: ask for a link, then spend it. Both here
              because whoever is on them has no session — and the second is reached from a
              mail rather than from this site at all. */}
          <Route path="/forgot" element={<ForgotPage />} />
          <Route path="/recover/:token" element={<RecoverPage />} />
          <Route path="/recover" element={<RecoverPage />} />
          <Route path="*" element={<Navigate to="/login" replace />} />
        </Routes>
      </BrowserRouter>
    </Boundary>
  );
}
