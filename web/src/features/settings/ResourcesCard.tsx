import type { Service } from "../../api/types";
import { Field, Input } from "../../components/Form";
import { SettingsCard, useSectionForm } from "./SectionForm";
import styles from "./Settings.module.css";

const MiB = 1 << 20;

/** ResourcesCard edits the container's CPU and memory limits. */
export function ResourcesCard({ service }: { service: Service }) {
  const form = useSectionForm(service, (s) => ({
    cpuLimit: s.cpuLimit,
    memoryLimit: s.memoryLimit,
  }));
  const { draft, set } = form;
  return (
    <SettingsCard title="Resources" meta="Limits for this service's container." form={form}>
      <div className={styles.twoCol}>
        <Field label="CPU" hint="Cores the container may use. 0 is unlimited.">
          {(id) => (
            <Input
              id={id}
              type="number"
              min={0}
              step={0.25}
              mono
              suffix="vCPU"
              value={draft.cpuLimit}
              onChange={(e) => set("cpuLimit", Number(e.target.value) || 0)}
            />
          )}
        </Field>
        <Field label="Memory" hint="Processes that exceed it are killed. 0 is unlimited.">
          {(id) => (
            <Input
              id={id}
              type="number"
              min={0}
              step={64}
              mono
              suffix="MiB"
              value={draft.memoryLimit / MiB}
              onChange={(e) => set("memoryLimit", Math.round((Number(e.target.value) || 0) * MiB))}
            />
          )}
        </Field>
      </div>
    </SettingsCard>
  );
}
