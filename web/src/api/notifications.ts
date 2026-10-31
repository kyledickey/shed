import { queryOptions, useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "./client";
import { keys } from "./keys";
import type {
  NotificationChannel,
  NotificationChannelInput,
  NotificationChannelPatch,
  NotificationDelivery,
  NotificationRule,
  NotificationRulePatch,
} from "./types";

export const notificationProvidersQuery = queryOptions({
  queryKey: keys.notificationProviders,
  queryFn: () => api.get<string[]>("/notifications/providers"),
});

export const notificationChannelsQuery = queryOptions({
  queryKey: keys.notificationChannels,
  queryFn: () => api.get<NotificationChannel[]>("/notifications/channels"),
});

export const notificationRulesQuery = queryOptions({
  queryKey: keys.notificationRules,
  queryFn: () => api.get<NotificationRule[]>("/notifications/rules"),
});

/** Deliveries change as the outbox retries, so they refetch while the page is open. */
export const notificationDeliveriesQuery = queryOptions({
  queryKey: keys.notificationDeliveries,
  queryFn: () => api.get<NotificationDelivery[]>("/notifications/deliveries"),
  refetchInterval: 30_000,
});

export function useCreateNotificationChannel() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (input: NotificationChannelInput) =>
      api.post<NotificationChannel>("/notifications/channels", input),
    onSuccess: () => qc.invalidateQueries({ queryKey: keys.notificationChannels }),
  });
}

export function useUpdateNotificationChannel() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ id, patch }: { id: string; patch: NotificationChannelPatch }) =>
      api.patch<NotificationChannel>(`/notifications/channels/${id}`, patch),
    onSuccess: () => qc.invalidateQueries({ queryKey: keys.notificationChannels }),
  });
}

/** useDeleteNotificationChannel also drops the channel's deliveries. */
export function useDeleteNotificationChannel() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (id: string) => api.delete(`/notifications/channels/${id}`),
    onSuccess: () =>
      qc.invalidateQueries({ predicate: ({ queryKey }) => queryKey[0] === "notifications" }),
  });
}

/** useTestNotificationChannel sends a test message now; a failure carries the provider's reason. */
export function useTestNotificationChannel() {
  return useMutation({
    mutationFn: (id: string) => api.post(`/notifications/channels/${id}/test`),
  });
}

/** useSaveNotificationRules patches each rule in turn and refetches the list, even after a failure. */
export function useSaveNotificationRules() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: async (patches: { id: string; patch: NotificationRulePatch }[]) => {
      for (const { id, patch } of patches) {
        await api.patch<NotificationRule>(`/notifications/rules/${id}`, patch);
      }
    },
    onSettled: () => qc.invalidateQueries({ queryKey: keys.notificationRules }),
  });
}
