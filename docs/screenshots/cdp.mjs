// A DevTools protocol client, in about forty lines.
//
// Shared by capture.mjs and shoot.mjs because both need to ask a running Chromium what it has
// actually drawn, and neither needs anything else — Chromium ships a headless mode and Node has
// a WebSocket client, so there is nothing here for Playwright to do.
//
// It was inline in shoot.mjs. It moved when capture.mjs needed one evaluate of its own: two
// copies of a protocol handshake is two things to fix when one of them is subtly wrong, and the
// second copy is always the one that is.
import { setTimeout as sleep } from "node:timers/promises";

/**
 * Connect to a headless Chromium already listening on `port`.
 *
 * Polls for the endpoint rather than assuming it: the browser is started as a child process a
 * moment earlier and its debugging socket is not up the instant `spawn` returns.
 */
export async function connect(port, { attempts = 80, every = 200 } = {}) {
  let target;
  for (let i = 0; i < attempts; i++) {
    try {
      const list = await (await fetch(`http://127.0.0.1:${port}/json/list`)).json();
      target = list.find((t) => t.type === "page");
      if (target?.webSocketDebuggerUrl) break;
    } catch {
      /* not up yet */
    }
    await sleep(every);
  }
  if (!target?.webSocketDebuggerUrl) {
    throw new Error(`chromium devtools endpoint never came up on :${port}`);
  }

  const ws = new WebSocket(target.webSocketDebuggerUrl);
  await new Promise((ok, bad) => {
    ws.onopen = ok;
    ws.onerror = bad;
  });

  let nextId = 1;
  const pending = new Map();
  const listeners = new Map();

  ws.onmessage = (ev) => {
    const msg = JSON.parse(ev.data);
    if (msg.id && pending.has(msg.id)) {
      const { resolve, reject } = pending.get(msg.id);
      pending.delete(msg.id);
      msg.error ? reject(new Error(JSON.stringify(msg.error))) : resolve(msg.result);
    } else if (msg.method && listeners.has(msg.method)) {
      listeners.get(msg.method).forEach((fn) => fn(msg.params));
      listeners.delete(msg.method);
    }
  };

  const send = (method, params = {}) =>
    new Promise((resolve, reject) => {
      const id = nextId++;
      pending.set(id, { resolve, reject });
      ws.send(JSON.stringify({ id, method, params }));
    });

  /** Resolve the next time Chromium emits `method`. Armed before the action that causes it. */
  const once = (method) =>
    new Promise((resolve) => {
      if (!listeners.has(method)) listeners.set(method, []);
      listeners.get(method).push(resolve);
    });

  async function evaluate(expression) {
    const { result, exceptionDetails } = await send("Runtime.evaluate", {
      expression,
      awaitPromise: true,
      returnByValue: true,
    });
    if (exceptionDetails) throw new Error(`${exceptionDetails.text}\n${expression}`);
    return result.value;
  }

  return { send, once, evaluate, close: () => ws.close() };
}
