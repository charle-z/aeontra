import { createContext, useContext, type ReactNode } from "react";
import { DEFAULT_TIMEZONE, formatTimestamp, relativeAge } from "./timeDisplay";

export type TimeDisplayState = {
  timezone: string;
  now: number;
};

const TimeDisplayContext = createContext<TimeDisplayState>({ timezone: DEFAULT_TIMEZONE, now: Date.now() });

export function TimeDisplayProvider({ value, children }: { value: TimeDisplayState; children: ReactNode }) {
  return <TimeDisplayContext.Provider value={value}>{children}</TimeDisplayContext.Provider>;
}

export function Timestamp({ value }: { value: string }) {
  const display = useContext(TimeDisplayContext);
  if (!value) return <>—</>;
  const formatted = formatTimestamp(value, display.timezone);
  const relative = relativeAge(value, display.now);
  return (
    <span className="timestamp-display">
      <time dateTime={value} title={value}>{formatted.visible}</time>
      <small className="timestamp-offset">{formatted.offset}</small>
      <small className="timestamp-relative">{relative}</small>
    </span>
  );
}
