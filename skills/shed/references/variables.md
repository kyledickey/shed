# Variables

A variable is a key and a string. Keys match `[A-Za-z_][A-Za-z0-9_]*`.
You can read key **names** with `list_variables`. You can never read values:
not through the tools, and logs mask them. Ask the user to check a value in the
dashboard (Service, Variables tab) instead of asking them to paste it.

## References

A value can point at another variable. shed expands references when a
deployment starts and writes the result into the container's environment.

| Written | Expands to |
| ------- | ---------- |
| `${{ KEY }}` | Variable `KEY` of the same service, stored or injected. |
| `${{ db.KEY }}` | Variable `KEY` of the service named `db` in the same project. Split at the first dot, so `db.a.b` is service `db`, key `a.b`. |
| `${{KEY}}` | The same. Whitespace inside the braces is optional. |
| `${{ NOPE }}` | An empty string. A missing variable or service is not an error. |
| `${A}`, `$HOME`, `${{ a b }}`, `${{}}` | Left exactly as written. |

A reference is expanded in the scope of the service that owns the value. If
`web` uses `${{ postgres.DATABASE_URL }}`, the `${{ POSTGRES_USER }}` inside
that value is postgres's, not web's.

Because a missing reference becomes an empty string silently, a common
failure is a typo in the service or key name: the app starts with an empty
`DATABASE_URL`. `list_variables` on the referenced service confirms the name
exists.

```env
DATABASE_URL=${{ postgres.DATABASE_URL }}
REDIS_URL=${{ redis.REDIS_URL }}
PUBLIC_URL=https://${{ SHED_PUBLIC_DOMAIN }}
```

Editing a variable does not change running containers. Deploy again to apply.
Saving only checks key names, so a bad reference shows up as a failed
deployment, not a save error.

## Injected variables

shed adds these to every service. A stored variable with the same name wins.

| Variable | Value | Present when |
| -------- | ----- | ------------ |
| `PORT` | the service's port | an app with a port above 0 |
| `SHED_PROJECT_NAME` | the project's name | always |
| `SHED_SERVICE_NAME` | the service's name | always |
| `SHED_PRIVATE_DOMAIN` | the service's name, its private host | always |
| `SHED_PUBLIC_DOMAIN` | the host of the first domain | the service has a domain |
| `SHED_GIT_COMMIT_SHA` | the commit being deployed | only for the service being deployed |
| `SHED_GIT_BRANCH` | the service's branch | the service has a branch |

`${{ web.SHED_GIT_COMMIT_SHA }}` used from another service expands to nothing.

## Database variables

Creating a database writes its variables. Passwords are 24 random
alphanumeric characters.

| Kind | Variables created |
| ---- | ----------------- |
| `postgres` | `POSTGRES_USER`, `POSTGRES_PASSWORD`, `POSTGRES_DB`, `DATABASE_URL` |
| `mysql` | `MYSQL_ROOT_PASSWORD`, `MYSQL_DATABASE`, `MYSQL_URL`, `DATABASE_URL` |
| `mongo` | `MONGO_INITDB_ROOT_USERNAME`, `MONGO_INITDB_ROOT_PASSWORD`, `MONGO_URL` |
| `redis` | `REDIS_PASSWORD`, `REDIS_URL` |

The connection strings are themselves references, for example
`DATABASE_URL=postgresql://${{POSTGRES_USER}}:${{POSTGRES_PASSWORD}}@${{SHED_PRIVATE_DOMAIN}}:5432/${{POSTGRES_DB}}`.
Postgres, MySQL, and MongoDB read their password variables only when the data
directory is first initialized, so changing them later does not change the
password inside an existing database. Redis picks up a changed
`REDIS_PASSWORD` on the next deploy.

## Resolution errors

Resolution walks the service's variables depth first. The first error fails
the deployment with `deploy: resolve variables: ...` in the build log.

- A cycle: `vars: reference cycle: web.A -> web.B -> web.A`.
- Depth over 64 levels.
- A stored value over 64 KiB, an expanded value over 64 KiB, or all resolved
  values together over 1 MiB.

Only the variables of the service being deployed are walked, so another
service's broken value matters only if something being deployed references it.

## Masking in logs

The resolved values of stored variables are masked as `***` in build logs and
runtime logs. Values under 8 characters are not masked (so `3000` or `true`
stay visible), injected values like `PORT` are not masked, and only the
literal string is masked: a base64 or URL-encoded copy is not.
