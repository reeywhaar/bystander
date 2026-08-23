import { describe, expect, it } from "vitest";

import { safeNext } from "@app/lib/redirect";

describe("safeNext", () => {
  it("honours a path on this origin", () => {
    expect(safeNext("?next=%2Fmanage")).toBe("/manage");
    expect(safeNext("?next=%2Fmanage%2Ftags%3Fa%3Db")).toBe("/manage/tags?a=b");
  });

  it("falls back when there is nothing to honour", () => {
    expect(safeNext("")).toBe("/");
    expect(safeNext("?next=")).toBe("/");
  });

  // Each of these would turn the login page into somebody else's redirect.
  it("refuses anything that could leave this origin", () => {
    for (const hostile of [
      "https://evil.example",
      "//evil.example",
      "/\\evil.example",
      "http:/evil.example",
      "javascript:alert(1)",
      "evil.example",
    ]) {
      expect(safeNext(`?next=${encodeURIComponent(hostile)}`)).toBe("/");
    }
  });
});
