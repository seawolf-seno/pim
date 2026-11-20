# Testing pim between two computers

How to see two people message each other through the school's own chat server, with
two Omarchy computers on the same network. Everything here uses the pim-server
development server and its fictional school; no real student's name or account.

Computer A runs the server and one copy of pim. Computer B runs a second copy of pim.

## 1. Each computer: Go and the plugin

```sh
mise use -g go@1.26.10        # or: sudo pacman -S go
git clone https://github.com/seawolf-seno/pim && cd pim
git switch xmpp-sign-in       # until it is merged
make install-local            # builds the helper from this checkout and installs the plugin
```

## 2. Computer A: start the server

```sh
cd ~/Projects/pim-server
PIM_FAKE_NOW="2026-11-09 10:00" bin/pim-dev up   # Monday 10:00, so class time applies
```

The development server listens on `127.0.0.1` only, so computer B can't reach it yet.
pim-server needs a change for that (asked of pim-server, below). Until then, forward the
port from computer B over SSH, if computer A runs `sshd`:

```sh
# on computer B; leave it running
ssh -N -L 5222:127.0.0.1:5222 <user>@<computer-A-address>
```

Copy the server's certificate to computer B, for example to `~/pim.test.crt`:

```sh
scp <user>@<computer-A-address>:Projects/pim-server/.dev/certs/pim.test.crt ~/pim.test.crt
```

## 3. Sign in, one person on each computer

Pick two fictional people from `pim-server/fixtures/school/school.toml`; the password for
every development account is `pim-dev`. In pim choose **Add an account → XMPP**:

| Field | Computer A | Computer B (with the SSH forward) |
| --- | --- | --- |
| Address | the first person, `…@pim.test` | the second person, `…@pim.test` |
| Password | `pim-dev` | `pim-dev` |
| Server | `127.0.0.1` | `127.0.0.1` |
| Certificate file | `/home/<you>/Projects/pim-server/.dev/certs/pim.test.crt` | `/home/<you>/pim.test.crt` |

The certificate is made for the name `pim.test`, which pim checks no matter which address
it connects to, so `127.0.0.1` or a LAN address both work.

## 4. What to try

- **Ctrl+B** shows the buddy list: each computer should see the other person online.
- Send a message from A to B and back. A one-to-one message shows delivered once the other
  side receives it.
- Class rooms appear as group chats. With the clock pinned to Monday 10:00 a student's post
  in class is refused while class chat is closed (it shows as failed).

## Not built yet

Text only (no attachments), no read markers, and no command in pim to set your own away
message.

## Asked of pim-server

`server/dev/prosody.cfg.lua.in` sets `interfaces = { "127.0.0.1" }`. An option such as
`PIM_LISTEN=0.0.0.0 bin/pim-dev up` (or the computer's LAN address) would let computer B
connect directly with **Server** set to computer A's address, with no SSH forward.
