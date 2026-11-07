# flipchat

Send messages over the air from a **Flipper Zero**, and read them on another computer
with a cheap **RTL-SDR** dongle.

```
      Computer A: the sender                           Computer B: the listener

  you type ──▶ Flipper Zero  ))) 433.92 MHz radio )))  RTL-SDR dongle ──▶ messages appear
```

Both computers run the same terminal app, `flipchat`, and each picks its role when it
starts. Here's a real session:

```
 flipchat  Sender · 433.920 MHz       ● Flipper R1kori7h
10:01:46  R1kori7h  Hi listener, this is the sender  sent
╭──────────────────────────────────────────────────╮
│› Type a message and press Enter                  │
╰──────────────────────────────────────────────────╯

 flipchat  Listener · 433.920 MHz     1 message   ● RTL-SDR listening
10:01:41  → R1kori7h joined chat.
10:01:46  R1kori7h  Hi listener, this is the sender
10:01:55  → R1kori7h left chat.
```

No custom firmware and no app on the Flipper: it uses the chat already built into the
Flipper's firmware. Tested with a Flipper Zero on official firmware 1.5.1 and an RTL-SDR
dongle with an R820T tuner.

---

## What you need

| | Sender | Listener |
|---|---|---|
| **Hardware** | a Flipper Zero and its USB cable | an RTL-SDR dongle (any RTL2832U, e.g. RTL-SDR Blog v3/v4) and its antenna |
| **Computer** | macOS or Linux | macOS or Linux |

You can also try both roles on one computer, with both devices plugged in.

> **Legal:** only transmit on frequencies allowed where you live. 433.92 MHz is
> licence-free in Europe (low power, short transmissions). In the US, use 315 MHz.
> The Flipper refuses frequencies that aren't allowed in its region.

## Install

flipchat comes in two pieces. You install both, but you only ever run the first one:

| | What it is |
|---|---|
| **`flipchat`** | **The app.** It's what you run. |
| **`flipchat-radio`** | **Its engine.** It talks to the Flipper and the dongle. flipchat runs it in the background. |

On **each computer**:

**1. Install uv,** the tool that installs the engine (skip this if you have it):

```bash
curl -LsSf https://astral.sh/uv/install.sh | sh       # or: brew install uv
```

**2. Install the engine:**

```bash
uv tool install git+https://github.com/alelouis/rtlsdr-flipper-chat
```

**3. Install the app:**

```bash
os=$(uname -s | tr A-Z a-z); arch=$(uname -m | sed 's/x86_64/amd64/; s/aarch64/arm64/')
curl -fsSL https://github.com/alelouis/rtlsdr-flipper-chat/releases/latest/download/flipchat_${os}_${arch}.tar.gz | tar -xz
mkdir -p ~/.local/bin && mv flipchat_${os}_${arch}/flipchat ~/.local/bin/
flipchat -version
```

If `flipchat` isn't found, open a new terminal (or run `uv tool update-shell` once):
`~/.local/bin` must be on your PATH.

**4. On the listener only, install the RTL-SDR drivers** and check the dongle:

```bash
brew install librtlsdr          # macOS
sudo apt install rtl-sdr        # Debian / Ubuntu

rtl_test -t                     # should print "Found 1 device(s)" and a tuner name; Ctrl+C to stop
```

With an R820T/R828D tuner, `No E4000 tuner found, aborting.` is normal. On Linux, if you
get `usb_claim_interface error`, run `sudo rmmod dvb_usb_rtl28xxu` (the TV driver took the
dongle).

To update later: `uv tool upgrade flipchat-radio`, then repeat step 3.

## Use it

Plug in your device, then run:

```bash
flipchat
```

It asks what's plugged into this computer:

```
  What's plugged into this computer?

  › 1  Sender · Flipper Zero
       Type messages; the Flipper transmits them.
       Flipper on USB, qFlipper closed.

    2  Listener · RTL-SDR dongle
       Read the messages sent by Flippers nearby.
       RTL-SDR dongle on USB, antenna attached.

  Frequency  433.920 MHz   tab to change · both sides must use the same one
```

Choose with **↑ ↓** (or **1** / **2**) and press **Enter**. Both computers must use the
same frequency: press **Tab** to change it first.

**As the sender:** quit qFlipper if it's open, because only one program can use the
Flipper's USB port. The header shows `● Flipper <its name>` once it's connected. Type a line
and press **Enter**. It shows `sending…`, then `sent` once the Flipper has transmitted it.

**As the listener:** the header shows `● RTL-SDR listening` and how many messages arrived.
Each message appears with the sender's name. You also see `→ name joined chat.` and
`left chat.` when a sender starts or stops.

Start with the two devices a few metres apart, then try further away.

| Key | |
|---|---|
| **Enter** | send the line (sender) |
| **↑ ↓ PgUp PgDn Home End** | scroll |
| **Ctrl+R** | reconnect the device after it dropped (e.g. you replugged it) |
| **Esc** | back to the role screen |
| **Ctrl+C**, or **q** as the listener | quit |

Options let you skip the role screen or adjust things:

```bash
flipchat -role sender                       # start as the sender
flipchat -role listener                     # start as the listener
flipchat -f 315                             # frequency in MHz (US: 315)
flipchat -role listener -g 20               # fixed dongle gain in dB (default: automatic)
flipchat -role sender -port /dev/ttyACM0    # if the Flipper isn't found automatically
flipchat -file capture.cu8                  # replay a recording (see "Recording" below)
```

Other Flippers running the same chat on the same frequency are part of the conversation
too: the listener hears them, and the sender sees their lines.

## Troubleshooting

| Problem | Fix |
|---------|-----|
| `can't find flipchat-radio` | Install the engine (step 2 above), then open a new terminal |
| `rtl_sdr not found` | Install the RTL-SDR drivers (step 4 above) |
| `usb_open error` | Another program (SDR++, GQRX, rtl_433...) is using the dongle: close it |
| `... is busy: quit qFlipper` | qFlipper holds the Flipper's USB port: quit it |
| `no Flipper found on USB` | Plug it in and unlock it, or use `-port` |
| `the Flipper doesn't answer on USB` | Unplug the Flipper, plug it back in, press Ctrl+R |
| `could not start subghz chat` | Close the app open on the Flipper; check the frequency is allowed in your region |
| Nothing arrives on the listener | Same frequency on both sides? Antenna on the dongle? Move closer. If the devices are very close, try `-g 20`; if far, `-g 40` |
| macOS says `flipchat` can't be opened | It was downloaded with a browser: run `xattr -d com.apple.quarantine flipchat` once |

---

## How it works

### What the Flipper sends

The Flipper's firmware has a chat for talking Flipper to Flipper, available from its USB
command line: `subghz chat <frequency> <radio>`. As the sender, flipchat starts it and
types your lines into it. Its radio settings come from the firmware source
(`lib/subghz/devices/cc1101_configs.c`, preset `FuriHalSubGhzPresetGFSK9_99KbAsync`):

| | |
|---|---|
| **Modulation** | GFSK: the frequency shifts up for `1`, down for `0`, by ±19 kHz |
| **Speed** | 9.99 kbit/s, so a full packet is on air for ~55 ms |
| **Packet** | preamble `AA AA AA AA` · sync word `46 4C` · length · 1–60 bytes of text · CRC-16 |
| **CRC** | polynomial 0x8005, start value 0xFFFF (the radio chip's own, a TI CC1101) |
| **Text** | `ESC[0;33m<flipper name>ESC[0m: <your line>\r\n` (the name is in colour) |

A line longer than 60 bytes is split over several packets about 10 ms apart. Joining
and leaving the chat send a line too (`<name> joined chat.`).

### How the listener decodes it

The dongle records the radio at 1.024 million samples per second, tuned 250 kHz below the
chat frequency to avoid the noise spike RTL-SDR dongles have at their centre. Then:

1. **Move the signal to 0 Hz and filter it,** keeping the ~50 kHz around the chat.
2. **Find bursts:** every 1 ms, compare the power to the noise floor (+10 dB). The noise
   floor is learned only between bursts, so a busy chat never raises it.
3. **FM-demodulate** each burst: the frequency over time gives the bits. The receiver
   measures the actual carrier itself, so a dongle a few kHz off still works.
4. **Find the bit timing** by sliding the known pattern `AA AA 46 4C` along the signal.
5. **Read the packet** (length, text, CRC) and drop it if the CRC is wrong.
6. **Rebuild the lines:** join packets up to the newline and strip the colour codes.

In simulation it decodes down to 0 dB SNR, about 20× faster than real time. A generic
decoder like rtl_433 caught only 1 packet in 3 of this chat, hence a dedicated receiver.

### How the two pieces fit

`flipchat` (written in Go, with [Bubble Tea](https://github.com/charmbracelet/bubbletea))
draws the screens. Depending on the role, it runs `flipchat-radio send` or
`flipchat-radio listen` (written in Python) with `--json`, and reads one event per line:

```
listen: {"event": "listening", ...}  {"event": "line", "text": "R1kori7h: Hello"}
send:   {"event": "ready", "name": "R1kori7h"}  {"event": "sent", "text": "Hello"}  {"event": "heard", "text": "Zorkly: hi"}
```

`send` reads the lines to transmit on its standard input. It types them one character every
3 ms, because the Flipper's chat drops characters that arrive faster.

---

## Using the engine alone

`flipchat-radio` works on its own in a terminal, without the app:

```bash
flipchat-radio listen                        # listener: prints every message
flipchat-radio send                          # sender: type lines, Enter sends, Ctrl+D quits
flipchat-radio listen -f 315e6 -g 20         # frequency in Hz, gain in dB
flipchat-radio listen --packets              # also print every raw packet
flipchat-radio send --port /dev/ttyACM0
```

The Flipper side also works by hand from any serial terminal, which is all `send` does.
Open `screen /dev/cu.usbmodemflip_*` (Linux: `/dev/ttyACM0`), press Enter, and type
`subghz chat 433920000 0` (frequency in Hz, then 0 = internal radio, 1 = external module).
**Ctrl+C** leaves the chat. [Flipper Lab](https://lab.flipper.net)'s CLI page does the
same from Chrome or Edge.

### Recording

Record the raw radio with the dongle, then decode it later, as text or in the app:

```bash
rtl_sdr -f 433670000 -s 1024000 capture.cu8      # 250 kHz below 433.92 MHz; Ctrl+C to stop
flipchat-radio listen --file capture.cu8
flipchat -file capture.cu8                       # replayed at real speed
```

## Development

```bash
git clone https://github.com/alelouis/rtlsdr-flipper-chat
cd rtlsdr-flipper-chat
uv sync                                      # the engine, in .venv
(cd app && go build -o ../flipchat .)        # the app (needs Go 1.24+)
./flipchat                                   # uses the engine from this clone's .venv
```

```
app/                     flipchat, the app (Go)
  main.go                options
  model.go               role screen, chat screens, keys
  view.go                drawing
  backend.go             runs flipchat-radio and reads its events
src/flipchat_radio/      flipchat-radio, the engine (Python)
  receiver.py            dongle samples -> chat lines
  flipper.py             the Flipper's USB command line and its chat
  cli.py                 the send and listen commands
tests/                   engine tests; sim.py simulates the radio signal and a fake Flipper
scripts/build-release.sh release archives
```

**Tests** need no hardware:

```bash
uv run pytest                    # engine: decoding, the Flipper session, the commands
(cd app && go test ./...)        # app: screens driven key by key, process handling
```

The engine tests generate the Flipper's signal in software, with noise and frequency
errors, and use a fake Flipper on a fake USB port. The app tests drive the screens with a
fake engine, and the process handling with small shell scripts standing in for it.
GitHub Actions runs them, plus `ruff` and `gofmt`, on every push.

**Releasing:** bump `version` in `pyproject.toml`, commit, then push a matching tag:
`git tag v0.2.0 && git push origin v0.2.0`. GitHub Actions tests, builds `flipchat` for
macOS and Linux (Intel and ARM), and publishes the release.

## Licence and credits

MIT, see [LICENSE](LICENSE). The chat itself is the Flipper Zero firmware's
`subghz chat`; this project adds the RTL-SDR receiver and the app.
