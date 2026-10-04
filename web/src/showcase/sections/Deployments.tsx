import { Ellipsis, RotateCcw, ScrollText, Square, Undo2 } from "lucide-react";
import { Button } from "../../components/Button";
import { LayerCard } from "../../components/Card";
import { DeployRow, DeploySteps } from "../../components/deploy/Deploy";
import { BuildLogView } from "../../components/logs/BuildLogView";
import { Menu, MenuItem, MenuSeparator } from "../../components/Overlay";
import { deployments, useFakeBuild } from "../sample";
import { Grid, Section, Specimen } from "../Section";
import s from "../showcase.module.css";

export function Deployments() {
  const build = useFakeBuild();
  return (
    <>
      <Section title="Progress" description="A deployment walks queued → build → deploy → live.">
        <Grid min={300}>
          <Specimen label="Building">
            <DeploySteps status="building" />
          </Specimen>
          <Specimen label="Failed while deploying">
            <DeploySteps status="failed" failedAt="deploying" />
          </Specimen>
          <Specimen label="Live">
            <DeploySteps status="active" />
          </Specimen>
        </Grid>
      </Section>

      <Section title="History">
        <LayerCard title="Deployments" meta="acme/api · main">
          <div className={s.list} style={{ padding: 4 }}>
            {deployments.map((d) => (
              <DeployRow
                key={d.id}
                deployment={d}
                branch="main"
                current={d.status === "active"}
                actions={
                  <Menu
                    trigger={
                      <Button variant="ghost" size="sm" icon aria-label="Deployment actions">
                        <Ellipsis size={14} />
                      </Button>
                    }
                  >
                    <MenuItem icon={<ScrollText size={14} />}>View logs</MenuItem>
                    <MenuItem icon={<RotateCcw size={14} />}>Redeploy</MenuItem>
                    {d.status !== "active" && (
                      <MenuItem icon={<Undo2 size={14} />}>Roll back to this</MenuItem>
                    )}
                    {d.status === "building" && (
                      <>
                        <MenuSeparator />
                        <MenuItem icon={<Square size={14} />} danger>
                          Cancel
                        </MenuItem>
                      </>
                    )}
                  </Menu>
                }
              />
            ))}
          </div>
        </LayerCard>
      </Section>

      <Section
        title="Build logs"
        description="Steps collapse, BuildKit ids get crayons, cache hits and timings are pulled out."
        actions={
          <Button size="sm" onClick={build.restart}>
            <RotateCcw size={13} /> Replay
          </Button>
        }
      >
        <BuildLogView lines={build.lines} state={build.state} height={460} />
      </Section>
    </>
  );
}
