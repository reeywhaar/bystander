/**
 * A refusal from the server, carrying the sentence it sent.
 *
 * The server writes those sentences for the person who will read them, so they are shown
 * as they arrive rather than being translated into an interface's own vocabulary — which
 * would mean maintaining two vocabularies and letting them drift.
 */
export class ApiError extends Error {
  constructor(
    readonly status: number,
    message: string,
  ) {
    super(message);
    this.name = "ApiError";
  }

  /** Not signed in. The one status handled centrally — see {@link ApiProvider}. */
  get unauthorized(): boolean {
    return this.status === 401;
  }

  /** Refused because of what is already true, rather than because of a mistake. */
  get conflict(): boolean {
    return this.status === 409;
  }
}

/** Pulls the server's sentence out of a response body, falling back to the status. */
export function messageFrom(text: string, status: number): string {
  try {
    const parsed: unknown = JSON.parse(text);
    if (parsed && typeof parsed === "object" && "error" in parsed) {
      const message = (parsed as { error: unknown }).error;
      if (typeof message === "string" && message !== "") return message;
    }
  } catch {
    // Not JSON. A proxy in front of us answering with its own HTML error page is the
    // usual reason, and its markup is not something to show anybody.
  }
  return `the server answered ${status}`;
}
