export type SchedulePreset = "hourly" | "daily" | "weekly";

/** schedulePresets are the cron expressions the backup form offers besides a custom one. */
export const schedulePresets: { id: SchedulePreset; label: string; cron: string; title: string }[] =
  [
    { id: "hourly", label: "Hourly", cron: "0 * * * *", title: "Every hour, on the hour" },
    { id: "daily", label: "Daily", cron: "0 3 * * *", title: "Every day at 03:00 UTC" },
    { id: "weekly", label: "Weekly", cron: "0 3 * * 0", title: "Every Sunday at 03:00 UTC" },
  ];

/** presetFor returns the preset a cron expression equals, ignoring extra whitespace, or "custom". */
export function presetFor(schedule: string): SchedulePreset | "custom" {
  const normalized = schedule.trim().split(/\s+/).join(" ");
  return schedulePresets.find((p) => p.cron === normalized)?.id ?? "custom";
}
