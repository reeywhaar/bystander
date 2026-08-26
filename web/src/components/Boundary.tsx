import { Component, type ErrorInfo, type ReactNode } from "react";

import { Colophon } from "@app/components/Colophon";
import { Alert } from "@app/components/ui/Alert";
import { Button } from "@app/components/ui/Button";

/**
 * Catches what a render threw, and says so.
 *
 * Without this, a failed query takes the whole document down to a blank page — which
 * leaves somebody unable to tell "there is nothing here" from "this broke", the one
 * distinction an error state exists to make.
 */
export class Boundary extends Component<
  { children: ReactNode },
  { error: Error | null }
> {
  state: { error: Error | null } = { error: null };

  static getDerivedStateFromError(error: Error) {
    return { error };
  }

  componentDidCatch(error: Error, info: ErrorInfo) {
    // The console is where somebody debugging will look, and the stack is the half the
    // rendered message deliberately does not show.
    console.error("bystander:", error, info.componentStack);
  }

  render() {
    if (!this.state.error) return this.props.children;

    return (
      <div className="mx-auto max-w-lg px-6 py-16">
        <Alert>{this.state.error.message}</Alert>
        <div className="mt-4 flex gap-2">
          <Button variant="primary" onClick={() => window.location.reload()}>
            Try again
          </Button>
        </div>
        {/* Here too, and this is the case that most needs it: a boundary replaces the whole
            island, so without it this document says nothing anywhere about what it is — and
            somebody looking at a broken instance is somebody who may be trying to find out. */}
        <Colophon className="mt-10" />
      </div>
    );
  }
}
