import { queryOptions } from "@tanstack/react-query";
import { api } from "./client";
import { keys } from "./keys";
import type { Repo } from "./types";

export const reposQuery = queryOptions({
  queryKey: keys.repos,
  queryFn: () => api.get<Repo[]>("/github/repos"),
  staleTime: 60_000,
});

export const branchesQuery = (repo: string) =>
  queryOptions({
    queryKey: keys.branches(repo),
    queryFn: () => api.get<string[]>(`/github/repos/${repo}/branches`),
    staleTime: 60_000,
    enabled: repo.includes("/"),
  });
