import jsQR from "jsqr";
import { describe, expect, it } from "vitest";

import { qrMatrix } from "@app/components/ui/QRCode";

/**
 * Renders a matrix the way a camera would receive it: black squares on white, several
 * pixels per module, as RGBA.
 *
 * The scale matters. A decoder locates the finder patterns by their proportions, and one
 * pixel per module leaves it nothing to work with — which is also true of the real thing,
 * and the reason the dialog shows this as large as it will go.
 */
function raster(matrix: boolean[][], scale = 4): ImageData {
  const size = matrix.length * scale;
  const data = new Uint8ClampedArray(size * size * 4);
  for (let y = 0; y < size; y++) {
    for (let x = 0; x < size; x++) {
      const dark =
        matrix[Math.floor(y / scale)]?.[Math.floor(x / scale)] ?? false;
      const value = dark ? 0 : 255;
      const at = (y * size + x) * 4;
      data[at] = value;
      data[at + 1] = value;
      data[at + 2] = value;
      data[at + 3] = 255;
    }
  }
  return { data, width: size, height: size, colorSpace: "srgb" };
}

const read = (value: string) => {
  const image = raster(qrMatrix(value));
  return jsQR(image.data, image.width, image.height)?.data;
};

describe("QRCode", () => {
  // Decoded by a different library than the one that encoded it. Asserting that some
  // rectangles were drawn would prove nothing about the only thing this component is for,
  // which is that somebody else's phone can read it.
  it("holds a link a scanner can read back", () => {
    const url =
      "https://read.example.com/share/8Kx2QhVn4pLmR7sTzYbW1cD9fG3jH5kA6nP0qU8vX2Y";
    expect(read(url)).toBe(url);
  });

  it("holds a link with nothing in it but the origin", () => {
    expect(read("https://example.com/")).toBe("https://example.com/");
  });

  // A self-hosted instance can be at any address, and some of them are long.
  it("holds a link long enough to need a bigger code", () => {
    const url =
      "https://reader.someones-quite-long-personal-domain.example.org:8443" +
      "/share/8Kx2QhVn4pLmR7sTzYbW1cD9fG3jH5kA6nP0qU8vX2Y";
    expect(read(url)).toBe(url);
  });

  it("keeps the quiet zone a scanner finds the code by", () => {
    const matrix = qrMatrix("https://example.com/");
    const size = matrix.length;

    // Four clear modules on every side. Without them a decoder cannot tell where the code
    // ends, and this is the failure that only shows up on somebody else's phone.
    for (let i = 0; i < size; i++) {
      for (let j = 0; j < 4; j++) {
        expect(matrix[j]?.[i]).toBe(false);
        expect(matrix[size - 1 - j]?.[i]).toBe(false);
        expect(matrix[i]?.[j]).toBe(false);
        expect(matrix[i]?.[size - 1 - j]).toBe(false);
      }
    }
  });
});
