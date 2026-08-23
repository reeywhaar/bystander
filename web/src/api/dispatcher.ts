import { ApiError, messageFrom } from "@app/api/error";
import type { ApiRequest } from "@app/api/request";
import type { Transport } from "@app/api/transport";

/**
 * Executes API requests.
 *
 * Configured once at startup with the transport it should use, and handed to the
 * application through `<ApiProvider>`. Nothing reaches for a module-level singleton, so a
 * test renders a subtree against a recorded transport without touching global state, and
 * the knowledge of *how* a request travels stays in exactly one object.
 */
export class ApiDispatcher {
  constructor(
    private readonly transport: Transport,
    private readonly signal?: AbortSignal,
  ) {}

  /**
   * A clone of this dispatcher bound to a cancellation signal.
   *
   * Cancellation belongs to the *caller* — TanStack Query hands a query function an
   * AbortSignal and expects it to be honoured — but it is nothing to do with what a
   * request is. Carrying it on the dispatcher means an action is `(d) => d.call(…)` and
   * never has to accept or forward a signal.
   *
   * Signals *merge* rather than replace, so a composite action already running under a
   * caller's signal can narrow further and both stay live: whichever aborts first wins,
   * which is the only behaviour that is ever correct. Replacing would silently discard the
   * outer cancellation and leave a request running after its owner had given up on it.
   */
  withSignal(signal: AbortSignal): ApiDispatcher {
    const merged = this.signal
      ? AbortSignal.any([this.signal, signal])
      : signal;
    return new ApiDispatcher(this.transport, merged);
  }

  /** Run a request and either return its parsed body or throw an {@link ApiError}. */
  async call<T>(request: ApiRequest): Promise<T> {
    const { method, path, query, body } = request;

    const response = await this.transport.send(method, path, {
      ...(query ? { query } : {}),
      ...(body !== undefined ? { body } : {}),
      ...(this.signal ? { signal: this.signal } : {}),
    });

    const text = await response.text();
    if (!response.ok) {
      throw new ApiError(response.status, messageFrom(text, response.status));
    }
    // `null` for an empty body, which is what an endpoint documented as answering with
    // nothing amounts to on this side.
    return (text ? (JSON.parse(text) as unknown) : null) as T;
  }
}
