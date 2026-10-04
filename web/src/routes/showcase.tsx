import { createFileRoute } from "@tanstack/react-router";
import { Showcase, showcaseTabs, type ShowcaseTab } from "../showcase/Showcase";

type ShowcaseSearch = { tab?: ShowcaseTab };

export const Route = createFileRoute("/showcase")({
  validateSearch: (search): ShowcaseSearch =>
    showcaseTabs.includes(search.tab as ShowcaseTab) ? { tab: search.tab as ShowcaseTab } : {},
  component: ShowcasePage,
});

function ShowcasePage() {
  const { tab = "foundations" } = Route.useSearch();
  const navigate = Route.useNavigate();
  return <Showcase tab={tab} onTab={(next) => void navigate({ search: { tab: next } })} />;
}
