"""flipchat-radio: the engine of the flipchat app. It drives the hardware: sends through a
Flipper Zero's built-in `subghz chat`, and receives that chat with an RTL-SDR dongle.

Most people run the flipchat app, which runs this in the background. It also works alone:

    flipchat-radio send                    # type lines; each is sent as soon as you press Enter
    flipchat-radio listen                  # live RTL-SDR receiver for Flipper chat packets
    flipchat-radio listen --file cap.cu8   # decode a recording (rtl_sdr ... cap.cu8)

With --json, both print one JSON event per line on stdout (what flipchat reads):
    listen: listening, line            send: ready, sent, heard
"""

from __future__ import annotations

import argparse
import json
import os
import shutil
import subprocess
import sys
import threading
import time
from collections.abc import Iterator

from .flipper import Flipper, is_own_echo
from .receiver import ANSI, ChatDemodulator, LineAssembler, cu8_to_iq

CHUNK = 2 * 32768  # bytes of cu8 per read: 32 ms at 1.024 MS/s


def event(kind: str, **fields) -> None:
    """One JSON event on stdout (--json)."""
    print(json.dumps({"event": kind, **fields}), flush=True)


def read_chunks(stream, nbytes: int = CHUNK) -> Iterator[bytes]:
    while data := stream.read(nbytes):
        yield data


def paced(chunks: Iterator[bytes], fs: float) -> Iterator[bytes]:
    """Replay a recording at its real speed (2 bytes per sample)."""
    t0, done = time.monotonic(), 0
    for c in chunks:
        done += len(c) // 2
        time.sleep(max(0.0, t0 + done / fs - time.monotonic()))
        yield c


def listen(chunks: Iterator[bytes], fs: float, offset: float, show_packets: bool = False, as_json: bool = False) -> int:
    """Decode a cu8 stream and print every chat line; returns how many were printed."""
    dem, asm, n = ChatDemodulator(fs, offset), LineAssembler(), 0

    def show(line: str, t: float | None) -> None:
        nonlocal n
        n += 1
        if as_json:
            event("line", text=line, t=t)
        else:
            stamp = f" [t={t:8.3f}s]" if t is not None else ""
            print(f"{time.strftime('%H:%M:%S')}{stamp} {line}", flush=True)

    def emit(pkts) -> None:
        for pk in pkts:
            t = pk.position / fs
            if show_packets:
                print(f"  [packet t={t:.3f}s {len(pk.data)} B] {pk.data!r}", file=sys.stderr)
            for line in asm.add(pk.data, t):
                show(line, t)

    seen = 0
    for chunk in chunks:
        iq = cu8_to_iq(chunk)
        seen += len(iq)
        emit(dem.feed(iq))
        for line in asm.expire(seen / fs):
            show(line, asm.last)
    emit(dem.flush())
    for line in asm.flush():
        show(line, None)
    return n


def cmd_listen(args) -> int:
    if args.file:
        if args.json:
            event("listening", freq=args.freq, source=args.file)
        with open(args.file, "rb") as fh:
            chunks = read_chunks(fh)
            if args.realtime:
                chunks = paced(chunks, args.rate)
            n = listen(chunks, args.rate, args.offset, args.packets, args.json)
        if not args.json:
            print(f"{n} line(s) decoded", file=sys.stderr)
        return 0 if n else 1

    if shutil.which("rtl_sdr") is None:
        sys.exit("error: rtl_sdr not found (install librtlsdr / rtl-sdr)")
    cmd = ["rtl_sdr", "-f", str(int(args.freq - args.offset)), "-s", str(int(args.rate))]
    if args.gain is not None:
        cmd += ["-g", str(args.gain)]
    proc = subprocess.Popen(cmd + ["-"], stdout=subprocess.PIPE, stderr=subprocess.PIPE)
    messages: list[str] = []  # rtl_sdr's own output, to explain why it stopped
    threading.Thread(
        target=lambda: messages.extend(m.decode(errors="replace").strip() for m in proc.stderr), daemon=True
    ).start()
    if args.json:
        event("listening", freq=args.freq, source="rtl-sdr")
    else:
        print(f"listening for Flipper chat on {args.freq / 1e6:.3f} MHz — Ctrl+C to stop", file=sys.stderr)
    try:
        listen(read_chunks(proc.stdout), args.rate, args.offset, args.packets, args.json)
    except KeyboardInterrupt:
        return 0
    finally:
        proc.terminate()
        proc.wait()
    time.sleep(0.1)
    why = next((m for m in reversed(messages) if m), "no samples")
    sys.exit(f"error: rtl_sdr stopped: {why}")


def cmd_send(args) -> int:
    try:
        f = Flipper(args.port)
        f.close_app()
        own = f.name()
        f.start_chat(args.freq, args.device)
    except (RuntimeError, OSError) as e:
        sys.exit(f"error: {e}")
    if args.json:
        event("ready", name=own, freq=args.freq, port=f.port)
    else:
        print(
            f"chat on {args.freq / 1e6:.3f} MHz — type a line and press Enter to send it (Ctrl+D to quit)",
            file=sys.stderr,
        )

    stop = threading.Event()
    sent: list[str] = []

    def show_others() -> None:  # lines from other Flippers, as the Flipper prints them
        pending = ""
        while not stop.is_set():
            try:
                pending += f.s.read(4096).decode(errors="replace")
            except OSError as e:
                if not stop.is_set():
                    print(f"error: lost the Flipper: {e}", file=sys.stderr, flush=True)
                    os._exit(1)
                return
            *lines, pending = pending.split("\n")
            for line in lines:
                line = ANSI.sub("", line).replace("\r", " ").strip()
                if line and not is_own_echo(line, own, sent):
                    if args.json:
                        event("heard", text=line)
                    else:
                        print(f"  < {line}", file=sys.stderr, flush=True)

    threading.Thread(target=show_others, daemon=True).start()
    try:
        for line in iter(sys.stdin.readline, ""):
            line = line.rstrip("\n")
            if line:
                sent.append(line)
                f.type_line(line)
                time.sleep(0.05)
                if args.json:
                    event("sent", text=line)
    except KeyboardInterrupt:
        pass
    except OSError as e:
        sys.exit(f"error: lost the Flipper: {e}")
    finally:
        time.sleep(0.3)
        stop.set()
        try:
            f.leave_chat()
        except OSError:
            pass
    return 0


def main(argv: list[str] | None = None) -> int:
    p = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    common = argparse.ArgumentParser(add_help=False)
    common.add_argument("-f", "--freq", type=float, default=433.92e6, help="frequency in Hz (default 433.92e6)")
    common.add_argument("--json", action="store_true", help="print JSON events on stdout (for front ends)")
    sub = p.add_subparsers(dest="cmd", required=True)

    s = sub.add_parser("send", parents=[common], help="type lines into the Flipper's subghz chat")
    s.add_argument("--port", help="Flipper serial port (default: autodetect)")
    s.add_argument("--device", type=int, default=0, help="0 = internal CC1101, 1 = external module")
    s.set_defaults(func=cmd_send)

    li = sub.add_parser("listen", parents=[common], help="receive Flipper chat with an RTL-SDR")
    li.add_argument("--file", help="decode a recorded .cu8 file instead of live")
    li.add_argument("-s", "--rate", type=float, default=1.024e6, help="sample rate (default 1.024e6)")
    li.add_argument("--offset", type=float, default=250e3, help="tune this many Hz below the carrier")
    li.add_argument("-g", "--gain", type=float, default=None, help="tuner gain in dB (default: automatic)")
    li.add_argument("--realtime", action="store_true", help="with --file: replay at the recording's real speed")
    li.add_argument("--packets", action="store_true", help="also print each raw packet")
    li.set_defaults(func=cmd_listen)

    args = p.parse_args(argv)
    return args.func(args)


if __name__ == "__main__":
    raise SystemExit(main())
