import { createFileRoute } from "@tanstack/react-router";
import { auditSettingsQuery } from "../../../api/audit";
import { AuditLog } from "../../../features/audit/AuditLog";

export const Route = createFileRoute("/_app/server/audit")({
  loader: ({ context: { queryClient } }) => queryClient.ensureQueryData(auditSettingsQuery),
  component: AuditLog,
});
