import { useSuspenseQuery } from "@tanstack/react-query";
import {
  notificationChannelsQuery,
  notificationDeliveriesQuery,
  notificationProvidersQuery,
  notificationRulesQuery,
} from "../../api/notifications";
import { Stack } from "../../components/Layout";
import { ChannelsCard } from "./ChannelsCard";
import { DeliveriesCard } from "./DeliveriesCard";
import { RulesCard } from "./RulesCard";

/** Notifications manages where shed sends notifications, what sends them, and what was sent. */
export function Notifications() {
  const { data: providers } = useSuspenseQuery(notificationProvidersQuery);
  const { data: channels } = useSuspenseQuery(notificationChannelsQuery);
  const { data: rules } = useSuspenseQuery(notificationRulesQuery);
  const { data: deliveries } = useSuspenseQuery(notificationDeliveriesQuery);
  return (
    <Stack gap={4}>
      <ChannelsCard channels={channels} providers={providers} />
      <RulesCard rules={rules} channels={channels} />
      <DeliveriesCard deliveries={deliveries} />
    </Stack>
  );
}
