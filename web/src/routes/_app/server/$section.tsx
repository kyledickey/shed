import { createFileRoute, notFound } from "@tanstack/react-router";
import { isMetricsRange } from "../../../api/metrics";
import type { MetricsRange } from "../../../api/types";
import { ServerPage } from "../../../features/server/ServerPage";
import { findSection } from "../../../features/server/sections";

type SectionSearch = { range?: MetricsRange };

export const Route = createFileRoute("/_app/server/$section")({
  validateSearch: (search: Record<string, unknown>): SectionSearch =>
    isMetricsRange(search.range) && search.range !== "1h" ? { range: search.range } : {},
  loader: ({ context: { queryClient }, params }) => {
    const section = findSection(params.section);
    if (!section) throw notFound();
    return section.load?.(queryClient);
  },
  component: SectionPage,
});

function SectionPage() {
  const params = Route.useParams();
  const { range = "1h" } = Route.useSearch();
  const navigate = Route.useNavigate();
  const section = findSection(params.section);
  if (!section) return null;
  return (
    <ServerPage
      section={section}
      range={range}
      setRange={(next) =>
        void navigate({ search: next === "1h" ? {} : { range: next }, replace: true })
      }
    />
  );
}
