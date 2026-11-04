import type { Service } from "../../api/types";
import { SettingRow } from "../../components/Settings";
import { HelpTip } from "../docs/HelpTip";
import { NumberInput } from "./fields";
import { SettingsForm, useSectionForm } from "./SectionForm";

const MiB = 1 << 20;

/** ResourcesSection edits the container's CPU and memory limits. */
export function ResourcesSection({ service }: { service: Service }) {
  const form = useSectionForm(service, (s) => ({
    cpuLimit: s.cpuLimit,
    memoryLimit: s.memoryLimit,
  }));
  const { draft, set } = form;
  return (
    <SettingsForm form={form}>
      <SettingRow
        label={
          <>
            <HelpTip topic="resourceLimits">CPU limit</HelpTip>
          </>
        }
        description="Cores the container may use. 0 is unlimited."
        control={
          <NumberInput
            aria-label="CPU limit"
            min={0}
            step={0.25}
            unit="vCPU"
            value={draft.cpuLimit}
            onChange={(e) => set("cpuLimit", Number(e.target.value) || 0)}
          />
        }
      />
      <SettingRow
        label="Memory limit"
        description="Processes that exceed it are killed. 0 is unlimited."
        control={
          <NumberInput
            aria-label="Memory limit"
            min={0}
            step={64}
            unit="MiB"
            value={draft.memoryLimit / MiB}
            onChange={(e) => set("memoryLimit", Math.round((Number(e.target.value) || 0) * MiB))}
          />
        }
      />
    </SettingsForm>
  );
}
