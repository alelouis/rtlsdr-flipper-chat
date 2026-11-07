"""The Flipper Zero's USB command line, and its `subghz chat` (needs pyserial, and qFlipper closed)."""

from __future__ import annotations

import glob
import re
import time

from .receiver import ANSI as ANSI_TEXT

ANSI = re.compile(rb"\x1b\[[0-9;]*m")
CHAR_DELAY_S = 0.003  # see Flipper.type_line


class Flipper:
    """Minimal client for the Flipper's USB CLI. `link` replaces the serial port (for tests)."""

    def __init__(self, port: str | None = None, link=None):
        if link is None:
            import serial

            port = port or find_port()
            try:
                link = serial.Serial(port, 230400, timeout=0.1)
            except serial.SerialException as e:
                if "busy" in str(e).lower():
                    raise RuntimeError(f"{port} is busy: quit qFlipper (only one program can use the port)") from e
                raise
        self.s = link
        self.s.write(b"\x03\r")  # Ctrl+C first: ends a chat left running, harmless at the prompt
        self._prompt(3)

    @property
    def port(self) -> str:
        return getattr(self.s, "port", "?")

    def _prompt(self, timeout: float) -> str:
        buf, end = b"", time.time() + timeout
        while time.time() < end:
            buf += self.s.read(4096)
            if buf.rstrip().endswith(b">:"):
                text = ANSI.sub(b"", buf).decode(errors="replace").rsplit(">:", 1)[0]
                return text.split("\n", 1)[-1].strip()  # drop the echoed command
        if not buf:
            raise TimeoutError("the Flipper doesn't answer on USB: unplug it and plug it back in")
        raise TimeoutError(f"no CLI prompt from the Flipper: {buf[-200:]!r}")

    def cmd(self, command: str, timeout: float = 10) -> str:
        self.s.write(command.encode() + b"\r")
        return self._prompt(timeout)

    def close_app(self) -> None:
        """`subghz` refuses to run while an app (e.g. Sub-GHz) is open."""
        info = self.cmd("loader info")  # "No application is running" or 'Application "X" is running'
        if "running" in info and not info.startswith("No application"):
            self.cmd("loader close")

    def name(self) -> str | None:
        m = re.search(r"hardware\.name\s*:\s*(\S+)", self.cmd("info device"))
        return m.group(1) if m else None

    # ---- subghz chat

    def start_chat(self, freq_hz: float, device: int = 0) -> None:
        self.s.write(f"subghz chat {int(freq_hz)} {device}\r".encode())
        time.sleep(0.5)
        banner = self.s.read(4096).decode(errors="replace")
        if "Receiving at frequency" not in banner:
            raise RuntimeError(f"could not start subghz chat: {ANSI_TEXT.sub('', banner).strip()}")

    def type_line(self, line: str) -> None:
        """Send one chat line. The chat drops characters that arrive in bursts of more than a
        few bytes (an 85-char line written at once lost ~20), so type it like a fast typist."""
        for byte in line.encode() + b"\r":
            self.s.write(bytes([byte]))
            time.sleep(CHAR_DELAY_S)

    def leave_chat(self) -> None:
        self.s.write(b"\x03")  # Ctrl+C ends the chat ("... left chat.")
        time.sleep(0.5)
        self.s.close()


def is_own_echo(line: str, own: str | None, sent: list[str]) -> bool:
    """The chat echoes what we type and our own lines; only other Flippers' lines are news."""
    if own is not None and line.startswith((f"{own}:", f"{own} ")):
        return True
    return any(line.endswith(t) for t in sent)


def find_port() -> str:
    ports = glob.glob("/dev/cu.usbmodemflip_*") + glob.glob("/dev/serial/by-id/*Flipper*")
    if not ports:
        raise RuntimeError("no Flipper found on USB (is it plugged in?)")
    return ports[0]
