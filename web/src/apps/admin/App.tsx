import { BrowserRouter, Navigate, Route, Routes } from "react-router";

import { Boundary } from "@app/components/Boundary";
import { Masthead } from "@app/components/Masthead";
import { RequireSession } from "@app/components/RequireSession";
import { Alert } from "@app/components/ui/Alert";
import { TabStrip } from "@app/components/ui/TabStrip";

import { ImagesPage } from "@app/apps/admin/ImagesPage";
import { InvitesPage } from "@app/apps/admin/InvitesPage";
import { PublishingPage } from "@app/apps/admin/PublishingPage";
import { MailPage } from "@app/apps/admin/MailPage";
import { UsersPage } from "@app/apps/admin/UsersPage";

export function App() {
  return (
    <Boundary>
      <RequireSession>
        {(me) => (
          <>
            <Masthead me={me} subtitle="Administration" />
            <main className="mx-auto max-w-3xl px-6 py-10">
              {/* The server refuses every admin route to an ordinary account. Saying so
                  here turns four failed requests into one sentence. */}
              {me.role !== "admin" ? (
                <Alert>That is an administrator's to do.</Alert>
              ) : (
                <BrowserRouter>
                  <TabStrip
                    tabs={[
                      { to: "/admin", label: "People", end: true },
                      { to: "/admin/invites", label: "Invitations" },
                      { to: "/admin/mail", label: "Mail" },
                      { to: "/admin/images", label: "Images" },
                      { to: "/admin/publishing", label: "Publishing" },
                    ]}
                  />
                  <Routes>
                    <Route path="/admin" element={<UsersPage me={me} />} />
                    <Route path="/admin/invites" element={<InvitesPage />} />
                    <Route path="/admin/mail" element={<MailPage />} />
                    <Route path="/admin/images" element={<ImagesPage />} />
                    <Route
                      path="/admin/publishing"
                      element={<PublishingPage />}
                    />
                    <Route
                      path="*"
                      element={<Navigate to="/admin" replace />}
                    />
                  </Routes>
                </BrowserRouter>
              )}
            </main>
          </>
        )}
      </RequireSession>
    </Boundary>
  );
}
