import { PersonFillIcon } from "@app/components/icons/PersonFillIcon";

/**
 * Somebody's name, with the mark that says it is a person's.
 *
 * A span, so it drops inside whatever already carries the colour and the click. Inline rather
 * than a flex row, because the text has to be what carries the baseline — see the test.
 */
export function UserLabel({
  username,
  className = "",
}: {
  username: string;
  className?: string;
}) {
  return (
    <span className={className}>
      <PersonFillIcon className="mr-[0.2em] inline align-[-0.125em]" />
      {username}
    </span>
  );
}
