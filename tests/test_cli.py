import io
import json
import threading
import time

import pytest
from sim import FakeFlipperLink, chat_line, chat_packets, gfsk_cu8

from flipchat_radio import cli, flipper
from flipchat_radio.flipper import Flipper


def events(text: str) -> list[dict]:
    return [json.loads(line) for line in text.splitlines() if line.strip()]


@pytest.fixture
def recording(tmp_path):
    path = tmp_path / "chat.cu8"
    pkts = chat_packets(chat_line("R1kori7h", "Hello")) + chat_packets(chat_line("Zorkly", "Hi back"))
    path.write_bytes(gfsk_cu8(pkts))
    return str(path)


# ---- listen


def test_listen_file_prints_lines(recording, capsys):
    assert cli.main(["listen", "--file", recording]) == 0
    out, err = capsys.readouterr()
    assert "R1kori7h: Hello" in out and "Zorkly: Hi back" in out
    assert "2 line(s) decoded" in err


def test_listen_file_json_events(recording, capsys):
    assert cli.main(["listen", "--file", recording, "--json"]) == 0
    evs = events(capsys.readouterr().out)
    assert evs[0] == {"event": "listening", "freq": 433.92e6, "source": recording}
    assert [(e["event"], e["text"]) for e in evs[1:]] == [("line", "R1kori7h: Hello"), ("line", "Zorkly: Hi back")]
    assert all(isinstance(e["t"], float) for e in evs[1:])


def test_listen_file_with_nothing_in_it_fails(tmp_path, capsys):
    empty = tmp_path / "quiet.cu8"
    empty.write_bytes(bytes([127, 128]) * 100_000)
    assert cli.main(["listen", "--file", str(empty)]) == 1
    assert "0 line(s) decoded" in capsys.readouterr().err


def test_listen_packets_option_shows_raw_packets(recording, capsys):
    cli.main(["listen", "--file", recording, "--packets"])
    assert "[packet t=" in capsys.readouterr().err


def test_listen_without_rtl_sdr_explains(monkeypatch):
    monkeypatch.setattr(cli.shutil, "which", lambda name: None)
    with pytest.raises(SystemExit, match="rtl_sdr not found"):
        cli.main(["listen"])


def test_paced_replays_at_the_sample_rate():
    t0 = time.monotonic()
    list(cli.paced(iter([bytes(2000)] * 5), fs=50_000))  # 5 x 1000 samples = 0.1 s
    assert 0.09 < time.monotonic() - t0 < 0.5


# ---- send


@pytest.fixture
def fake_flipper(monkeypatch):
    """`send` talks to a FakeFlipperLink instead of a USB port."""
    monkeypatch.setattr(flipper, "CHAR_DELAY_S", 0)
    link = FakeFlipperLink()
    monkeypatch.setattr(cli, "Flipper", lambda port=None: Flipper(link=link))
    return link


def run_send(monkeypatch, capsys, lines: str, *args):
    monkeypatch.setattr(cli.sys, "stdin", io.StringIO(lines))
    code = cli.main(["send", "--json", *args])
    return code, events(capsys.readouterr().out)


def test_send_types_each_line_and_reports_it(monkeypatch, capsys, fake_flipper):
    code, evs = run_send(monkeypatch, capsys, "hello\n\nsecond line\n")
    assert code == 0
    assert evs[0] == {"event": "ready", "name": "R1kori7h", "freq": 433.92e6, "port": "/dev/fake-flipper"}
    assert [e for e in evs if e["event"] == "sent"] == [
        {"event": "sent", "text": "hello"},
        {"event": "sent", "text": "second line"},  # the empty line is skipped
    ]
    assert fake_flipper.chat_lines == [
        "R1kori7h joined chat.",
        "R1kori7h: hello",
        "R1kori7h: second line",
        "R1kori7h left chat.",
    ]
    assert fake_flipper.closed


def test_send_uses_the_frequency_and_device(monkeypatch, capsys, fake_flipper):
    run_send(monkeypatch, capsys, "", "-f", "315e6", "--device", "1")
    assert "subghz chat 315000000 1" in fake_flipper.commands


def test_send_reports_other_flippers_but_not_its_own_echo(monkeypatch, capsys, fake_flipper):
    class SlowStdin(io.StringIO):  # keep the chat open while another Flipper talks
        def readline(self):
            time.sleep(1.2)
            return super().readline()

    threading.Timer(0.8, lambda: fake_flipper.inject("Zorkly: anyone there?")).start()
    monkeypatch.setattr(cli.sys, "stdin", SlowStdin("me\n"))
    cli.main(["send", "--json"])
    evs = events(capsys.readouterr().out)
    assert {"event": "heard", "text": "Zorkly: anyone there?"} in evs
    assert [e for e in evs if e["event"] == "heard"] == [{"event": "heard", "text": "Zorkly: anyone there?"}]


def test_send_exits_with_the_flipper_error(monkeypatch):
    monkeypatch.setattr(flipper.time, "sleep", lambda s: None)
    monkeypatch.setattr(cli, "Flipper", lambda port=None: Flipper(link=FakeFlipperLink(chat_ok=False)))
    with pytest.raises(SystemExit, match="^error: could not start subghz chat"):
        cli.main(["send", "--json"])


def test_send_without_a_flipper(monkeypatch):
    monkeypatch.setattr(flipper.glob, "glob", lambda pattern: [])
    with pytest.raises(SystemExit, match="no Flipper found on USB"):
        cli.main(["send"])
