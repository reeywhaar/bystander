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
 * Colour carries none of the meaning on its own. The palette here has one accent and no green,
 * and a traffic light would be the only one in the product; more to the point, position already
 * says it and a second signal that only some people can see is not a second signal.
 */
export type Stance = "exclude" | "neutral" | "include";

/** Where the knob sits, in pixels, in a track 56 wide with a knob 20 across. */
const AT: Record<Stance, number> = { exclude: 2, neutral: 18, include: 34 };

const TRACK: Record<Stance, string> = {
  exclude: "bg-ink/10",
  neutral: "bg-paper-sunken",
  include: "bg-accent/20",
};

const KNOB: Record<Stance, string> = {
  exclude: "bg-ink-faint",
  neutral: "bg-ink-faint/50",
  include: "bg-accent",
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
        className={`pointer-events-none absolute top-0.5 h-5 w-5 rounded-full transition-[left,background-color] duration-150 ${KNOB[value]}`}
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
