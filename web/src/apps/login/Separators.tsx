import { useEffect, useMemo, useRef, useState } from "react";

/**
 * Two moving rules for the landing page, drawn on a canvas. Nothing else on the site moves.
 *
 * The reader is under a rule that the page holds still — a newspaper does not react to being
 * looked at — and this is the one document that is not the newspaper. It is the argument for it,
 * and an argument is allowed to be paced.
 *
 * A canvas rather than elements, and the wave is the reason. In the DOM the dot rule is a
 * hundred and eighty spans, each running its own infinite CSS animation, and the only way to
 * vary them is to write the variation into a hundred and eighty inline styles. A canvas is one
 * element and one loop: the wave becomes a function of time and position, which is what it
 * actually is, and it costs about the same drawn with fifty dots or five hundred.
 *
 * Both stop under `prefers-reduced-motion` — one frame, no loop — and both stop when scrolled
 * out of view, because a decoration nobody is looking at should not be keeping a phone awake.
 */

/** How fast the train runs, in viewport widths per second. */
const TRAIN_SPEED = 0.16;

/**
 * How much of a car is its tail.
 *
 * The half nearest the leading edge is solid and the half behind it fades out, so a car has a
 * body before it has a trail. Faded from the leading edge instead, the whole car is a gradient
 * and there is nothing left that reads as a stripe — which is the thing the rule is made of.
 */
const TRAIL = 0.5;

/**
 * How many stops the tail is drawn with.
 *
 * A canvas gradient interpolates straight between its stops, so a curve has to be sampled. Nine
 * is past the point where another one changes anything a screen can show.
 */
const TRAIL_STOPS = 8;

/**
 * How solid a car is across its body, before the tail fades away behind it.
 *
 * Faint on purpose. This rule sits between the masthead and the one claim the page is making,
 * and a band of full-strength accent there competes with both — it is meant to be noticed on
 * the way past, not read. A third is a watermark that happens to move.
 */
const TRAIN_ALPHA = 0.32;

/** How many cars are in one pass. Enough that a train does not obviously repeat. */
const CAR_COUNT = 10;

/**
 * mulberry32: small, fast, and well enough distributed for deciding how wide a stripe is.
 *
 * Seeded rather than `Math.random` at draw time, because a train has to be the same train on
 * every frame — its cars are worked out once and then only moved. The same seed gives the same
 * train, so a lane can be pinned by number and looked at again.
 */
function generator(seed: number) {
  let a = seed >>> 0;
  return () => {
    a = (a + 0x6d2b79f5) >>> 0;
    let t = Math.imul(a ^ (a >>> 15), 1 | a);
    t = (t + Math.imul(t ^ (t >>> 7), 61 | t)) ^ t;
    return ((t ^ (t >>> 14)) >>> 0) / 4294967296;
  };
}

/**
 * One train, drawn from a seed.
 *
 * Widths run from a tenth of the viewport to four fifths of it and couplings from a hundredth
 * to a thirtieth, which is what makes a pass uneven enough to watch. `ink` is how far along the
 * page's own two colours a car is mixed — 0 the paper it sits on, 100 the accent at full
 * strength — so the rule runs from a barely-there beige to iron red and every car is a colour
 * the rest of the page already uses.
 *
 * `pace` and `phase` come out of the same stream, so two trains given different seeds differ in
 * speed and in where they start as well as in their cars. Three of them stacked then drift
 * against each other rather than moving as one striped band.
 *
 * `run` is the sum of the whole pass, and the animation slides exactly that far before
 * repeating — which is why it is returned with the cars rather than written down beside them.
 */
function train(seed: number) {
  const next = generator(seed);
  const cars = Array.from({ length: CAR_COUNT }, () => ({
    w: 10 + next() * 68,
    gap: 1 + next() * 2,
    ink: 20 + next() * 80,
  }));
  return {
    cars,
    pace: 0.7 + next() * 0.6,
    phase: next(),
    run: cars.reduce((sum, car) => sum + car.w + car.gap, 0) / 100,
  };
}

/**
 * The dot rule: a dot, and a gap twice as wide.
 *
 * Two pixels at rest, so the rule reads as a hairline of grain rather than a row of beads — the
 * dots are a texture until a wave picks some of them out. It is also why the peaks below go as
 * high as three: from two pixels that is the difference between a dot and a dot you noticed,
 * and it still only reaches six.
 */
const DOT = 2;
const PITCH = DOT * 3;

/**
 * The waves: how long after the one before each sets off, and how fast it then travels.
 *
 * Both are written out, and both are deliberately uneven.
 *
 * A rule that pulses on a fixed beat is a metronome, and a metronome is the one thing on a page
 * that cannot be ignored — the eye locks to it and then waits for it. So the gaps run from just
 * over a second to more than six.
 *
 * `step` is seconds per dot, which is the wave's speed. On a laptop screen the quickest crosses
 * in about a second and the slowest takes over three, and that spread is what lets a wave
 * launched later catch and pass one still travelling. Measured over a whole sequence, the rule
 * is empty about a third of the time and carrying two at once about a fifth of it.
 *
 * `strength` is how hard it hits. A rule where every wave lands the same is a rule with one
 * event in it played over and over; some of these barely lift the dots and one comes through
 * at full height, so what crosses is worth looking up for or not.
 *
 * Nothing here is random. The sequence is the same on every load, so the rule has a rhythm
 * rather than a shuffle.
 */
const WAVES = [
  { gap: 2.4, step: 0.006, strength: 0.85 },
  { gap: 1.3, step: 0.012, strength: 0.45 },
  { gap: 5.1, step: 0.004, strength: 1 },
  { gap: 2.0, step: 0.009, strength: 0.6 },
  { gap: 3.7, step: 0.005, strength: 0.9 },
  { gap: 1.1, step: 0.015, strength: 0.35 },
  { gap: 6.4, step: 0.007, strength: 0.75 },
  { gap: 2.8, step: 0.01, strength: 0.55 },
];

/**
 * How far a dot can be pushed when more than one wave is on it.
 *
 * Waves add where they meet rather than the loudest simply winning, which is what a wave does.
 * Without a ceiling two strong ones crossing would double a dot and the crossing would read as
 * a fault rather than as an event; at half again, it reads as the two of them arriving at once.
 */
const CREST = 1.5;

/** When each wave sets off, counted from the top of the sequence, and how fast it goes. */
const LAUNCHES = WAVES.map((wave, i) => ({
  ...wave,
  at: WAVES.slice(0, i).reduce((sum, w) => sum + w.gap, 0),
}));

/** How long before the whole sequence comes round again. */
const PERIOD = WAVES.reduce((sum, wave) => sum + wave.gap, 0);

/** How long one dot takes to rise, and how much longer to settle. */
const RISE = 0.09;
const FALL = 0.55;

/**
 * How high each dot rises, cycled by position.
 *
 * A wave in which every dot rises to the same height reads as one shape sliding along rather
 * than as a row of things each answering in its own way. Ten values against a swell about
 * fifteen dots wide puts the variation inside a single wave, which is where it can be seen.
 */
const PEAKS = [2.8, 2.1, 3.2, 2.4, 1.9, 2.9, 2.3, 3.1, 2.0, 2.6];

/**
 * Seconds each dot lags behind where an even wave would have put it.
 *
 * The front of a real wave is not a straight line. Without this the swell arrives as a ruled
 * edge travelling sideways.
 */
const SKEWS = [
  0, 0.006, -0.003, 0.009, -0.002, 0.004, -0.006, 0.002, 0.007, -0.004,
];

type RGB = [number, number, number];

/** `#rrggbb` as three numbers. The palette is hex throughout, so nothing else is handled. */
function rgb(hex: string): RGB {
  const v = parseInt(hex.trim().replace("#", ""), 16);
  return [(v >> 16) & 255, (v >> 8) & 255, v & 255];
}

/** One of the page's own custom properties, read off the document. */
function token(name: string) {
  return getComputedStyle(document.documentElement).getPropertyValue(name);
}

function mix(a: RGB, b: RGB, t: number): RGB {
  const at = (i: number) => Math.round(a[i]! + (b[i]! - a[i]!) * t);
  return [at(0), at(1), at(2)];
}

/**
 * The shape of one swell, given how long ago it reached this dot.
 *
 * Straight up and a longer way down. A dot that rises and falls at the same rate reads as
 * breathing; one that snaps up and settles reads as something arriving, which is the difference
 * between a texture and an impulse.
 */
/**
 * easeInSine: nought to one, leaving flat and arriving steep.
 *
 * The tail is eased rather than ramped because a ramp gives a car a hard end. This leaves the
 * tip with almost no slope, so the last of a car thins out into the paper over a long way
 * rather than stopping, and then climbs into the body — which is the shape a wake has.
 *
 * It does arrive at the body still climbing, where a smoothstep would flatten out to meet it.
 * That corner is real and is not worth avoiding: the rise is spread over half a car, so on a
 * fifty-viewport-wide stripe the steepest stretch still runs about forty pixels and there is
 * nothing at the junction a screen can show.
 */
function easeInSine(t: number) {
  return 1 - Math.cos((t * Math.PI) / 2);
}

function impulse(dt: number) {
  if (dt < 0 || dt > RISE + FALL) return 0;
  return dt < RISE ? dt / RISE : 1 - (dt - RISE) / FALL;
}

type Draw = (
  ctx: CanvasRenderingContext2D,
  w: number,
  h: number,
  t: number,
) => void;

/**
 * Runs a draw function against a canvas kept in step with its own box and the display.
 *
 * Everything awkward about a canvas lives here. The backing store is sized in device pixels and
 * the context scaled to match, so a dot drawn at 5 is 5 CSS pixels on any screen; a resize
 * redoes that rather than stretching what was already there; and the loop runs only while the
 * canvas is both on screen and allowed to move.
 */
function useCanvas(draw: Draw) {
  const ref = useRef<HTMLCanvasElement>(null);
  // Held in a ref so the effect can stay on an empty dependency list: the draw function is a
  // new closure every render, and a loop that restarted on each of them would stutter.
  const drawing = useRef(draw);
  drawing.current = draw;

  useEffect(() => {
    const canvas = ref.current;
    const ctx = canvas?.getContext("2d");
    if (!canvas || !ctx) return;

    let width = 0;
    let height = 0;
    const fit = () => {
      const box = canvas.getBoundingClientRect();
      const dpr = window.devicePixelRatio || 1;
      width = box.width;
      height = box.height;
      canvas.width = Math.round(width * dpr);
      canvas.height = Math.round(height * dpr);
      ctx.setTransform(dpr, 0, 0, dpr, 0, 0);
    };

    const once = () => {
      ctx.clearRect(0, 0, width, height);
      drawing.current(ctx, width, height, 0);
    };

    fit();

    const still = !!window.matchMedia?.("(prefers-reduced-motion: reduce)")
      .matches;
    let frame = 0;
    let started: number | null = null;

    const paint = (now: number) => {
      started ??= now;
      ctx.clearRect(0, 0, width, height);
      drawing.current(ctx, width, height, (now - started) / 1000);
      frame = requestAnimationFrame(paint);
    };

    if (still) once();
    else frame = requestAnimationFrame(paint);

    const resize =
      typeof ResizeObserver === "undefined"
        ? null
        : new ResizeObserver(() => {
            fit();
            if (still) once();
          });
    resize?.observe(canvas);

    // Off screen, stop. Coming back restarts the clock rather than resuming it — nobody can
    // tell, and it keeps the elapsed time from jumping by however long the page was read for.
    const watching =
      typeof IntersectionObserver === "undefined"
        ? null
        : new IntersectionObserver(([entry]) => {
            if (still) return;
            if (entry?.isIntersecting) {
              if (!frame) {
                started = null;
                frame = requestAnimationFrame(paint);
              }
            } else if (frame) {
              cancelAnimationFrame(frame);
              frame = 0;
            }
          });
    watching?.observe(canvas);

    // The colours are read fresh on every frame, so a moving rule follows the theme on its own.
    // This is for the still case, where there is no next frame to read them on.
    const scheme = window.matchMedia?.("(prefers-color-scheme: dark)");
    const repaint = () => {
      if (still) once();
    };
    scheme?.addEventListener("change", repaint);

    return () => {
      if (frame) cancelAnimationFrame(frame);
      resize?.disconnect();
      watching?.disconnect();
      scheme?.removeEventListener("change", repaint);
    };
  }, []);

  return ref;
}

/**
 * One rule that goes past like a train: uneven cars, uneven couplings, one direction.
 *
 * Three of these are stacked on the landing page, each its own canvas, its own loop and its own
 * seed. Drawn as three lanes of one canvas instead it was fewer objects and a worse component:
 * the count was baked into the drawing, the stacking could not be spaced by the layout, and a
 * caller who wanted one band could not have one.
 *
 * Left to right, which is the direction the page is read in. The pattern is tiled from one pass
 * behind the left edge to past the right one, so there is no seam to hide and no second copy to
 * keep in step — the modulo does what a duplicated track had to.
 *
 * Every car is solid across its leading half and dissolves into the paper over its trailing
 * one, so what goes past is a run of comets rather than a band.
 */
export function Train({
  seed,
  className = "",
}: {
  /**
   * Which train this is. The same seed is the same train, every time.
   *
   * Optional, and drawn once on mount when it is left out — so a caller who only wants *a*
   * train does not have to invent a number, and one who wants a particular one can name it.
   */
  seed?: number;
  className?: string;
}) {
  const [drawn] = useState(() => Math.floor(Math.random() * 2 ** 31));
  const {
    cars,
    pace,
    phase,
    run: RUN,
  } = useMemo(() => train(seed ?? drawn), [seed, drawn]);

  const ref = useCanvas((ctx, w, h, t) => {
    const paper = rgb(token("--paper"));
    const accent = rgb(token("--accent"));
    const vw = window.innerWidth / 100;
    const run = RUN * window.innerWidth;
    const travelled = t * TRAIN_SPEED * pace * window.innerWidth + phase * run;

    let x = (travelled % run) - run;
    while (x < w) {
      for (const car of cars) {
        const width = car.w * vw;
        if (x + width > 0 && x < w) {
          // Each car is solid across its leading half and eases out over the trailing one, so
          // it has a body and then a tail rather than being a gradient end to end.
          //
          // Per car rather than one gradient over the whole rule. Over the rule the fade is a
          // property of *where you are looking*, and every car dims as it crosses; per car it
          // belongs to the car, so each keeps its own head and tail the whole way across and
          // the couplings stay legible as gaps.
          const [r, g, b] = mix(paper, accent, car.ink / 100);
          const trail = ctx.createLinearGradient(x, 0, x + width, 0);
          for (let k = 0; k <= TRAIL_STOPS; k++) {
            const along = k / TRAIL_STOPS;
            const alpha = TRAIN_ALPHA * easeInSine(along);
            trail.addColorStop(
              along * TRAIL,
              `rgba(${r}, ${g}, ${b}, ${alpha})`,
            );
          }
          trail.addColorStop(1, `rgba(${r}, ${g}, ${b}, ${TRAIN_ALPHA})`);
          ctx.fillStyle = trail;
          ctx.fillRect(x, 0, width, h);
        }
        x += width + car.gap * vw;
      }
    }
  });

  return (
    <canvas
      ref={ref}
      className={`block h-0.5 w-full ${className}`}
      aria-hidden="true"
    />
  );
}

/**
 * A rule of dots with a swell running through it.
 *
 * Every dot answers the same impulse, and each answers a little after the one to its left, so
 * what crosses is a phase rather than an object — the way a wave crosses a rope nobody has moved
 * sideways.
 *
 * The waves do not arrive on a beat, no two travel at the same speed, and no two hit as hard.
 * See WAVES: they come in twos, then not for six seconds, and a fast one launched later will
 * catch and pass a slow one still going — lifting the dots it is passing through further than
 * either would alone. That is what keeps this from being a metronome in the corner of the eye.
 */
export function Pulse({ className = "" }: { className?: string }) {
  const ref = useCanvas((ctx, w, h, t) => {
    const faint = rgb(token("--ink-faint"));
    ctx.fillStyle = `rgb(${faint[0]} ${faint[1]} ${faint[2]})`;
    const mid = h / 2;

    const local = ((t % PERIOD) + PERIOD) % PERIOD;

    for (let i = 0, n = Math.ceil(w / PITCH) + 1; i < n; i++) {
      const skew = SKEWS[i % SKEWS.length]!;

      // Every wave currently over this dot, added. Where two cross, the dot is lifted by both
      // — which is what interference looks like and is the one moment this rule has that is
      // not just a wave going past. CREST is what keeps that from becoming a bulge.
      let swell = 0;
      for (const wave of LAUNCHES) {
        const since = local - wave.at - i * wave.step - skew;
        // The same wave one period back as well, for the slow ones still crossing when the
        // sequence comes round. Only one of the two can be inside the impulse window, so
        // adding them is a way of picking whichever it is.
        swell += wave.strength * (impulse(since) + impulse(since + PERIOD));
      }
      swell = Math.min(swell, CREST);

      const radius = (DOT / 2) * (1 + swell * (PEAKS[i % PEAKS.length]! - 1));

      ctx.globalAlpha = Math.min(1, 0.45 + 0.55 * swell);
      ctx.beginPath();
      ctx.arc(i * PITCH + PITCH / 2, mid, radius, 0, Math.PI * 2);
      ctx.fill();
    }
    ctx.globalAlpha = 1;
  });

  return (
    <canvas
      ref={ref}
      className={`block h-5 w-full ${className}`}
      aria-hidden="true"
    />
  );
}
