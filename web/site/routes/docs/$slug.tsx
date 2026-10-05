import { createFileRoute, notFound } from "@tanstack/react-router";
import { loadDoc } from "../../../src/features/docs/content";
import { MdxPage } from "../../../src/features/docs/mdx";
import { docPage } from "../../../src/features/docs/registry";

export const Route = createFileRoute("/docs/$slug")({
  loader: async ({ params }) => {
    const meta = docPage(params.slug);
    const Content = meta && (await loadDoc(params.slug));
    if (!meta || !Content) throw notFound();
    return { meta, Content };
  },
  component: DocRoute,
});

function DocRoute() {
  const { meta, Content } = Route.useLoaderData();
  return <MdxPage meta={meta} Content={Content} />;
}
