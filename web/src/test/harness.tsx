import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, type RenderResult } from "@testing-library/react";
import type { ReactNode } from "react";

import { ApiDispatcher } from "@app/api/dispatcher";
import { ApiProvider } from "@app/api/provider";
import type { Method, RequestOptions, Transport } from "@app/api/transport";

/** One canned answer: a status and a body, keyed by "METHOD /path". */
export type Recording = Record<string, { status?: number; body?: unknown }>;

/**
 * A transport that answers from a script and records what it was asked.
 *
 * This is the reason `Transport` is an interface with one implementation, and the reason
 * the dispatcher is injected rather than imported: a test renders a subtree against this
 * without `vi.stubGlobal("fetch")`, which is global state two tests would eventually fight
 * over.
 */
export class RecordedTransport implements Transport {
  readonly calls: { method: Method; path: string; body?: unknown }[] = [];

  /**
   * Mutable, so a test can change what a later request answers.
   *
   * That is the only way to exercise the thing an invalidation is *for*: a write happens,
   * the list refetches, and the interface has to show what came back rather than what it
   * was holding.
   */
  constructor(readonly recording: Recording) {}

  send(
    method: Method,
    path: string,
    opts: RequestOptions = {},
  ): Promise<Response> {
    this.calls.push({ method, path, body: opts.body });

    const answer = this.recording[`${method} ${path}`];
    if (!answer) {
      // Louder than a 404: an unscripted request means the test and the component
      // disagree about what this screen does, which is worth failing over rather than
      // rendering an error state that looks plausible.
      return Promise.resolve(
        new Response(
          JSON.stringify({ error: `nothing recorded for ${method} ${path}` }),
          {
            status: 501,
          },
        ),
      );
    }
    const status = answer.status ?? 200;
    // 204, 205 and 304 are null-body statuses, and the Response constructor throws rather
    // than tolerating an empty string for them — which is a real distinction, since the
    // API answers 204 to every write that returns nothing.
    const nullBody = status === 204 || status === 205 || status === 304;
    const body =
      nullBody || answer.body === undefined
        ? null
        : JSON.stringify(answer.body);
    return Promise.resolve(new Response(body, { status }));
  }
}

/** Renders a subtree against a recorded transport. */
export function renderWith(
  node: ReactNode,
  recording: Recording,
): RenderResult & {
  transport: RecordedTransport;
} {
  const transport = new RecordedTransport(recording);
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });

  const result = render(
    <QueryClientProvider client={client}>
      <ApiProvider dispatcher={new ApiDispatcher(transport)}>
        {node}
      </ApiProvider>
    </QueryClientProvider>,
  );
  return { ...result, transport };
}
