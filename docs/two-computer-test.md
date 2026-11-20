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

Listen on computer A's network address (`ip -4 addr` shows it) and open the port in its
firewall (Omarchy turns ufw on):

```sh
PIM_FAKE_NOW="2026-11-09 10:00" PIM_LISTEN=<computer-A-address> bin/pim-dev up
sudo ufw allow from 192.168.0.0/16 to any port 5222 proto tcp
```

Anyone who can reach the port can sign in with the dev password, so do this on a network
you trust, not school or public Wi-Fi.

Copy the server's certificate to computer B, for example to `~/pim.test.crt` (with `scp`
or a USB stick):

```sh
scp <user>@<computer-A-address>:Projects/pim-server/.dev/certs/pim.test.crt ~/pim.test.crt
```

## 3. Sign in, one person on each computer

Pick two fictional people from `pim-server/fixtures/school/school.toml`; the password for
every development account is `pim-dev`. In pim choose **Add an account → XMPP**:

| Field | Computer A | Computer B |
| --- | --- | --- |
| Address | the first person, `…@pim.test` | the second person, `…@pim.test` |
| Password | `pim-dev` | `pim-dev` |
| Server | `127.0.0.1` | computer A's address |
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

## If computer B can't connect

The form says it couldn't connect to the server. Check, on computer B:
`nc -zv <computer-A-address> 5222`. If that fails, the server isn't listening on that
address (`ss -ltn | grep 5222` on computer A) or the firewall on A is blocking the port.
