import { Diagram, Edge, Zone } from "../../diagram";

const lanes = { browser: 85, shed: 400, github: 715 } as const;

const steps: { from: keyof typeof lanes; to: keyof typeof lanes; label: string }[] = [
  { from: "browser", to: "shed", label: "1 · GET /api/setup/github?token=…" },
  { from: "shed", to: "browser", label: "2 · auto-submit form carrying the manifest" },
  { from: "browser", to: "github", label: "3 · POST manifest to github.com/settings/apps/new" },
  { from: "github", to: "browser", label: "4 · redirect to /api/setup/github/callback?code=…" },
  { from: "browser", to: "shed", label: "5 · callback with code and state" },
  { from: "shed", to: "github", label: "6 · POST /app-manifests/{code}/conversions" },
  { from: "github", to: "shed", label: "7 · App ID, slug, secrets, private key" },
  { from: "shed", to: "browser", label: "8 · redirect to the App's install page" },
];

/** SetupSequence is the manifest flow that creates the GitHub App. */
export function SetupSequence() {
  return (
    <Diagram
      width={800}
      height={400}
      label="Sequence of the GitHub App manifest flow between your browser, shed, and GitHub"
    >
      <Zone x={10} y={10} w={150} h={380} label="Your browser" />
      <Zone x={325} y={10} w={150} h={380} label="shed" tone="accent" />
      <Zone x={640} y={10} w={150} h={380} label="GitHub" />
      {steps.map((s, i) => {
        const y = 62 + i * 40;
        return (
          <Edge
            key={s.label}
            points={[
              [lanes[s.from], y],
              [lanes[s.to], y],
            ]}
            label={s.label}
            tone={s.from === "shed" || s.to === "shed" ? "accent" : "neutral"}
          />
        );
      })}
    </Diagram>
  );
}
