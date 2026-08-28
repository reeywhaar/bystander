import type { ReactNode } from "react";

import type { Me } from "@app/api/types";
import { Logo } from "@app/components/Logo";
import { PersonIcon } from "@app/components/icons/PersonIcon";

/**
 * The band across the top of every island.
 *
 * The links between islands are ordinary anchors, not client-side routes: each island is a
 * separate document, and that separation is the point.
 */
export function Masthead({
  me,
  subtitle,
  children,
}: {
  me: Me;
  subtitle?: ReactNode;
  children?: ReactNode;
}) {
  return (
    <header className="border-b border-rule">
      {/* On a wide screen the nav sits at the right, opposite the name. On a narrow one it
          wraps, and `ml-auto` would leave it clinging to the right edge with a hole beside
          it — so below `sm` it starts at the same left edge as everything else. */}
      <div className="mx-auto flex max-w-[1400px] flex-wrap items-center gap-x-6 gap-y-3 px-6 py-5">
        {/* Sized by height; the lockup is a little over five to one, so the width follows.
            Shorter than the 30px the wordmark was set at, because the logo carries the article
            above the name and the head beside it — the same type in this arrangement is half
            again as tall a block. */}
        {/* The row is centred, so the logo is set at its whole box — the one that runs from
            the top of the article to the tail of the y. Cropping it to the baseline would
            centre a box that is not the drawing, which puts the drawing low by the tail.

            35px, which is the 28 the name was set at plus the descender the box gets back. */}
        <a
          href="/"
          aria-label="the bystander"
          className="inline-flex text-ink hover:text-accent"
        >
          <Logo className="h-[35px] w-auto" />
        </a>
        {subtitle ? (
          <p className="basis-full text-sm text-ink-muted sm:basis-auto">
            {subtitle}
          </p>
        ) : null}

        <div className="flex basis-full items-baseline gap-4 text-sm sm:ml-auto sm:basis-auto">
          {children}
          <a href="/manage" className="text-ink-muted hover:text-ink">
            Settings
          </a>
          {me.role === "admin" ? (
            <a href="/admin" className="text-ink-muted hover:text-ink">
              Admin
            </a>
          ) : null}
          {/* Who you are, and the way to the page that says what that means — including
              the way out. Sign out used to live here, one slip away from being pressed by
              somebody aiming at the link beside it, and in exchange for that risk it told
              nobody anything. */}
          <a
            href="/manage/account"
            className="flex items-center gap-1.5 text-ink-muted hover:text-ink"
          >
            <PersonIcon />
            {me.username}
          </a>
        </div>
      </div>
    </header>
  );
}
