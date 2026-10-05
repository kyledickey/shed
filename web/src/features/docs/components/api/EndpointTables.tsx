import { Fragment } from "react";
import { Badge } from "../../../../components/Badge";
import { endpointGroups } from "../../lib/apiEndpoints";
import { DocTable } from "../../kit";
import styles from "./api.module.css";

/** EndpointTables renders the API reference, one table per endpoint group. */
export function EndpointTables() {
  return endpointGroups.map((g) => (
    <div key={g.title}>
      <h3>{g.title}</h3>
      <DocTable
        head={["Method", "Path", "Body", "Response", "Notes"]}
        rows={g.endpoints.map((e) => [
          <Badge key="m" size="sm" mono>
            {e.method}
          </Badge>,
          <span key="p" className={styles.mono}>
            {breakable(e.path)}
          </span>,
          e.body ? (
            <span key="b" className={styles.mono}>
              {e.body}
            </span>
          ) : (
            ""
          ),
          <span key="r" className={styles.mono}>
            {e.response}
          </span>,
          e.notes ?? "",
        ])}
      />
    </div>
  ));
}

/** breakable lets a path wrap only after its slashes and query marks. */
function breakable(path: string) {
  return path.split(/(?<=[/?&])/).map((part, i) => (
    <Fragment key={i}>
      {i > 0 && <wbr />}
      {part}
    </Fragment>
  ));
}
