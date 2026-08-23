import { createContext, useContext, useMemo, type ReactNode } from "react";

import type { ApiDispatcher } from "@app/api/dispatcher";
import type { ApiAction } from "@app/api/request";

const DispatcherContext = createContext<ApiDispatcher | null>(null);

/**
 * Provides the dispatcher built at startup.
 *
 * Injected rather than imported as a module singleton, so a test renders a subtree against
 * a recorded transport without touching global state, and nothing in the tree can reach
 * the network except through something its parent handed it.
 */
export function ApiProvider({
  dispatcher,
  children,
}: {
  dispatcher: ApiDispatcher;
  children: ReactNode;
}) {
  return (
    <DispatcherContext.Provider value={dispatcher}>
      {children}
    </DispatcherContext.Provider>
  );
}

export function useApiDispatcher(): ApiDispatcher {
  const dispatcher = useContext(DispatcherContext);
  if (!dispatcher) {
    throw new Error("useApiDispatcher must be used inside an <ApiProvider>");
  }
  return dispatcher;
}

export type ApiCall = <T>(
  action: ApiAction<T>,
  signal?: AbortSignal,
) => Promise<T>;

/**
 * Runs an action against the provided dispatcher:
 *
 *     const callApi = useApiCall();
 *     const page = await callApi(getEdition(), signal);
 *
 * The signal is bound to a clone of the dispatcher rather than passed down to the action,
 * so actions themselves never mention cancellation. It is threaded here because this is
 * the one place that knows about it — TanStack Query hands every query function a signal,
 * and honouring it is what makes a superseded fetch stop instead of racing the one that
 * replaced it.
 *
 * Stable across renders: an unstable identity would land in the dependency array of every
 * queryFn and refetch on every render.
 */
export function useApiCall(): ApiCall {
  const dispatcher = useApiDispatcher();
  return useMemo<ApiCall>(
    () => (action, signal) =>
      action(signal ? dispatcher.withSignal(signal) : dispatcher),
    [dispatcher],
  );
}
