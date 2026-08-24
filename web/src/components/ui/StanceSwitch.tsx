/**
 * What a page does with one tag or one feed: take it, ignore it, or drop it.
 *
 * Three states rather than two lists of ticks. The two-list arrangement this replaced could
 * express the same thing and let somebody tick a name on both of them, which is a contradiction
 * the server has to refuse and an error nobody should have been able to reach. One control per
 * name cannot hold two answers.
 *
 * A switch rather than three buttons, because the three states are ordered — drop, nothing,
 * take — and a control laid out along that order says which is which by position before anybody
 * reads a label. The knob is the whole of the state: left, middle, right.
 *
 * Green right and red left, which is the one place this product spends a colour that means
 * something on its own — see --positive and --negative in styles.css. A switch answering
 * taken-or-dropped is answering a question people have been answering in those two colours for
 * longer than they have been using this, and house style is not worth a legend.
 *
 * Position carries the same meaning independently, so nothing here depends on telling green
 * from red: left is off, middle is nothing, right is on.
 */
export type Stance = "exclude" | "neutral" | "include";

/**
 * Where the knob sits, measured inside the border rather than outside it.
 *
 * The track is 56 wide with a 1px border, so what the knob is positioned within is 54; a 20px
 * knob then sits 1 from either end and 17 from the left when centred. Measuring from the outer
 * width instead left it a pixel adrift at every position — near enough to look like a mistake
 * and not near enough to be one you could name.
 */
const AT: Record<Stance, number> = { exclude: 1, neutral: 17, include: 33 };

const TRACK: Record<Stance, string> = {
  exclude: "bg-negative/15",
  neutral: "bg-paper-sunken",
  include: "bg-positive/15",
};

const KNOB: Record<Stance, string> = {
  exclude: "bg-negative",
  // Not a faded version of the other two. The middle position means "no opinion", not "not
  // really there", and a knob you have to look for is a switch whose state you have to look
  // for. Half-strength ink-faint measured 1.69:1 against its own track in the dark palette,
  // which is invisible; ink-muted is 5.0:1 light and 6.5:1 dark, comfortably past the 3:1 that
  // a control's own shape is meant to clear.
  neutral: "bg-ink-muted",
  include: "bg-positive",
};

const ORDER: Stance[] = ["exclude", "neutral", "include"];

export function StanceSwitch({
  value,
  onChange,
  /** What this switch is about, for the labels a screen reader reads out. */
  name,
  /** What each position means here, since a tag and a feed do different things with it. */
  says,
}: {
  value: Stance;
  onChange: (value: Stance) => void;
  name: string;
  says: Record<Stance, string>;
}) {
  return (
    // A radiogroup rather than three buttons: these are one question with three answers, and
    // that is what a screen reader should be told. Each position keeps its own label, so the
    // answer is readable without having to work out what "middle" means here.
    <div
      role="radiogroup"
      aria-label={name}
      className={`relative h-6 w-14 shrink-0 rounded-full border border-rule ${TRACK[value]}`}
    >
      <span
        aria-hidden
        // Centred against the border box rather than offset from the top, so the knob cannot
        // drift by the width of the border the way it did horizontally.
        className={`pointer-events-none absolute top-1/2 h-5 w-5 -translate-y-1/2 rounded-full transition-[left,background-color] duration-150 ${KNOB[value]}`}
        style={{ left: AT[value] }}
      />
      {ORDER.map((stance) => (
        <button
          key={stance}
          type="button"
          role="radio"
          aria-checked={value === stance}
          aria-label={`${name}: ${says[stance]}`}
          title={says[stance]}
          onClick={() => onChange(stance)}
          // A third of the track each, so the gesture is "press where you want it" rather
          // than "press to cycle" — cycling through a state you did not want is how a
          // three-state control becomes annoying.
          className="absolute inset-y-0 w-1/3 cursor-pointer rounded-full"
          style={{ left: `${ORDER.indexOf(stance) * 33.3333}%` }}
        />
      ))}
    </div>
  );
}

/** The stance a list of ids implies for one id. */
export function stanceOf(
  id: string,
  include: string[],
  exclude: string[],
): Stance {
  if (include.includes(id)) return "include";
  if (exclude.includes(id)) return "exclude";
  return "neutral";
}

/**
 * The two lists with one id moved to the side it now belongs on.
 *
 * Removed from both first, so a name can never end up on both — which is the contradiction the
 * server refuses and the reason this is one control rather than two.
 */
export function withStance(
  id: string,
  stance: Stance,
  include: string[],
  exclude: string[],
): { include: string[]; exclude: string[] } {
  const without = (list: string[]) => list.filter((x) => x !== id);
  return {
    include:
      stance === "include" ? [...without(include), id] : without(include),
    exclude:
      stance === "exclude" ? [...without(exclude), id] : without(exclude),
  };
}
