import { BrowserRouter, Navigate, Route, Routes } from "react-router";

import { Boundary } from "@app/components/Boundary";

import { InvitePage } from "@app/apps/login/InvitePage";
import { LoginPage } from "@app/apps/login/LoginPage";

/**
 * The island an unauthenticated visitor gets: a login form and an invitation.
 *
 * Nothing here reads a session, and nothing here can: the reader's, the manage island's
 * and the admin island's code are three separate bundles that this document never loads.
 */
export function App() {
  return (
    <Boundary>
      <BrowserRouter>
        <Routes>
          <Route path="/login" element={<LoginPage />} />
          <Route path="/invite/:token" element={<InvitePage />} />
          {/* A truncated link, which the page answers with "this looks incomplete". */}
          <Route path="/invite" element={<InvitePage />} />
          <Route path="*" element={<Navigate to="/login" replace />} />
        </Routes>
      </BrowserRouter>
    </Boundary>
  );
}
