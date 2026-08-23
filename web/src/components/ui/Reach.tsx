import { describeWindow, shortWindow, windowWeight } from "@app/lib/constants";

/**
 * How far back a feed reaches, as a label.
 *
 * It used to be a phrase in the middle of the summary line — "reaches back a week" — which
 * made the longest thing on that line the one that says the least. As a label it is
 * scannable down a column of feeds, which is how somebody actually reads this: not "what is
 * this feed's window" but "which of these reaches further than I meant".
 *
 * The phrase is not lost, it moves to the title. A label this short is only obvious once you
 * already know what it means.
 */
export function Reach({ seconds }: { seconds: number }) {
  const short = shortWindow(seconds);
  if (short === "") return null;

  // Mild on purpose. This sits in a line of grey text on a page whose whole argument is
  // that it does not ask for attention, so the strongest of these is still a tint.
  const tone = [
    "border-rule text-ink-faint",
    "border-rule bg-paper-sunken text-ink-muted",
    "border-accent-quiet/40 bg-accent/8 text-accent-quiet",
  ][windowWeight(seconds)];

  return (
    <span
      title={describeWindow(seconds)}
      className={`rounded border px-1.5 py-px align-baseline text-[0.6875rem] ${tone}`}
    >
      {short}
    </span>
  );
}
