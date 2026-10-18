import { describe, expect, it } from "vitest";
import { formatTimestamp, relativeAge } from "./timeDisplay";

describe("explicit timezone formatting", () => {
  it("formats the requested Bogotá instant without using browser timezone", () => {
    expect(formatTimestamp("2026-07-17T19:32:10Z", "America/Bogota")).toEqual({
      visible: "2026-07-17 14:32:10 COT",
      offset: "UTC-05:00",
    });
  });

  it("formats UTC exactly", () => {
    expect(formatTimestamp("2026-07-17T19:32:10Z", "UTC")).toEqual({
      visible: "2026-07-17 19:32:10 UTC",
      offset: "UTC+00:00",
    });
  });

  it("uses the date-specific daylight-saving offset", () => {
    expect(formatTimestamp("2026-07-17T19:32:10Z", "America/New_York").offset).toBe("UTC-04:00");
    expect(formatTimestamp("2026-01-17T19:32:10Z", "America/New_York").offset).toBe("UTC-05:00");
  });

  it("derives relative age from the UTC instant", () => {
    const now = Date.parse("2026-07-17T19:32:10Z");
    expect(relativeAge("2026-07-17T19:32:05Z", now)).toBe("just now");
    expect(relativeAge("2026-07-17T19:31:58Z", now)).toBe("12 s ago");
    expect(relativeAge("2026-07-17T19:28:10Z", now)).toBe("4 min ago");
    expect(relativeAge("2026-07-17T16:20:10Z", now)).toBe("3 h 12 min ago");
    expect(relativeAge("2026-07-15T19:32:10Z", now)).toBe("2 d ago");
  });
});
