import { Alert } from "@app/components/ui/Alert";
import { Spinner } from "@app/components/ui/Spinner";
import { StanceSwitch, type Stance } from "@app/components/ui/StanceSwitch";
import { useInstance, useSetInstance } from "@app/queries/hooks";

const SAYS: Record<Stance, string> = {
  exclude: "no",
  neutral: "no",
  include: "yes",
};

/**
 * What this instance serves to strangers.
 *
 * Three switches, and one of them starts in the other position. Publishing and indexing are
 * exposure — who may put a page on the open web, and whether a search engine may keep it — and
 * both start at no, because a default of yes there decides something on somebody's behalf. The
 * asymmetry between those two is its own point: publishing is reversible and indexing is not,
 * so indexing needs two yeses and publishing needs one.
 *
 * The landing page is not exposure. It decides what the front door *says*, and a door that
 * explains itself is the better default — so it starts on, and turning it off is the choice.
 */
export function PublishingPage() {
  const instance = useInstance();
  const save = useSetInstance();

  if (instance.isPending) return <Spinner />;
  if (instance.error) throw instance.error;

  const settings = instance.data;

  return (
    <div className="flex flex-col gap-8">
      <section className="flex flex-col gap-3">
        <h2 className="font-serif text-xl text-ink">The front door</h2>
        <p className="max-w-prose text-sm text-ink-muted">
          What somebody without an account gets at{" "}
          <span className="font-mono text-xs">/</span>. With this on it is a
          page saying what this is and why it works the way it does, with a way
          to sign in; with it off it is the sign-in form on its own, which is
          what it used to be.
        </p>

        <Row
          label="Show the landing page"
          hint="Nobody with an account ever sees it — they get their front page."
          on={settings.landing}
          busy={save.isPending}
          onChange={(on) =>
            save.mutate({
              public_pages: settings.public_pages,
              public_indexing: settings.public_indexing,
              landing: on,
            })
          }
        />
      </section>

      <section className="flex flex-col gap-3 border-t border-rule pt-8">
        <h2 className="font-serif text-xl text-ink">Published pages</h2>
        <p className="max-w-prose text-sm text-ink-muted">
          Whether anybody here may put a page on the open web, at{" "}
          <span className="font-mono text-xs">/p/their-name/the-page</span>.
          Nothing becomes public on its own — somebody still has to publish each
          page — but nothing can without this.
        </p>

        <Row
          label="Allow publishing"
          hint="Turning this off takes every published page down, not just new ones."
          on={settings.public_pages}
          busy={save.isPending}
          onChange={(on) =>
            save.mutate({
              landing: settings.landing,
              public_pages: on,
              // Indexing cannot outlive publishing: a page nobody can reach is not a page
              // a search engine should be invited to keep looking for.
              public_indexing: on && settings.public_indexing,
            })
          }
        />
      </section>

      <section className="flex flex-col gap-3 border-t border-rule pt-8">
        <h2 className="font-serif text-xl text-ink">Search engines</h2>
        <p className="max-w-prose text-sm text-ink-muted">
          Whether a published page may ask to be indexed. This is a ceiling, not
          a default: with it off, the choice is not offered to anybody, and no
          page is indexable however it was set. It starts off because this is
          the one decision here that cannot be taken back — a page that has been
          crawled stays in somebody else's cache long after it is taken down.
        </p>

        {!settings.public_pages ? (
          <Alert tone="note">
            Nothing is published here, so there is nothing to index.
          </Alert>
        ) : (
          <Row
            label="Allow indexing"
            hint="Each page still has to ask for it. This only decides whether they may."
            on={settings.public_indexing}
            busy={save.isPending}
            onChange={(on) =>
              save.mutate({
                landing: settings.landing,
                public_pages: settings.public_pages,
                public_indexing: on,
              })
            }
          />
        )}
      </section>

      {save.error ? <Alert>{save.error.message}</Alert> : null}
    </div>
  );
}

function Row({
  label,
  hint,
  on,
  busy,
  onChange,
}: {
  label: string;
  hint: string;
  on: boolean;
  busy: boolean;
  onChange: (on: boolean) => void;
}) {
  return (
    <div className="flex items-center gap-4 rounded-md border border-rule px-3 py-2.5">
      <span className="min-w-0 flex-1">
        <span className="block text-sm text-ink">{label}</span>
        <span className="block text-xs text-ink-faint">{hint}</span>
      </span>
      {/* The same switch the page filter uses, at two of its three positions: this is a yes
          or a no, and there is no "no opinion" for an instance to hold. */}
      <StanceSwitch
        value={on ? "include" : "exclude"}
        onChange={(next) => !busy && onChange(next === "include")}
        name={label}
        says={SAYS}
      />
    </div>
  );
}
