# Builds

## Dockerfile or Railpack

For an app built from a repository, shed clones the exact commit
(`git fetch --depth 1`) into a scratch directory, then chooses a build tool:

- A Dockerfile in the build context: `docker buildx build` with it.
- No Dockerfile: [Railpack](https://railpack.com) inspects the source, works
  out the language and build steps, and builds that plan.

`rootDir` is the build context and `dockerfilePath` is relative to it. Both
must stay inside the repository. Setting a Dockerfile path is a promise: a
missing file is an error, not a quiet fall back to Railpack.

The build log shows the choice: `==> Building with Dockerfile` or
`==> Building with Railpack`.

## Build secrets

Service variables reach the build as BuildKit secrets, not build arguments.
In a Dockerfile, read one with `RUN --mount=type=secret,id=KEY`, which exposes
it as the file `/run/secrets/KEY` for that instruction only. `ARG KEY` does not
receive service variables. Railpack reads variables while planning, except
names that control host tools (`PATH`, `HOME`, `TMPDIR`, and anything starting
with `LD_`, `DYLD_`, `XDG_`, `GIT_`, `DOCKER_`).

Build logs mask variable values, but a build that prints or bakes in a secret
it was given can still leak it.

## Builder limits

Builds use a dedicated BuildKit builder named `shed` with its own CPU and
memory limits (`build.cpus`, `build.memory_mb`; 0 is unlimited). Only one
build runs at a time across the whole instance, so a deployment can sit in
`building` with only its first heading in the log while it waits for the slot.
The deadline for a build is 30 minutes and includes that wait.

## Disk guard

Before the clone, a build fails if free space is below `build.min_free_mb`
(default 2048 MiB). During the build it is canceled if free space falls below
half of that. Both the data directory's filesystem and Docker's are checked.

## Images and ports

A built image is tagged `shed/<serviceID>:<deploymentID>`. shed keeps the five
newest per service plus the active one, which limits how far back a rollback
can reach. When a service's port is 0, shed saves the lowest `EXPOSE`d TCP
port after the build and injects it as `PORT`. A new repo app starts with
port 8080, so it does not auto-detect. Image apps and databases are not built;
shed pulls the image and records its local image ID so restarts and rollbacks
use that exact image.
