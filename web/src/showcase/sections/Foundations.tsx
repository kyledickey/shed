import { crayons } from "../../components/tone";
import { Grid, Section, Specimen } from "../Section";
import s from "../showcase.module.css";

const neutrals = [
  ["bg", "--bg"],
  ["shell", "--shell"],
  ["panel", "--panel"],
  ["raised", "--raised"],
  ["sunken", "--sunken"],
  ["hover", "--hover"],
  ["line", "--line"],
  ["ink-4", "--ink-4"],
  ["ink-3", "--ink-3"],
  ["ink-2", "--ink-2"],
  ["ink", "--ink"],
] as const;

export function Foundations() {
  return (
    <>
      <div className={s.hero}>
        <h1 className={s.heroTitle}>Soft little crayons for serious servers.</h1>
        <p className={s.heroText}>
          The shed kit: warm paper and soft graphite, eight crayon hues, generous radii, and quiet
          motion. Use the tweaks button (bottom right) to try accents, softness and fonts.
        </p>
      </div>

      <Section
        title="Crayons"
        description="Each hue has a solid, a soft tint, an ink for text, and a line."
      >
        <Grid min={150}>
          {crayons.map((c) => (
            <div key={c} className={s.swatch} data-tone={c}>
              <div className={s.swatchTop}>
                <span className={s.swatchInk}>Aa</span>
                <span className={s.swatchDot} />
              </div>
              <div className={s.swatchName}>{c}</div>
            </div>
          ))}
        </Grid>
      </Section>

      <Section title="Neutrals" description="Five surfaces stack from page canvas up to popovers.">
        <div className={s.neutrals}>
          {neutrals.map(([name, v]) => (
            <div
              key={name}
              style={{
                background: `var(${v})`,
                color: name.startsWith("ink") ? "var(--panel)" : undefined,
              }}
            >
              {name}
            </div>
          ))}
        </div>
      </Section>

      <Section
        title="Type"
        description="Open Runde for everything you read, Commit Mono for everything a machine wrote."
      >
        <Specimen label="Scale">
          <div>
            <div className={s.typeRow}>
              <span className={s.typeMeta}>display / 40</span>
              <span
                style={{
                  fontFamily: "var(--font-sans)",
                  fontSize: 40,
                  fontWeight: 700,
                  letterSpacing: "-0.035em",
                  lineHeight: 1.1,
                }}
              >
                Ship it from the shed
              </span>
            </div>
            <div className={s.typeRow}>
              <span className={s.typeMeta}>display / 28</span>
              <span
                style={{
                  fontFamily: "var(--font-sans)",
                  fontSize: 28,
                  fontWeight: 650,
                  letterSpacing: "-0.025em",
                }}
              >
                acme / api
              </span>
            </div>
            <div className={s.typeRow}>
              <span className={s.typeMeta}>title / 16</span>
              <span style={{ fontSize: 16, fontWeight: 600 }}>Deployments</span>
            </div>
            <div className={s.typeRow}>
              <span className={s.typeMeta}>body / 14</span>
              <span>Pushes to main deploy automatically once CI passes.</span>
            </div>
            <div className={s.typeRow}>
              <span className={s.typeMeta}>small / 12</span>
              <span style={{ fontSize: 12, color: "var(--ink-3)" }}>
                Last deployed 4 minutes ago by kyle
              </span>
            </div>
            <div className={s.typeRow}>
              <span className={s.typeMeta}>mono / 12.5</span>
              <code style={{ fontSize: 12.5 }}>
                DATABASE_URL=postgres://shed@postgres.internal:5432/app
              </code>
            </div>
          </div>
        </Specimen>
      </Section>

      <Grid min={360}>
        <Specimen label="Radii (scaled by --soft)">
          <div className={s.radii}>
            {["xs", "sm", "md", "lg", "xl"].map((r) => (
              <div key={r} style={{ borderRadius: `var(--r-${r})` }}>
                r-{r}
              </div>
            ))}
          </div>
        </Specimen>
        <Specimen label="Elevation">
          <div className={s.shadows}>
            {["sm", "md", "lg"].map((sh) => (
              <div key={sh} style={{ boxShadow: `var(--shadow-${sh}), 0 0 0 1px var(--line)` }}>
                shadow-{sh}
              </div>
            ))}
          </div>
        </Specimen>
      </Grid>
    </>
  );
}
