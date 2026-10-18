export const DEFAULT_TIMEZONE = "America/Bogota";

const abbreviations: Record<string, string> = {
  "America/Bogota": "COT",
  "America/Argentina/Buenos_Aires": "ART",
  "Europe/Moscow": "MSK",
  UTC: "UTC",
};

function partsFor(value: Date, timezone: string): Record<string, string> {
  const formatter = new Intl.DateTimeFormat("en-CA", {
    timeZone: timezone,
    year: "numeric",
    month: "2-digit",
    day: "2-digit",
    hour: "2-digit",
    minute: "2-digit",
    second: "2-digit",
    hourCycle: "h23",
  });
  const result: Record<string, string> = {};
  for (const part of formatter.formatToParts(value)) {
    if (part.type !== "literal") result[part.type] = part.value;
  }
  return result;
}

function derivedAbbreviation(value: Date, timezone: string): string {
  if (abbreviations[timezone]) return abbreviations[timezone];
  const formatter = new Intl.DateTimeFormat("en-US", { timeZone: timezone, timeZoneName: "short" });
  const name = formatter.formatToParts(value).find((part) => part.type === "timeZoneName")?.value;
  return name || timezone;
}

function timezoneOffset(value: Date, timezone: string): string {
  if (timezone === "UTC") return "UTC+00:00";
  const formatter = new Intl.DateTimeFormat("en-US", { timeZone: timezone, timeZoneName: "longOffset" });
  const raw = formatter.formatToParts(value).find((part) => part.type === "timeZoneName")?.value || "GMT";
  if (raw === "GMT") return "UTC+00:00";
  return raw.replace(/^GMT/, "UTC");
}

export type FormattedTimestamp = {
  visible: string;
  offset: string;
};

export function formatTimestamp(value: string, timezone: string): FormattedTimestamp {
  const instant = new Date(value);
  if (!Number.isFinite(instant.getTime())) return { visible: "—", offset: "" };
  const parts = partsFor(instant, timezone);
  const visible = `${parts.year}-${parts.month}-${parts.day} ${parts.hour}:${parts.minute}:${parts.second} ${derivedAbbreviation(instant, timezone)}`;
  return { visible, offset: timezoneOffset(instant, timezone) };
}

export function relativeAge(value: string, nowMilliseconds: number): string {
  const instant = Date.parse(value);
  if (!Number.isFinite(instant)) return "";
  const elapsed = Math.max(0, Math.floor((nowMilliseconds - instant) / 1000));
  if (elapsed < 10) return "just now";
  if (elapsed < 60) return `${elapsed} s ago`;
  const minutes = Math.floor(elapsed / 60);
  if (minutes < 60) return `${minutes} min ago`;
  const hours = Math.floor(minutes / 60);
  if (hours < 48) {
    const remainder = minutes % 60;
    return remainder ? `${hours} h ${remainder} min ago` : `${hours} h ago`;
  }
  return `${Math.floor(hours / 24)} d ago`;
}
