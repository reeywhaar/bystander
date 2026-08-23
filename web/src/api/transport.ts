export type Method = "GET" | "POST" | "PUT" | "PATCH" | "DELETE";

export interface RequestOptions {
  query?: Record<string, string | number | boolean | undefined>;
  /** Sent as JSON. */
  body?: unknown;
  signal?: AbortSignal;
}

/**
 * The only thing that knows how a request physically reaches the server.
 *
 * Everything above this line — requests, the dispatcher, every query, every component — is
 * transport-agnostic and must stay that way. Two rules keep it honest: a transport returns
 * a raw `Response` and never a parsed body, and nothing outside this file mentions
 * `fetch`, credentials or a header.
 *
 * There is one implementation. It is an interface anyway because that is what lets a test
 * render a subtree against a recorded transport rather than reaching for
 * `vi.stubGlobal("fetch")` and hoping no other test is doing the same thing at the same
 * time.
 */
export interface Transport {
  send(method: Method, path: string, opts?: RequestOptions): Promise<Response>;
}

/** Same-origin `fetch`, which is the only way this interface talks to its server. */
export class FetchTransport implements Transport {
  send(
    method: Method,
    path: string,
    opts: RequestOptions = {},
  ): Promise<Response> {
    // Relative, deliberately. The API is served from the same origin as the page, so there
    // is no origin to name — and naming one would put a `window.location` in the one class
    // that has to work anywhere, including under a test runner with no page loaded.
    const search = new URLSearchParams();
    for (const [name, value] of Object.entries(opts.query ?? {})) {
      if (value !== undefined) search.set(name, String(value));
    }
    const query = search.toString();
    const url = query === "" ? path : `${path}?${query}`;

    const headers: Record<string, string> = { accept: "application/json" };
    const init: RequestInit = {
      method,
      // The session cookie is the only credential the browser holds, so it must ride along.
      credentials: "same-origin",
      ...(opts.signal ? { signal: opts.signal } : {}),
    };
    if (opts.body !== undefined) {
      init.body = JSON.stringify(opts.body);
      // Not decoration: the server refuses a mutating request that does not declare JSON,
      // which is what stops a cross-site form post from reaching a handler.
      headers["content-type"] = "application/json";
    }
    init.headers = headers;

    return fetch(url, init);
  }
}
