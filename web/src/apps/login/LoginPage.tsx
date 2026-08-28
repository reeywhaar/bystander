import { useQuery } from "@tanstack/react-query";
import { useState, type FormEvent } from "react";

import { getPublicInstance, postLogin } from "@app/api/actions/auth";
import { useApiCall } from "@app/api/provider";
import { Alert } from "@app/components/ui/Alert";
import { Button } from "@app/components/ui/Button";
import { Field } from "@app/components/ui/Field";
import { safeNext } from "@app/lib/redirect";
import { qk } from "@app/queries/keys";

import { Frame } from "@app/apps/login/Frame";

export function LoginPage() {
  const callApi = useApiCall();
  // Whether this instance can mail a way back into an account. Offered only where it can:
  // a link to a form that takes an address and says "check your inbox" on an instance with
  // no relay is a promise it cannot keep, and somebody locked out would wait on it.
  //
  // The form does not wait for the answer. Signing in is what almost everybody came for, and
  // one extra request must not hold it up — the link appears when the answer arrives.
  const instance = useQuery({
    queryKey: qk.publicInstance,
    queryFn: ({ signal }) => callApi(getPublicInstance(), signal),
    retry: false,
  });
  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

  async function submit(event: FormEvent) {
    event.preventDefault();
    setBusy(true);
    setError(null);
    try {
      await callApi(postLogin(username, password));
      // A whole-document navigation, not a route: the reader is a different bundle, which
      // is the entire point of shipping this one on its own.
      window.location.href = safeNext(window.location.search);
    } catch (failure) {
      setError(
        failure instanceof Error ? failure.message : "that did not work",
      );
      setBusy(false);
    }
  }

  return (
    <Frame title="Sign in">
      <form
        onSubmit={(event) => void submit(event)}
        className="flex flex-col gap-4"
      >
        {error ? <Alert>{error}</Alert> : null}

        <Field
          label="Name"
          value={username}
          autoComplete="username"
          autoFocus
          required
          onChange={(event) => setUsername(event.target.value)}
        />
        <Field
          label="Password"
          type="password"
          value={password}
          autoComplete="current-password"
          required
          onChange={(event) => setPassword(event.target.value)}
        />
        <Button type="submit" variant="primary" disabled={busy}>
          {busy ? "Signing in…" : "Sign in"}
        </Button>

        {instance.data?.recovery ? (
          <p className="text-sm text-ink-muted">
            <a className="text-accent underline" href="/forgot">
              Forgotten your password?
            </a>
          </p>
        ) : null}
      </form>
    </Frame>
  );
}
