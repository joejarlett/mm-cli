# Installing Jarlett through `mm`

**Status: direction, not contract.** Written 2026-08-28. Nothing here is built.

The Jarlett suite — the `jarlett-ctl` supervisor, the JSPA, Rudie and Holly engines with
their UIs, and the `jarlett-tray` menu-bar app — is going to other people, starting with one
brother. `jarlett-ctl/specs/03-rolling-out-to-other-people.md` lists what blocks that. This
spec is the answer to three of the five, and it is mostly an argument that `mm` already
solved them.

## The decision that shapes everything else

**`mm` is the delivery mechanism. It is not the surface.** Jarlett installs as its own thing,
with its own tray, its own icon, its own update button.

The alternative — folding Jarlett into `mm-tray` — was considered and rejected on 2026-08-28.
Two trays, one install, one signed binary, one icon is genuinely tempting. It is still wrong:

- `mm-tray` is 81 lines. `jarlett-tray` is ~1000, nearly all of it engine semantics: group
  displacement, stalled versus stopped, `streamDead`, watchdog give-up. None of that means
  anything to the Meta-Me tray, and the Meta-Me tray's concerns mean nothing on stage.
- They are reached for in different states of mind. The menu you open mid-set to get the
  audio back should not also contain KB sync status.
- A music tool would inherit a personal-hub release cycle, and vice versa.

So: one delivery mechanism, two products. `mm install jarlett`, and Jarlett is a separate
application from that point on.

## What `mm` has already built that Jarlett needs

`scripts/install.sh` — served at `meta-me.uk/install.sh` — is the whole distribution problem
solved once already:

```
$HUB/dist/mm/latest                    → the version string
$HUB/dist/mm/$VERSION/mm-$platform     → the binary
$HUB/dist/mm/$VERSION/SHA256SUMS       → verified before anything is moved into place
```

Platform detection across `darwin|linux × arm64|amd64`, `shasum` or `sha256sum` whichever
exists, refusal to install unverified, atomic rename into place. `release.sh` cross-compiles
all five targets and signs the macOS ones.

Jarlett needs exactly that and has none of it. Its install today is `git clone` of five
private repos plus a C++ and two JavaScript toolchains.

**The generalisation is one path segment.**

```
$HUB/dist/<product>/latest
$HUB/dist/<product>/$VERSION/<file>
$HUB/dist/<product>/$VERSION/SHA256SUMS
```

`mm` grows a fetch-verify-place helper in Go that mirrors what the shell installer does, and
`install.sh` keeps working unchanged. Nothing about the hub side changes but the directory.

## `mm install jarlett`

```
mm install jarlett [--channel stable|beta] [--version vX.Y.Z]
mm update  jarlett
mm status  jarlett
```

What one install does, in order:

1. **Check the grant.** Jarlett is an app the hub knows about, and installing it needs the
   caller to have been granted it. This is the whole reason to route through `mm` rather than
   a public download: the brother tier is people you can grant, and revoke.
2. **Resolve the version** from the channel, exactly as `install.sh` resolves `latest`.
3. **Fetch and verify** the supervisor, the per-platform engine binaries, the prebuilt UIs
   and — on macOS — the tray. One `SHA256SUMS` covers all of them.
4. **Place them.** `~/.jarlett/` on macOS, because a LaunchAgent cannot read `~/Documents`
   (`jarlett-ctl/specs/02`). The same layout on Linux for symmetry.
5. **Write the registry** at the platform's `apps.json` path, with `dir` pointing at the
   placed artefacts and a `channel` instead of a `repo`.
6. **Install the service** — LaunchAgent or systemd `--user` — and start it.
7. **Say what to do next**, which is `mm login` if they have not, and nothing else.

Uninstall is the same list backwards, and should exist from the first version rather than
being the thing nobody wrote.

## What this fixes, and what it does not

| Rollout blocker | After this |
|---|---|
| 1. Nothing updates `jarlett-ctl` | **Fixed.** The supervisor stops being special — it is an artefact `mm update jarlett` replaces like any other. |
| 2. `main` is the release channel | **Fixed.** A channel resolves to a version; a bad release is one `--version` away from reverted. |
| 3. Source installs need a toolchain | **Fixed** for the UIs and, on Linux, the engines. |
| 4. Gatekeeper quarantines a downloaded `.app` | **Not fixed. Blocks the macOS tray specifically.** |
| 5. Identity asserts rather than proves | **Mostly.** `mm login` already exists; installing behind a grant means every machine has a real account. |

Blocker 1 is worth pausing on. It has no fix inside Jarlett — a supervisor cannot reliably
replace its own source and restart itself, which is why the tray can self-update and the
supervisor cannot. Moving it outside, into a tool that is already installed and already
updates itself, is not a workaround. It is where that job belongs.

### Blocker 4 is the one that has already bitten this repo

`jarlett-tray/scripts/build-tray.sh` carries the warning, learned here:

> a self-signed `.app` shipped in a ZIP gets quarantined by Gatekeeper and refuses to open,
> which is why that download was dropped in favour of an un-quarantined install

`release.sh` still builds `MetaMe-Tray-darwin-$ARCH.zip`. `install.sh` never offers it.
**Meta-me already tried shipping a tray this way and pulled it**, and Jarlett would hit the
identical wall on the first Mac that is not M4.

There is no engineering around this. Either the recipient builds the tray on their own Mac —
which works today and is fine for a brother — or the £79/yr Apple Developer account gets
bought and `sign-darwin.sh` gains a notarisation step. Everything else in this spec can ship
before that is decided; the macOS tray cannot.

Linux is unaffected and has no `.app` to distribute. jj-laptop runs the `jj.jarlett`
Quickshell panel instead, which is a config directory rather than a binary.

## The condition carried forward

From `jarlett-tray/specs/01`, and it is not negotiable:

> **The artefact must carry its commit, and the supervisor must compare it to the engine's.**

A downloaded UI and a locally built engine are two artefacts that can come from different
commits. Every recurring bug in this codebase is two things disagreeing quietly, and shipping
prebuilt without solving this returns it in a form that is *harder* to see, because one half
is no longer built from a checkout anyone can inspect.

Concretely: every artefact in `SHA256SUMS` has its source commit recorded beside it, `/status`
reports what it actually placed, and a mismatch is a state the front-ends already know how to
render — the same `state` and `detail` pair the supervisor added on 2026-08-28.

## What this is not

- **A public download page.** Not before blocker 4 is decided, because the honest version of
  that page today contains "right-click and choose Open".
- **A consumer product.** Fanning out through Meta-Me works for people who can be granted an
  app in Joe's hub. Strangers creating accounts in a personal life-OS to get a music app is
  the wrong front door, and the fix is a different channel later, not different code. Keep
  the artefact layout free of anything that assumes the hub, so a second channel is a base
  URL rather than a rewrite.
- **Auto-update.** A rig updates when someone decides to, not mid-set.
- **A replacement for the dev path.** `dir` plus `repo` plus a git checkout stays exactly as
  it is on the machines where the code is written. `jarlett-ctl/specs/01` called this "config,
  not redesign" and it still is: a machine has either a `repo` or a `channel`.
