# flipcom-chat: Flipper Zero → RTL-SDR text chat

Type a line on a **Flipper Zero** and read it on a computer with a cheap **RTL-SDR** dongle.

```
 you type ──▶ Flipper Zero ~~~ 433.92 MHz radio ~~~▶ RTL-SDR dongle ──▶ "R1kori7h: Hello!"
```

The Flipper already has a hidden chat built into its firmware (`subghz chat`), meant for
Flipper-to-Flipper messaging. This project adds the missing half: a **software receiver
for that chat**, so any computer with a ~€30 RTL-SDR dongle can read it. No custom
firmware, no app to install on the Flipper, no files to copy.

Two people, two computers: the **sender** types through a Flipper, the **listener** reads
with an RTL-SDR. Both run the same terminal app, `flipchat`, and pick their role when it starts.

```
 Sender's computer (Flipper on USB)                Listener's computer (RTL-SDR on USB)

 flipchat  Sender · 433.920 MHz   ● Flipper R1kori7h      flipchat  Listener · 433.920 MHz    ● RTL-SDR listening
10:01:46  R1kori7h  Hi listener, this is the sender  sent  10:01:41  → R1kori7h joined chat.
╭───────────────────────────────────────────────╮         10:01:46  R1kori7h  Hi listener, this is the sender
│› Type a message and press Enter               │         10:01:55  → R1kori7h left chat.
╰───────────────────────────────────────────────╯
```

Tested on real hardware: a Flipper Zero on official firmware 1.5.1 and an RTL-SDR dongle
with an R820T tuner. Every packet from the Flipper passed its CRC check. (Screens above:
a real session, trimmed.)

---

## What you need

| | |
|---|---|
| **Flipper Zero** | Official firmware (or any fork that keeps `subghz chat`), plugged into the computer over USB |
| **RTL-SDR dongle** | Any RTL2832U dongle (e.g. RTL-SDR Blog v3/v4) with its antenna |
| **Computer** | macOS or Linux, with [uv](https://docs.astral.sh/uv/) (Python package manager) and [Go](https://go.dev/dl/) 1.24+ (to build the app) |

The sender needs only the Flipper, the listener only the dongle. Install the project on
both computers. (Both devices on one computer also works: run `flipchat` twice, once per role.
That's handy for testing.)

> **Legal:** only transmit on frequencies allowed where you live. 433.92 MHz is a
> licence-free ISM frequency in Europe (low power, short transmissions). In the US, use
> 315 MHz (`./flipchat -f 315`). The Flipper's firmware also refuses frequencies that are not
> allowed in its region.

## Install

```bash
# 1. RTL-SDR drivers (provides rtl_sdr and rtl_test)
brew install librtlsdr          # macOS
sudo apt install rtl-sdr        # Debian / Ubuntu

# 2. uv and Go, if you don't have them
brew install uv go              # macOS (Linux: see docs.astral.sh/uv and go.dev/dl)

# 3. This project
git clone https://github.com/alelouis/rtlsdr-flipper-chat
cd rtlsdr-flipper-chat
uv sync                                   # the Python receiver: creates .venv with numpy and pyserial
(cd tui && go build -o ../flipchat .)     # the terminal app: builds ./flipchat

# 4. Optional: check everything works, no hardware needed
uv run pytest -q && (cd tui && go test ./...)
```

Check the dongle is detected:

```bash
rtl_test -t      # should print "Found 1 device(s)" and a tuner name; Ctrl+C to stop
```

With an R820T/R828D tuner, `No E4000 tuner found, aborting.` is normal (the `-t` test is
for another tuner). On Linux, if you get `usb_claim_interface error`, run
`sudo rmmod dvb_usb_rtl28xxu` (the TV driver grabbed the dongle).

## Use it

On each computer, in the project folder:

```bash
./flipchat
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

Pick with **↑ ↓** (or **1** / **2**) and press **Enter**. Press **Tab** to change the frequency
first: both sides must use the same one.

**Sender** (Flipper on USB, qFlipper closed, since only one program can use the Flipper's
port). The header shows `● Flipper <its name>` once connected. Type a line and press
**Enter**: it shows `sending…`, then `sent` once the Flipper has transmitted it. Lines from
other Flippers on the same frequency appear too.

**Listener** (RTL-SDR on USB, antenna attached). The header shows `● RTL-SDR listening` and
a message count. Every chat line the dongle decodes appears with the sender's name, as
well as `→ name joined chat.` / `left chat.` when a sender starts or stops.

Start with the two devices a few metres apart, then try further away.

| Key | |
|---|---|
| **Enter** | send the line (sender) |
| **↑ ↓ PgUp PgDn Home End** | scroll the history |
| **Ctrl+R** | reconnect the device after it dropped (e.g. replugged) |
| **Esc** | leave the chat and go back to the role screen |
| **Ctrl+C** (or **q** for the listener) | quit |

Options skip the setup screen or tune each role:

```bash
./flipchat -role sender                   # straight to the sender
./flipchat -role listener                 # straight to the listener
./flipchat -f 315                         # frequency in MHz (US: 315)
./flipchat -role listener -g 20           # fixed RTL-SDR gain in dB (default: automatic)
./flipchat -role sender -port /dev/ttyACM0   # if the Flipper isn't found automatically
./flipchat -file capture.cu8              # listener replaying a recording (see below)
```

`flipchat` is a front end: depending on the role, it runs the Python `flipcom-chat send` or
`flipcom-chat listen` in the background and reads its events (`--json`). It finds them in the
project's `.venv`, so keep the binary in the project folder, or pass
`-project /path/to/rtlsdr-flipper-chat`.

## Without the app: two terminals

The Python commands work on their own too.

**Listener's computer (RTL-SDR)**

```bash
uv run flipcom-chat listen
```

It prints `listening for Flipper chat on 433.920 MHz` and waits.

**Sender's computer (Flipper over USB)**

```bash
uv run flipcom-chat send
```

Type a line and press **Enter**. It's transmitted immediately and shows up on the
listener a fraction of a second later. Press **Ctrl+D** to quit.

### Options

```bash
uv run flipcom-chat listen -g 20             # fixed gain in dB (default: automatic)
uv run flipcom-chat listen -f 315e6          # another frequency (Hz)
uv run flipcom-chat listen --packets         # also print every raw packet
uv run flipcom-chat send -f 315e6            # same frequency on the Flipper side
uv run flipcom-chat send --port /dev/ttyACM0 # if the Flipper isn't found automatically
```

`listen` and `send` must use the same frequency.

### Without `flipcom-chat send`

`flipcom-chat send` only types into the Flipper's own command line. You can do the same
by hand from any serial terminal, and `flipcom-chat listen` will decode it just the same.

```bash
screen /dev/cu.usbmodemflip_*     # macOS. Linux: screen /dev/ttyACM0 (picocom, minicom, tio also work)
```

Press Enter to get the `>:` prompt, then start the chat:

```
subghz chat 433920000 0           # frequency in Hz, then 0 = internal radio, 1 = external module
```

- Type a line, press **Enter** to send it.
- **Ctrl+C** leaves the chat (it sends "... left chat.").
- **Ctrl+A** then **K** quits `screen`.

If `subghz` complains that an app is running, close the Sub-GHz app on the Flipper (or type
`loader close`).

No terminal program? The CLI page of [Flipper Lab](https://lab.flipper.net) works from
Chrome or Edge over USB. On Windows, use PuTTY on the Flipper's COM port.

### Talking to other Flippers

This is the Flipper's standard chat, so other Flippers running `subghz chat` on the same
frequency receive your lines too. Their lines show up for the sender (heard by its Flipper)
and for the listener (heard by the dongle).

### Decoding a recording

You can record raw radio with `rtl_sdr` and decode it later:

```bash
rtl_sdr -f 433670000 -s 1024000 capture.cu8    # tuned 250 kHz below 433.92 MHz; Ctrl+C to stop
uv run flipcom-chat listen --file capture.cu8
./flipchat -file capture.cu8                   # or replay it in the app, at real speed
```

---

## How it works

### What the Flipper sends

`subghz chat` doesn't send text as a slow on/off signal like a remote control. It uses the
Flipper's radio chip (a TI CC1101) in hardware packet mode, with the
`FuriHalSubGhzPresetGFSK9_99KbAsync` preset (from the firmware source,
`lib/subghz/devices/cc1101_configs.c`):

| | |
|---|---|
| **Modulation** | GFSK (the frequency shifts up for `1`, down for `0`), ±19 kHz deviation |
| **Speed** | 9.99 kbit/s: a 60-byte packet is on air for ~55 ms |
| **Packet** | preamble `AA AA AA AA` · sync word `46 4C` · length · 1–60 bytes of data · CRC-16 |
| **CRC** | CRC-16, polynomial 0x8005, start value 0xFFFF (the CC1101's hardware CRC) |
| **Data** | `ESC[0;33m<flipper name>ESC[0m: <your line>\r\n` (the name is in colour) |

A line longer than 60 bytes is split over several packets about 10 ms apart. Joining and
leaving the chat send a line too (`<name> joined chat.`).

### What the receiver does

`flipcom-chat listen` runs `rtl_sdr` at 1.024 million samples per second. The dongle is tuned
250 kHz below the chat frequency, because RTL-SDR dongles have a spike of noise right at
their centre. Then, in `src/flipcom_chat/receiver.py`, for each block of samples:

1. **Move the signal to 0 Hz** (shift by the 250 kHz offset) and **filter** to keep only
   the ~50 kHz around the chat channel.
2. **Find bursts.** Measure the power every 1 ms. A burst is power 10 dB above the noise floor.
   The noise floor is learned only from quiet moments, so a long chat never raises it.
3. **FM-demodulate** each burst: the frequency over time gives the bits (high = `1`). The
   receiver measures the Flipper's actual carrier offset itself, so a dongle that is a few
   kHz off still works.
4. **Find the bit timing** by sliding the known pattern `AA AA 46 4C` (end of preamble + sync
   word) along the signal and picking the best match.
5. **Read the packet:** length, data, CRC. Packets with a wrong CRC are dropped.
6. **Rebuild the lines:** join packets until `\n`, strip the colour codes, print.

It processes the stream block by block, so a packet split between two blocks is still
decoded. In simulation it decodes down to 0 dB SNR, and it runs about 20× faster than
real time. rtl_433 (a popular generic decoder) only caught 1 of 3 packets of this chat,
which is why this project has its own receiver.

### Project layout

```
src/flipcom_chat/            the Python side
  receiver.py   RTL-SDR samples -> chat lines (ChatDemodulator, LineAssembler)
  flipper.py    the Flipper's USB command line and its `subghz chat`
  cli.py        the flipcom-chat command: send, listen
tests/
  sim.py        simulated GFSK packets, and a fake Flipper on a fake USB port
  test_*.py     receiver, Flipper session and command tests
tui/                         the flipchat terminal app (Go, Bubble Tea)
  main.go       options
  model.go      role screen, chat screens, keys
  view.go       drawing
  backend.go    runs `flipcom-chat send` or `listen --json` and reads its events
  *_test.go     screens driven key by key, and the process plumbing with stand-in scripts
```

With `--json`, the Python commands print one event per line, which is all the app reads:

```
listen: {"event": "listening", ...}  {"event": "line", "text": "R1kori7h: Hello"}
send:   {"event": "ready", "name": "R1kori7h"}  {"event": "sent", "text": "Hello"}  {"event": "heard", "text": "Zorkly: hi"}
```

`send` reads the lines to transmit on its standard input.

### Tests

None of the tests need hardware:

```bash
uv run pytest                 # Python: receiver, Flipper session, commands
(cd tui && go test ./...)     # Go: the app's screens and process handling
```

- **Receiver:** the tests generate the Flipper's GFSK signal in software, with noise and
  frequency errors. They cover long lines over several packets, UTF-8, CRC errors, low SNR,
  pure noise, any chunk size and other sample rates.
- **Flipper:** a fake USB port plays the Flipper's command line and chat, including its
  refusals, an app left open and a frozen Flipper.
- **Commands:** `listen` decodes recordings in text and JSON, and `send` drives the fake Flipper
  end to end.
- **App:** the screens are driven key by key with a fake launcher, and the process handling is
  tested with shell scripts standing in for Python.

GitHub Actions runs them all, plus `ruff` and `gofmt`, on every push.

---

## Troubleshooting

| Problem | Fix |
|---------|-----|
| `rtl_sdr not found` | Install the drivers: `brew install librtlsdr` / `sudo apt install rtl-sdr` |
| `usb_open error` | Another program (SDR++, GQRX, rtl_433...) is using the dongle: close it |
| `... is busy: quit qFlipper` | qFlipper holds the Flipper's USB port: quit it |
| `no Flipper found on USB` | Plug it in and unlock it, or pass `--port` |
| `the Flipper doesn't answer on USB` | Its command line is stuck (e.g. a chat program was killed mid-way): unplug it, plug it back in, press Ctrl+R |
| `could not start subghz chat` | Close the app open on the Flipper; check the frequency is allowed in your region |
| Nothing is decoded | Check the antenna is on the dongle and both sides use the same frequency. Move closer. If the Flipper is very close, try a lower gain (`-g 20`); if it's far, a higher one (`-g 40`). `listen --packets` shows every packet that passes its CRC |

## Licence and credits

MIT, see [LICENSE](LICENSE).

The chat itself is the Flipper Zero firmware's `subghz chat`. This project only adds the
RTL-SDR receiver, a small helper to type into it, and a terminal app built with
[Bubble Tea](https://github.com/charmbracelet/bubbletea).
