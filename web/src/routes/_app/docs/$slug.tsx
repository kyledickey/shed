import { createFileRoute, notFound } from "@tanstack/react-router";
import { docPageComponents } from "../../../features/docs/pages";
import { isDocSlug } from "../../../features/docs/registry";

export const Route = createFileRoute("/_app/docs/$slug")({
  beforeLoad: ({ params }) => {
    if (!isDocSlug(params.slug)) throw notFound();
  },
  component: DocRoute,
});

function DocRoute() {
  const { slug } = Route.useParams();
  if (!isDocSlug(slug)) return null;
  const Page = docPageComponents[slug];
  return <Page />;
}
