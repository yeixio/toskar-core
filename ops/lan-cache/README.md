# LAN download cache

Toskar's CI runs on home runners: nine Linux runners on one computer, plus
a Mac, a Windows PC, a models runner and an Android runner. Each downloaded
the same Go modules, npm packages, Playwright browsers, Docker images and
models for itself. This stack keeps one copy of each on the home network,
so a file crosses the internet connection once.

| Port | Service | Caches | Used by |
| --- | --- | --- | --- |
| 8380 | nginx | Hugging Face model files (`/hf/`), Go modules (`/go/`), Playwright's browsers (`/playwright/`), Maven Central (`/maven/`), Google's Maven (`/google-maven/`) and Gradle (`/gradle/`) | `GOPROXY`, `PLAYWRIGHT_DOWNLOAD_HOST`, `TOSKAR_DOWNLOAD_MIRROR` |
| 4873 | Verdaccio | npm and pnpm packages | `npm_config_registry` |
| 5005 | registry | Docker Hub images | the Docker daemon's and BuildKit's registry mirror |

It sits beside `CI_SHARED_CACHE`, the folder a computer's runners share for
what they build, such as BuildKit's layer cache. This stack holds what they
download, and every computer on the network can use it.

## How CI uses it

The variable `CI_LAN_CACHE` is the address of the computer
running the stack. A job on a self-hosted runner runs
`.github/scripts/lan-cache.sh`, which checks that the cache answers and then
points the job's downloads at it. If the variable is unset, or the cache
doesn't answer within three seconds, the job downloads from the internet as
before and says so in a warning. Go modules and models also fall back to
their source by themselves if the cache fails partway through.

A file is kept until nobody has asked for it in 45 days (`CACHE_DAYS`), so
a model in weekly use stays however long ago it was first downloaded. When
the downloads cache reaches `MAX_SIZE` (300 GB unless the workflow is told
otherwise), the least recently used files go first. The files live in Docker
volumes on that computer, not in a folder of its own.

Toskar's daemon uses the cache for model downloads when
`TOSKAR_DOWNLOAD_MIRROR` is set. It asks the cache first and Hugging Face
second, and it downloads from Hugging Face again when the cache's copy
doesn't match the model's checksum.

What it trusts: Go modules are still checked against sum.golang.org, npm
packages against the lockfile's hashes, and catalog models with a checksum
against it. Playwright's browsers and models without a checksum are taken
as the cache serves them, so the cache should be reachable only on the home
network.

## Setting it up

Nobody needs to sign in to the Linux computer: a workflow starts the stack
from one of its runners, and the containers then run with Docker by
themselves, after reboots too.

1. In toskar-core's Actions, run **LAN download cache** with `start`. It
   builds and starts the three containers, checks that each answers, and
   prints the computer's address on the home network in the run summary.
2. Set the variable `CI_LAN_CACHE` to that address for every repository,
   in the organization's Settings, then Secrets and variables, then
   Actions, or:

   ```sh
   gh variable set CI_LAN_CACHE --org yeixio --visibility all --body 192.168.1.20
   ```

   The next run's "Use the LAN download cache" step says what came through
   the cache.

Optional: a `DOCKERHUB_USERNAME` variable and `DOCKERHUB_TOKEN` secret raise
Docker Hub's pull limit for the registry mirror.

To stop using it, delete the variable; jobs go back to the internet at once.
Running the workflow with `stop` stops the containers and keeps their files.

### What the workflow doesn't do

BuildKit jobs (toskar.ai) use the registry mirror through their own
configuration. A plain `docker build` or `docker pull` uses the Docker
daemon's settings, which only an administrator of that computer can change.
To send those through the mirror too, add this to `/etc/docker/daemon.json`
(merged with what is there) and restart Docker while no job runs:

```json
{
  "registry-mirrors": ["http://192.168.1.20:5005"],
  "insecure-registries": ["192.168.1.20:5005"]
}
```

On a Mac or Windows PC the same two settings go in Docker Desktop's
Settings, then Docker Engine.

## Looking after it

- Space used and the address: the workflow's run summary.
- Updates: run the workflow again with `start`; it pulls newer images.
- A response's `X-Lan-Cache` header says `HIT` when a file came from disk,
  such as `curl -sI http://192.168.1.20:8380/go/golang.org/x/text/@v/v0.43.0.mod`.
- Start again from empty: run the workflow with `clear`, then with `start`.
