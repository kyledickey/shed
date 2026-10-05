import type { ComponentType } from "react";
import type { DocSlug } from "../registry";
import { OverviewDoc } from "./overview";
import { ConceptsDoc } from "./concepts";
import { DeploymentsDoc } from "./deployments";
import { BuildsDoc } from "./builds";
import { GithubDoc } from "./github";
import { NetworkingDoc } from "./networking";
import { VariablesDoc } from "./variables";
import { ResourcesDoc } from "./resources";
import { MetricsDoc } from "./metrics";
import { LogsDoc } from "./logs";
import { BackupsDoc } from "./backups";
import { RestoresDoc } from "./restores";
import { ConfigurationDoc } from "./configuration";
import { SecurityDoc } from "./security";
import { ApiDoc } from "./api";
import { CodebaseDoc } from "./codebase";
import { InternalsDoc } from "./internals";
import { LocalDevelopmentDoc } from "./local-development";
import { DashboardDoc } from "./dashboard";

/** docPageComponents renders each docs page. */
export const docPageComponents: Record<DocSlug, ComponentType> = {
  overview: OverviewDoc,
  concepts: ConceptsDoc,
  deployments: DeploymentsDoc,
  builds: BuildsDoc,
  github: GithubDoc,
  networking: NetworkingDoc,
  variables: VariablesDoc,
  resources: ResourcesDoc,
  metrics: MetricsDoc,
  logs: LogsDoc,
  backups: BackupsDoc,
  restores: RestoresDoc,
  configuration: ConfigurationDoc,
  security: SecurityDoc,
  api: ApiDoc,
  codebase: CodebaseDoc,
  internals: InternalsDoc,
  "local-development": LocalDevelopmentDoc,
  dashboard: DashboardDoc,
};
