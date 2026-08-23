import { BrowserRouter, NavLink, Navigate, Route, Routes } from "react-router";

import { Boundary } from "@app/components/Boundary";
import { Masthead } from "@app/components/Masthead";
import { RequireSession } from "@app/components/RequireSession";
import { Alert } from "@app/components/ui/Alert";

import { InvitesPage } from "@app/apps/admin/InvitesPage";
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
                  <nav className="mb-8 flex gap-1 border-b border-rule">
                    {[
                      { to: "/admin", label: "People", end: true },
                      {
                        to: "/admin/invites",
                        label: "Invitations",
                        end: false,
                      },
                    ].map((tab) => (
                      <NavLink
                        key={tab.to}
                        to={tab.to}
                        end={tab.end}
                        className={({ isActive }) =>
                          `-mb-px border-b-2 px-3 py-2 text-sm ${
                            isActive
                              ? "border-accent text-ink"
                              : "border-transparent text-ink-muted hover:text-ink"
                          }`
                        }
                      >
                        {tab.label}
                      </NavLink>
                    ))}
                  </nav>
                  <Routes>
                    <Route path="/admin" element={<UsersPage me={me} />} />
                    <Route path="/admin/invites" element={<InvitesPage />} />
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
