"""Test helpers: what a Flipper running `subghz chat` puts on air, and what it says over USB."""

from __future__ import annotations

import threading
import time

import numpy as np

from flipchat_radio.receiver import BAUD, DEVIATION, MAX_DATA, PREAMBLE, SYNC, crc16_cc1101

FS, OFFSET = 1.024e6, 250e3


def chat_line(name: str, text: str) -> bytes:
    """The bytes the chat sends for one typed line."""
    return f"\x1b[0;33m{name}\x1b[0m: {text}\r\n".encode()


def chat_packets(data: bytes) -> list[bytes]:
    """Split into <=60-byte CC1101 packets, as the Flipper's chat worker does."""
    out = []
    for i in range(0, len(data), MAX_DATA):
        body = bytes([len(data[i : i + MAX_DATA])]) + data[i : i + MAX_DATA]
        out.append(PREAMBLE + SYNC + body + crc16_cc1101(body).to_bytes(2, "big"))
    return out


def gfsk_cu8(packets: list[bytes], freq_error=3e3, snr_db=15.0, gap_s=0.012, seed=0, fs=FS, offset=OFFSET) -> bytes:
    """rtl_sdr-style cu8 samples of GFSK packets (BT~0.5) with noise, tuned `offset` below the carrier.

    snr_db is measured over the whole sampled band; the chat channel sees ~12 dB more.
    """
    rng = np.random.default_rng(seed)
    sps = fs / BAUD
    taps = np.exp(-0.5 * (np.arange(-2 * sps, 2 * sps + 1) / (0.5 * sps)) ** 2)
    taps /= taps.sum()
    silence = int(0.2 * fs)
    env, freq = [np.zeros(silence)], [np.zeros(silence)]
    for pk in packets:
        bits = np.unpackbits(np.frombuffer(pk, np.uint8)).astype(float) * 2 - 1
        nrz = np.repeat(bits, np.diff(np.round(np.arange(len(bits) + 1) * sps)).astype(int))
        freq += [np.convolve(nrz, taps, "same") * DEVIATION, np.zeros(int(gap_s * fs))]
        env += [np.ones(len(nrz)), np.zeros(int(gap_s * fs))]
    env, freq = np.concatenate(env + [np.zeros(silence)]), np.concatenate(freq + [np.zeros(silence)])
    phase = 2 * np.pi * np.cumsum(freq + offset + freq_error) / fs
    sigma = np.sqrt(10 ** (-snr_db / 10) / 2)
    noise = sigma * (rng.standard_normal(len(env)) + 1j * rng.standard_normal(len(env)))
    x = 0.4 * (env * np.exp(1j * phase) + noise)
    raw = np.empty(2 * len(x))
    raw[0::2], raw[1::2] = x.real, x.imag
    return np.clip(np.round(raw * 127.5 + 127.5), 0, 255).astype(np.uint8).tobytes()


class FakeFlipperLink:
    """Stands in for the Flipper's USB serial port: a tiny model of its CLI and `subghz chat`."""

    port = "/dev/fake-flipper"

    def __init__(self, name="R1kori7h", app_running=False, chat_ok=True, silent=False):
        self.name, self.app_running, self.chat_ok, self.silent = name, app_running, chat_ok, silent
        self.out = bytearray()  # what the Flipper will send back
        self.typed = b""
        self.commands: list[str] = []
        self.chat_lines: list[str] = []  # lines transmitted by the chat
        self.write_sizes: list[int] = []
        self.in_chat = self.closed = False
        self.lock = threading.Lock()

    # serial API
    def write(self, data: bytes) -> int:
        self.write_sizes.append(len(data))
        with self.lock:
            for b in data:
                if b == 0x03:
                    if self.in_chat:
                        self.in_chat = False
                        self.chat_lines.append(f"{self.name} left chat.")
                    self.typed = b""
                elif b == 0x0D:
                    self._line(self.typed.decode())
                    self.typed = b""
                else:
                    self.typed += bytes([b])
        return len(data)

    def read(self, n: int) -> bytes:
        with self.lock:
            data, self.out[:] = bytes(self.out[:n]), self.out[n:]
        if not data:
            time.sleep(0.005)
        return data

    def close(self) -> None:
        self.closed = True

    # the other end
    def inject(self, text: str) -> None:
        """Another Flipper's line arrives while chatting."""
        with self.lock:
            self.out += f"\x1b[0;33m{text}\x1b[0m\r\n".encode()

    def _line(self, line: str) -> None:
        if self.silent:
            return
        if self.in_chat:
            self.chat_lines.append(f"{self.name}: {line}")
            self.out += f"{line}\r\n\x1b[0;33m{self.name}\x1b[0m: {line}\r\n".encode()
            return
        self.commands.append(line)
        reply = ""
        if line == "loader info":
            reply = 'Application "Sub-GHz" is running' if self.app_running else "No application is running"
        elif line == "loader close":
            self.app_running = False
        elif line == "info device":
            reply = f"hardware.model                : Flipper Zero\r\nhardware.name                 : {self.name}"
        elif line.startswith("subghz chat"):
            if self.app_running or not self.chat_ok:
                self.out += f"{line}\r\nThis command cannot be started while an application is open\r\n\r\n>: ".encode()
                return
            self.in_chat = True
            self.chat_lines.append(f"{self.name} joined chat.")
            self.out += f"{line}\r\nReceiving at frequency 433920000 Hz\r\nPress CTRL+C to stop\r\n".encode()
            return
        self.out += f"{line}\r\n{reply}\r\n\r\n>: ".encode()
