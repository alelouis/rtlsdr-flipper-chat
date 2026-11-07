import numpy as np
import pytest
from sim import FS, OFFSET, chat_line, chat_packets, gfsk_cu8

from flipchat_radio.receiver import ChatDemodulator, LineAssembler, crc16_cc1101, cu8_to_iq


def decode(cu8: bytes, chunk: int = 65536, fs=FS, offset=OFFSET) -> list[str]:
    dem, asm, lines = ChatDemodulator(fs, offset), LineAssembler(), []
    for i in range(0, len(cu8), chunk):
        for pk in dem.feed(cu8_to_iq(cu8[i : i + chunk])):
            lines += asm.add(pk.data, pk.position / fs)
    for pk in dem.flush():
        lines += asm.add(pk.data, pk.position / fs)
    return lines + asm.flush()


# ---- building blocks


def test_crc_matches_the_crc16_cms_check_value():
    assert crc16_cc1101(b"123456789") == 0xAEE7  # CRC-16/CMS = the CC1101's hardware CRC


def test_crc_of_empty_data_is_the_init_value():
    assert crc16_cc1101(b"") == 0xFFFF


def test_cu8_to_iq_scales_to_unit_range_and_pairs_i_q():
    iq = cu8_to_iq(bytes([255, 0, 0, 255, 128, 127]))
    assert iq.dtype == np.complex64
    np.testing.assert_allclose(iq, [1 - 1j, -1 + 1j, 0.0039 - 0.0039j], atol=1e-3)


def test_cu8_to_iq_ignores_a_trailing_odd_byte():
    assert len(cu8_to_iq(bytes(5))) == 2


def test_chat_packets_split_long_lines_at_60_bytes():
    pkts = chat_packets(b"x" * 125)
    assert [p[6] for p in pkts] == [60, 60, 5]  # length byte after preamble + sync


# ---- demodulation


def test_decodes_chat_lines_and_strips_colour_codes():
    pkts = chat_packets(chat_line("R1kori7h", "Hello")) + chat_packets(chat_line("R1kori7h", "second line"))
    assert decode(gfsk_cu8(pkts)) == ["R1kori7h: Hello", "R1kori7h: second line"]


def test_decodes_join_and_leave_lines():
    pkts = chat_packets(b"\x1b[0;33mR1kori7h joined chat.\x1b[0m\r\n")
    assert decode(gfsk_cu8(pkts)) == ["R1kori7h joined chat."]


def test_long_line_split_over_packets_is_reassembled():
    text = "a long line that does not fit into one sixty byte CC1101 packet at all"
    pkts = chat_packets(chat_line("R1kori7h", text))
    assert len(pkts) == 2
    assert decode(gfsk_cu8(pkts)) == [f"R1kori7h: {text}"]


def test_full_60_byte_packets():
    text = "y" * (3 * 60 - len(chat_line("x", "")))  # exactly three full packets
    pkts = chat_packets(chat_line("x", text))
    assert [p[6] for p in pkts] == [60, 60, 60]
    assert decode(gfsk_cu8(pkts)) == [f"x: {text}"]


def test_utf8_text_survives():
    assert decode(gfsk_cu8(chat_packets(chat_line("x", "café ✓ 📡")))) == ["x: café ✓ 📡"]


@pytest.mark.parametrize("chunk", [4096, 10_000, 65536, 300_000])
def test_results_do_not_depend_on_chunk_size(chunk):
    pkts = chat_packets(chat_line("R1kori7h", "chunks")) * 3
    assert decode(gfsk_cu8(pkts, seed=2), chunk) == ["R1kori7h: chunks"] * 3


@pytest.mark.parametrize("freq_error", [-15e3, -5e3, 0, 5e3, 15e3])
def test_tolerates_carrier_frequency_errors(freq_error):
    # cheap dongles and the Flipper's crystal are each off by a few kHz
    assert decode(gfsk_cu8(chat_packets(chat_line("x", "drift")), freq_error=freq_error)) == ["x: drift"]


def test_decodes_at_low_snr():
    assert decode(gfsk_cu8(chat_packets(chat_line("R1kori7h", "weak")), snr_db=3, seed=5)) == ["R1kori7h: weak"]


def test_other_sample_rates():
    fs = 2.048e6
    cu8 = gfsk_cu8(chat_packets(chat_line("x", "fast")), fs=fs)
    assert decode(cu8, fs=fs) == ["x: fast"]


def test_corrupted_packet_is_dropped_by_crc():
    pk = bytearray(chat_packets(chat_line("x", "bad"))[0])
    pk[10] ^= 0x01
    assert decode(gfsk_cu8([bytes(pk)] + chat_packets(chat_line("x", "good")))) == ["x: good"]


def test_noise_alone_decodes_nothing():
    rng = np.random.default_rng(1)
    noise = np.clip(127.5 + 8 * rng.standard_normal(2 * int(FS)), 0, 255).astype(np.uint8).tobytes()
    assert decode(noise) == []


def test_packet_positions_are_absolute_sample_indices():
    dem = ChatDemodulator(FS, OFFSET)
    cu8 = gfsk_cu8(chat_packets(chat_line("x", "where")))
    pkts = []
    for i in range(0, len(cu8), 8192):
        pkts += dem.feed(cu8_to_iq(cu8[i : i + 8192]))
    pkts += dem.flush()
    assert len(pkts) == 1
    assert abs(pkts[0].position / FS - 0.2) < 0.005  # the simulated burst starts after 200 ms of silence


# ---- line assembly


def test_line_assembler_joins_packets_until_newline():
    asm = LineAssembler()
    assert asm.add(b"\x1b[0;33mx\x1b[0m: hel", 1.0) == []
    assert asm.add(b"lo\r\nnext", 1.01) == ["x: hello"]
    assert asm.flush() == ["next"]
    assert asm.flush() == []


def test_line_missing_its_end_is_shown_after_the_timeout():
    asm = LineAssembler(timeout_s=0.5)
    assert asm.add(b"\x1b[0;33mx\x1b[0m: first half", 1.0) == []
    assert asm.expire(1.2) == []
    assert asm.expire(1.6) == ["x: first half"]
    assert asm.add(b"next\r\n", 5.0) == ["next"]


def test_late_packet_starts_a_new_line():
    asm = LineAssembler(timeout_s=0.5)
    asm.add(b"lost its end", 1.0)
    assert asm.add(b"new\r\n", 3.0) == ["lost its end", "new"]
