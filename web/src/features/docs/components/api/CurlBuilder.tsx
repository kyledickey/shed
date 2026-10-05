import { useState } from "react";
import { Input, Select } from "../../../../components/Form";
import { curlCommand, endpointGroups } from "../../lib/apiEndpoints";
import { CodeBlock, Demo } from "../../kit";
import styles from "./api.module.css";

const endpoints = endpointGroups.flatMap((g) => g.endpoints);

/** CurlBuilder writes the curl command for an endpoint and an origin the reader types. */
export function CurlBuilder() {
  const [index, setIndex] = useState("0");
  const [origin, setOrigin] = useState("https://shed.example.com");
  const e = endpoints[Number(index)] ?? endpoints[0]!;
  return (
    <Demo title="curl command builder">
      <div className={styles.stack}>
        <div className={styles.controls}>
          <label className={styles.control}>
            Endpoint
            <Select
              mono
              value={index}
              onChange={setIndex}
              options={endpoints.map((x, i) => ({
                value: String(i),
                label: `${x.method} ${x.path}`,
              }))}
            />
          </label>
          <label className={styles.control}>
            Your server URL
            <Input value={origin} onChange={(ev) => setOrigin(ev.target.value)} />
          </label>
        </div>
        <CodeBlock lang="sh">{curlCommand(origin.replace(/\/+$/, ""), e)}</CodeBlock>
      </div>
    </Demo>
  );
}
