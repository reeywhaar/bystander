import { BrowserRouter, Navigate, Route, Routes } from "react-router";

import { Boundary } from "@app/components/Boundary";
import { RequireSession } from "@app/components/RequireSession";

import { AccountPage } from "@app/apps/manage/AccountPage";
import { FeedsPage } from "@app/apps/manage/FeedsPage";
import { Layout } from "@app/apps/manage/Layout";
import { ReadPage } from "@app/apps/manage/ReadPage";
import { SharePage } from "@app/apps/manage/SharePage";
import { SettingsPage } from "@app/apps/manage/SettingsPage";
import { TagsPage } from "@app/apps/manage/TagsPage";

export function App() {
  return (
    <Boundary>
      <RequireSession>
        {(me) => (
          <BrowserRouter>
            <Layout me={me}>
              <Routes>
                <Route path="/manage" element={<FeedsPage />} />
                <Route path="/manage/tags" element={<TagsPage />} />
                <Route path="/manage/settings" element={<SettingsPage />} />
                <Route path="/manage/read" element={<ReadPage />} />
                <Route path="/manage/account" element={<AccountPage />} />
                {/* Where a shared link lands. Outside /manage because it is a place
                    somebody arrives from a message, not a tab of their own settings. */}
                <Route path="/share/:token" element={<SharePage />} />
                <Route path="*" element={<Navigate to="/manage" replace />} />
              </Routes>
            </Layout>
          </BrowserRouter>
        )}
      </RequireSession>
    </Boundary>
  );
}
