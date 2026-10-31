---
name: shedlab
description: Start, update, and inspect throwaway shed instances in local VMs with the shedlab CLI. Use when testing a shed change on a real install, reproducing a bug on a running instance, needing an instance with real services or lots of mock data, or testing anything that needs root, Docker, or firewall changes without touching this machine.
---

# shedlab

`shedlab` runs real shed installs in local KVM VMs: Ubuntu, Docker, systemd, and
shed's own `deploy/install.sh`. Only the binary (this checkout) and the ports
(the lab's slot) differ from production. Use a lab instead of running anything
as root on this machine.

Run it from inside the repository. `go install ./cmd/shedlab` puts it on `PATH`;
`go run ./cmd/shedlab` also works.

## Quick start

```sh
shedlab doctor               # check KVM, QEMU, and tools
shedlab up firehose --no-auth   # start a lab; the first run also builds a base image
shedlab push                 # ship your changes (build + restart, seconds)
shedlab                      # list labs
shedlab down lab1            # delete it
```

## Commands

| Command | What it does |
| --- | --- |
| `shedlab` | List labs and whether they are running |
| `shedlab up [scenario]` | Start a new lab. No scenario opens a picker, which needs a terminal |
| `shedlab push [lab]` | Build this checkout and restart the lab on it. Rebuilds the dashboard only if `web/src` changed |
| `shedlab open [lab]` | Open the dashboard, signed in |
| `shedlab ssh [lab] [-- cmd]` | Open a shell or run a command. The user is `ubuntu` with passwordless sudo |
| `shedlab logs [lab]` | Follow shed's journal |
| `shedlab stop [lab]` / `start [lab]` | Free the lab's CPU and memory, or bring it back with its data |
| `shedlab save <name> [lab]` | Snapshot the disk, then restart the lab |
| `shedlab down [lab]` / `down --all` | Delete labs and their disks |
| `shedlab auth` | Save a GitHub App (and optionally a tunnel) for labs to reuse |
| `shedlab doctor` | Check this machine |

The lab argument is optional when only one lab fits. `up` flags: `--no-auth`,
`--release vX.Y.Z` (install that release; follow with `shedlab push` to test an
upgrade to this checkout), `--from <snapshot>`, `--no-open`.

## Scenarios

Scenarios are TOML files in `cmd/shedlab/scenarios/`, applied through shed's real
API.

| Scenario | Size | Contents |
| --- | --- | --- |
| `empty` | 4 vCPU, 8 GB | Nothing deployed |
| `real` | 4 vCPU, 8 GB | Gitea on Postgres, Redis, whoami, telemetry on |
| `firehose` | 8 vCPU, 16 GB | 20 generators: 14k logs/s, 6k spans/s, 4k metric points/s |

To add one, add a file. Service fields: `name`, `kind` (`postgres`, `mysql`,
`mongodb`, `redis`, or `app` by default), `image`, `repo`, `branch`, `start`,
`port`, `count`, `domain`, `vars`. Mock data generators use `generate` (`logs`,
`otel-logs`, `traces`, `metrics`) and `rate` (records per second per copy) instead
of `image`. `repo` needs saved credentials.

Build a large dataset once, then reuse it:

```sh
shedlab up firehose --no-auth
# wait for as much data as you want
shedlab save big-data
shedlab up --from big-data     # starts in seconds, data included
```

## Auth

| Mode | When | Sign-in | GitHub features |
| --- | --- | --- | --- |
| None | `--no-auth`, or nothing saved | `shedlab open` signs you in as `lab` | Repos and webhooks unavailable |
| GitHub App | After `shedlab auth` | `shedlab open` signs you in; `lab1.shed.land` labs use GitHub sign-in | Repos work; webhooks need the tunnel |

`shedlab auth` creates one GitHub App with one click in the browser, and saves it
to `~/.config/shedlab/credentials.json`. Every later `up` imports it, so nobody
types an ID or secret. Answering yes to the `shed.land` question also creates a
Cloudflare tunnel, with DNS for `lab1.shed.land` to `lab5.shed.land`.

The App has one webhook URL, and each `up` on `shed.land` points it at the new
lab. Only the newest such lab receives push events.

Signing in with no GitHub involves no backdoor in shed: `shedlab` inserts a
session row into the guest's `shed.db` over SSH, and hands the browser the cookie
from a one-shot local server on the lab's hostname.

## Addresses

Each lab is a slot from 1 to 5, with fixed ports. For `lab2`:

| What | Address |
| --- | --- |
| Dashboard | `http://lab2.localhost:18200` (or `https://lab2.shed.land` with the tunnel) |
| Apps | `https://<service>-<project>.lab2.localhost:18243` |
| SSH | `127.0.0.1:18222`, through `shedlab ssh` |

## Driving a lab from a script

Without a terminal, commands print plain lines and `up` doesn't open a browser.
Read the session from the lab's state file to call the API:

```sh
shedlab up firehose --no-auth
session=$(jq -r .session ~/.local/share/shedlab/labs/lab1/lab.json)
curl --cookie "shed_session=$session" http://127.0.0.1:18100/api/projects
```

Telemetry stores listen inside the guest:

```sh
shedlab ssh lab1 -- "curl -s -G http://127.0.0.1:9428/select/logsql/stats_query --data-urlencode 'query=* | stats count() n'"
```

## Limits

- Apps use Caddy's internal CA, so browsers warn about the certificate. `curl`
  needs `-k`.
- Links and redirects that shed generates for apps leave out the port, so add
  `:18243` by hand. Dashboard links to app domains hit the same problem.
- A lab can't test Let's Encrypt, ports 80 and 443, or real client IPs. Those need
  a real server.
- `up` failing leaves the lab for inspection. Read `console.log` in
  `~/.local/share/shedlab/labs/<lab>/`, then `shedlab down <lab>`. Ctrl-C removes it.
- To rebuild the base image, for example after changing `deploy/install.sh`, delete
  `~/.local/share/shedlab/images/base.qcow2`. Remove labs first, since they
  depend on it.
