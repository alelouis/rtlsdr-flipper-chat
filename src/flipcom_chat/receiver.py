"""Receiver for the Flipper Zero's `subghz chat`, from RTL-SDR samples to text lines.

`subghz chat` makes the Flipper's CC1101 send hardware packets with the preset
FuriHalSubGhzPresetGFSK9_99KbAsync (firmware lib/subghz/devices/cc1101_configs.c):

    GFSK, 9.99 kbit/s, deviation +-19.04 kHz, higher frequency = 1
    preamble AA AA AA AA | sync 46 4C | LEN | DATA[1..60] | CRC-16 (poly 0x8005, init 0xFFFF)

DATA is the text the chat typed: "\\e[0;33m<flipper name>\\e[0m: <line>\\r\\n".
Lines longer than 60 bytes continue in the next packets (~10 ms apart).
"""

from __future__ import annotations

import re
from dataclasses import dataclass

import numpy as np

BAUD = 26e6 * (256 + 0x93) * 2**8 / 2**28  # CC1101 MDMCFG4/3 = 0xC8/0x93 -> 9992.6 baud
DEVIATION = 19_043  # Hz, CC1101 DEVIATN = 0x34
PREAMBLE = b"\xaa\xaa\xaa\xaa"
SYNC = b"\x46\x4c"
MAX_DATA = 60  # bytes of text per packet
ANSI = re.compile(r"\x1b\[[0-9;]*m")


def crc16_cc1101(data: bytes) -> int:
    """The CC1101's hardware CRC (CRC-16/CMS): poly 0x8005, init 0xFFFF, no reflection."""
    crc = 0xFFFF
    for byte in data:
        crc ^= byte << 8
        for _ in range(8):
            crc = ((crc << 1) ^ 0x8005) & 0xFFFF if crc & 0x8000 else (crc << 1) & 0xFFFF
    return crc


def cu8_to_iq(raw: bytes) -> np.ndarray:
    """rtl_sdr's interleaved unsigned 8-bit I/Q -> complex64 in [-1, 1]."""
    a = np.frombuffer(raw, dtype=np.uint8)
    a = (a[: len(a) // 2 * 2].astype(np.float32) - 127.5) / 127.5
    return (a[0::2] + 1j * a[1::2]).astype(np.complex64)


@dataclass
class Packet:
    data: bytes
    position: int  # sample index of the burst start


class ChatDemodulator:
    """Streaming receiver: feed IQ blocks, get CRC-checked chat packets.

    The carrier is expected `offset_hz` above the tuned frequency (the dongle is
    tuned off-centre to avoid its DC spike).
    """

    def __init__(self, fs: float = 1.024e6, offset_hz: float = 250e3, threshold_db: float = 10.0):
        self.fs, self.offset = fs, offset_hz
        self.sps = fs / BAUD  # samples per bit
        self.block = int(fs / 1000)  # 1 ms power blocks for burst detection
        self.threshold = 10 ** (threshold_db / 10)
        self.buf = np.zeros(0, np.complex64)  # filtered baseband not yet scanned
        self.base = 0  # absolute sample index of buf[0]
        self.noise: float | None = None
        self.phase = 0.0
        self.taps = max(1, int(fs / 50e3))  # boxcar channel filter, first null at ~50 kHz
        self.tail = np.zeros(self.taps - 1, np.complex64)
        # bit timing reference: the end of the preamble + sync word, as +-1 per sample
        bits = np.unpackbits(np.frombuffer(PREAMBLE[:2] + SYNC, np.uint8)).astype(np.float32) * 2 - 1
        self.template = bits[(np.arange(int(32 * self.sps)) / self.sps).astype(int)]

    def feed(self, iq: np.ndarray) -> list[Packet]:
        # mix to baseband phase-continuously, so a burst can span feed() calls
        n = np.arange(len(iq))
        lo = np.exp(-1j * (self.phase + 2 * np.pi * self.offset * n / self.fs))
        self.phase = (self.phase + 2 * np.pi * self.offset * len(iq) / self.fs) % (2 * np.pi)
        x = np.concatenate((self.tail, (iq * lo).astype(np.complex64)))
        self.tail = x[len(x) - (self.taps - 1) :] if self.taps > 1 else x[:0]
        bb = np.convolve(x, np.ones(self.taps, np.float32) / self.taps, "valid").astype(np.complex64)
        self.buf = np.concatenate((self.buf, bb))
        return self._scan(final=False)

    def flush(self) -> list[Packet]:
        """End of stream: decode whatever burst is still pending."""
        return self._scan(final=True)

    def _scan(self, final: bool) -> list[Packet]:
        k = self.block
        nblk = len(self.buf) // k
        if nblk == 0:
            return []
        p = (np.abs(self.buf[: nblk * k].reshape(nblk, k)) ** 2).mean(1)
        if self.noise is None:
            self.noise = float(np.percentile(p, 20))
        on = p > self.noise * self.threshold
        quiet = p[~on]
        if len(quiet):  # track the floor from burst-free blocks only, so packets never raise it
            self.noise = 0.95 * self.noise + 0.05 * float(np.median(quiet))
        d = np.diff(np.r_[0, on.astype(np.int8), 0])
        starts, ends = np.flatnonzero(d == 1), np.flatnonzero(d == -1)
        out, consumed = [], 0
        for a, b in zip(starts, ends):
            if b + 2 > nblk and not final:  # burst (or its trailing margin) not complete yet
                consumed = max(0, a - 2) * k
                break
            seg = self.buf[max(0, (a - 2) * k) : min(len(self.buf), (b + 2) * k)]
            data = self.demod(seg)
            if data is not None:
                out.append(Packet(data, self.base + a * k))
            consumed = min(len(self.buf), (b + 2) * k)
        else:
            # nothing pending: keep a short tail so the next burst's start isn't cut
            consumed = max(consumed, len(self.buf) - 4 * k)
        if consumed > 0:
            self.buf = self.buf[consumed:]
            self.base += consumed
        return out

    def demod(self, bb: np.ndarray) -> bytes | None:
        """Channel-filtered baseband burst -> packet data, if the sync is found and the CRC matches."""
        if len(bb) < 40 * self.sps:
            return None
        fm = np.angle(bb[1:] * np.conj(bb[:-1]))
        m = max(1, int(self.sps * 0.6))
        fm = np.convolve(fm, np.ones(m) / m, "same")
        # carrier offset: FM median over the samples that belong to the burst
        mag = np.abs(bb[1:])
        dc = np.median(fm[mag > 0.5 * mag.max()])
        # bit timing: correlate with the known end of preamble + sync word
        tpl = self.template
        search = fm[: min(len(fm), int(20e-3 * self.fs) + len(tpl))] - dc
        if len(search) < len(tpl):
            return None
        t_sync = int(np.argmax(np.correlate(search, tpl, "valid")))
        dc = fm[t_sync : t_sync + int(16 * self.sps)].mean()  # AA AA is balanced: exact slicing level
        return self._parse(fm > dc, t_sync + 32 * self.sps)

    def _parse(self, bits_hi: np.ndarray, t0: float) -> bytes | None:
        nbits = int((len(bits_hi) - t0) / self.sps) - 1
        if nbits < 8:
            return None
        idx = (t0 + (np.arange(nbits) + 0.5) * self.sps).astype(int)
        raw = np.packbits(bits_hi[idx].astype(np.uint8)[: nbits // 8 * 8]).tobytes()
        n = raw[0]
        if not 1 <= n <= MAX_DATA + 1 or len(raw) < n + 3:
            return None
        body, crc = raw[: n + 1], int.from_bytes(raw[n + 1 : n + 3], "big")
        return body[1:] if crc16_cc1101(body) == crc else None


class LineAssembler:
    """Chat lines arrive as 1+ packets: join them until '\\n', or until the timeout."""

    def __init__(self, timeout_s: float = 0.5):
        self.buf, self.last, self.timeout = b"", 0.0, timeout_s

    def add(self, data: bytes, t: float) -> list[str]:
        lines = self.expire(t)
        self.buf += data
        self.last = t
        while b"\n" in self.buf:
            line, self.buf = self.buf.split(b"\n", 1)
            lines.append(self._clean(line))
        return lines

    def expire(self, t: float) -> list[str]:
        """A line whose last packet was lost: stop waiting for its end after the timeout."""
        if self.buf and t - self.last > self.timeout:
            return self.flush()
        return []

    def flush(self) -> list[str]:
        out = [self._clean(self.buf)] if self.buf else []
        self.buf = b""
        return out

    @staticmethod
    def _clean(raw: bytes) -> str:
        return ANSI.sub("", raw.decode("utf-8", errors="replace")).rstrip("\r")
