import type { ApiDispatcher } from "@app/api/dispatcher";
import type { Method, RequestOptions } from "@app/api/transport";

/**
 * The raw description of one HTTP call.
 *
 * Carries no behaviour and no response type — the dispatcher's `call<T>` supplies that.
 */
export interface ApiRequest {
  method: Method;
  path: string;
  query?: RequestOptions["query"];
  body?: unknown;
}

/**
 * An API action: everything needed to make one call, minus the thing that makes it.
 *
 * Actions are curried — `getEdition()` builds the action, and handing it a dispatcher runs
 * it — which buys two things over returning a bare request object:
 *
 *  - An action can post-process with `.then`, so unwrapping or reshaping a response is
 *    ordinary code whose result type TypeScript infers, rather than a `select` callback
 *    typed by hand and cast at the boundary.
 *  - Composing actions is just calling them: an action can await another, or chain
 *    several, without any of that leaking into the dispatcher.
 *
 * Note what is *not* in this signature: an AbortSignal. Cancellation rides on the
 * dispatcher instead (see `ApiDispatcher.withSignal`), so no action has to accept, name or
 * forward a signal it does not care about — and forgetting to thread one through becomes
 * impossible rather than merely unlikely.
 */
export type ApiAction<T> = (dispatcher: ApiDispatcher) => Promise<T>;

/**
 * Identity at runtime; entirely about types.
 *
 * Wrapping the body in this contextually types `dispatcher`, so an action needs no
 * parameter annotations, and it infers `T` from whatever the body resolves to — including
 * through a `.then` that reshapes something. Writing the return type by hand instead would
 * mean stating the result twice and letting the two drift.
 */
export function createApiAction<T>(run: ApiAction<T>): ApiAction<T> {
  return run;
}
