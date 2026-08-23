import { Boundary } from "@app/components/Boundary";
import { RequireSession } from "@app/components/RequireSession";

import { ReaderPage } from "@app/apps/reader/ReaderPage";

/**
 * The reader has no routes.
 *
 * There is one page and it is the product; react-router earns its place in the islands
 * that have sections, not here.
 */
export function App() {
  return (
    <Boundary>
      <RequireSession>{(me) => <ReaderPage me={me} />}</RequireSession>
    </Boundary>
  );
}
