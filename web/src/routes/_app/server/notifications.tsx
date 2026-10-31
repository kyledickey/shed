import { createFileRoute } from "@tanstack/react-router";
import {
  notificationChannelsQuery,
  notificationDeliveriesQuery,
  notificationProvidersQuery,
  notificationRulesQuery,
} from "../../../api/notifications";
import { Notifications } from "../../../features/notifications/Notifications";

export const Route = createFileRoute("/_app/server/notifications")({
  loader: ({ context: { queryClient } }) =>
    Promise.all([
      queryClient.ensureQueryData(notificationProvidersQuery),
      queryClient.ensureQueryData(notificationChannelsQuery),
      queryClient.ensureQueryData(notificationRulesQuery),
      queryClient.ensureQueryData(notificationDeliveriesQuery),
    ]),
  component: Notifications,
});
