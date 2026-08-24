import { BrowserRouter, NavLink, Navigate, Route, Routes } from "react-router";

import { Boundary } from "@app/components/Boundary";
import { Masthead } from "@app/components/Masthead";
import { RequireSession } from "@app/components/RequireSession";
import { Alert } from "@app/components/ui/Alert";

import { InvitesPage } from "@app/apps/admin/InvitesPage";
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
                  {/* Wraps, like the manage island's — three shorter labels fit today, and
                      the first translation of "Invitations" is the one that would not. */}
                  <nav className="mb-8 flex flex-wrap gap-x-1 gap-y-0.5 border-b border-rule">
                    {[
                      { to: "/admin", label: "People", end: true },
                      {
                        to: "/admin/invites",
                        label: "Invitations",
                        end: false,
                      },
                      { to: "/admin/mail", label: "Mail", end: false },
                    ].map((tab) => (
                      <NavLink
                        key={tab.to}
                        to={tab.to}
                        end={tab.end}
                        className={({ isActive }) =>
                          `-mb-px border-b-2 px-2 py-2 text-sm whitespace-nowrap sm:px-3 ${
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
                    <Route path="/admin/mail" element={<MailPage />} />
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
