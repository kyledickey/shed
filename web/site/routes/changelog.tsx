import { createFileRoute } from "@tanstack/react-router";
import { Changelog } from "../Changelog";

export const Route = createFileRoute("/changelog")({
  component: Changelog,
});
