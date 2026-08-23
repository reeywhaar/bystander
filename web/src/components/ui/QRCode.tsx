import qrcode from "qrcode-generator";

/**
 * The quiet zone, in modules.
 *
 * Four is what the specification requires, and it is not decoration: a scanner finds the
 * code by its border, so a QR pressed against surrounding content is a QR that often will
 * not read.
 */
const QUIET = 4;

/**
 * The module matrix for a value, quiet zone included, as rows of booleans.
 *
 * Separate from the drawing so a test can decode what a camera would see rather than
 * asserting that some rectangles exist.
 */
export function qrMatrix(value: string): boolean[][] {
  // Version 0 is "as small as will hold it". Error correction M — the middle setting, which
  // is what almost every QR in the world uses: enough redundancy for a screen photographed
  // at an angle, without inflating a link into a wall of dots.
  const qr = qrcode(0, "M");
  qr.addData(value);
  qr.make();

  const count = qr.getModuleCount();
  const size = count + QUIET * 2;
  return Array.from({ length: size }, (_, y) =>
    Array.from(
      { length: size },
      (_, x) =>
        y >= QUIET &&
        x >= QUIET &&
        y < QUIET + count &&
        x < QUIET + count &&
        qr.isDark(y - QUIET, x - QUIET),
    ),
  );
}

/**
 * A link as a square, for the camera in somebody else's hand.
 *
 * SVG rather than a canvas: it is a grid of squares, it stays crisp at whatever size the
 * dialog gives it, and it needs no pixel ratio arithmetic to survive a retina screen.
 *
 * Black on white, always, whatever the page's theme is. A scanner expects dark modules on a
 * light field, and enough of them fail on an inverted code that following the palette here
 * would be choosing consistency over the one thing this element is for.
 */
export function QRCode({
  value,
  className = "",
}: {
  value: string;
  className?: string;
}) {
  const matrix = qrMatrix(value);
  const size = matrix.length;

  // One path for every dark module rather than an element each: a version 8 code is over a
  // thousand squares, and a thousand DOM nodes to draw a static square is a thousand more
  // than it takes.
  let d = "";
  matrix.forEach((row, y) => {
    row.forEach((dark, x) => {
      if (dark) d += `M${x} ${y}h1v1h-1z`;
    });
  });

  return (
    <svg
      viewBox={`0 0 ${size} ${size}`}
      // Crisp at any scale, because a module boundary landing mid-pixel is what makes a
      // small code fail to read.
      shapeRendering="crispEdges"
      role="img"
      aria-label="A code holding this link"
      className={className}
    >
      <rect width={size} height={size} fill="#ffffff" />
      <path d={d} fill="#000000" />
    </svg>
  );
}
