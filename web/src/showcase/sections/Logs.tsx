import { Badge } from "../../components/Badge";
import { RuntimeLogView } from "../../components/logs/RuntimeLogView";
import { GitHubIcon, ServiceIcon } from "../../components/Misc";
import { useFakeStream } from "../sample";
import { Section } from "../Section";

export function Logs() {
  const stream = useFakeStream(160);
  return (
    <Section
      title="Runtime logs"
      description="JSON, logfmt, and plain lines are parsed into levels, fields, and HTTP requests. Click a line to expand it; hit the expand icon to watch fullscreen."
    >
      <RuntimeLogView
        lines={stream.lines}
        state={stream.state}
        onClear={stream.clear}
        height={560}
        title={
          <>
            <ServiceIcon kind="app" icon={<GitHubIcon />} size={30} />
            api
            <Badge tone="neutral" mono size="sm">
              4f2a9c1
            </Badge>
          </>
        }
      />
    </Section>
  );
}
