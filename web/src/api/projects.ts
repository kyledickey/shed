import { queryOptions, useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "./client";
import { keys } from "./keys";
import type { Project, ProjectDetail } from "./types";

const anyDeploying = (projects: (Project | ProjectDetail)[] | undefined) =>
  projects?.some((p) => p.services.some((s) => s.status === "deploying")) ?? false;

export const projectsQuery = queryOptions({
  queryKey: keys.projects,
  queryFn: () => api.get<Project[]>("/projects"),
  refetchInterval: (q) => (anyDeploying(q.state.data) ? 3000 : 15_000),
});

export const projectQuery = (id: string) =>
  queryOptions({
    queryKey: keys.project(id),
    queryFn: () => api.get<ProjectDetail>(`/projects/${id}`),
    refetchInterval: (q) =>
      anyDeploying(q.state.data ? [q.state.data] : undefined) ? 3000 : 15_000,
  });

export function useCreateProject() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (name: string) => api.post<Project>("/projects", { name }),
    onSuccess: () => qc.invalidateQueries({ queryKey: keys.projects }),
  });
}

export function useRenameProject(id: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (name: string) => api.patch<Project>(`/projects/${id}`, { name }),
    onSuccess: () => qc.invalidateQueries({ queryKey: keys.projects }),
  });
}

export function useDeleteProject(id: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: () => api.delete(`/projects/${id}`),
    onSuccess: () => {
      qc.removeQueries({ queryKey: keys.project(id) });
      return qc.invalidateQueries({ queryKey: keys.projects, exact: true });
    },
  });
}
