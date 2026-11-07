import pytest
from sim import FakeFlipperLink

from flipcom_chat import flipper
from flipcom_chat.flipper import Flipper, is_own_echo


@pytest.fixture(autouse=True)
def fast(monkeypatch):
    monkeypatch.setattr(flipper.time, "sleep", lambda s: None)


def test_connect_sends_ctrl_c_then_waits_for_the_prompt():
    link = FakeFlipperLink()
    Flipper(link=link)
    assert link.write_sizes[0] == 2  # b"\x03\r"


def test_name_is_read_from_info_device():
    assert Flipper(link=FakeFlipperLink(name="Zorkly")).name() == "Zorkly"


def test_close_app_leaves_the_flipper_alone_when_nothing_runs():
    link = FakeFlipperLink()
    Flipper(link=link).close_app()
    assert "loader close" not in link.commands


def test_close_app_closes_a_running_app():
    link = FakeFlipperLink(app_running=True)
    Flipper(link=link).close_app()
    assert link.commands[-1] == "loader close"


def test_start_chat_and_type_lines():
    link = FakeFlipperLink()
    f = Flipper(link=link)
    f.start_chat(433.92e6, 0)
    assert link.commands[-1] == "subghz chat 433920000 0"
    f.type_line("Hello there")
    f.leave_chat()
    assert link.chat_lines == ["R1kori7h joined chat.", "R1kori7h: Hello there", "R1kori7h left chat."]
    assert link.closed


def test_lines_are_typed_one_byte_at_a_time():
    # the Flipper's chat drops characters written in bursts
    link = FakeFlipperLink()
    f = Flipper(link=link)
    f.start_chat(433.92e6)
    link.write_sizes.clear()
    f.type_line("héllo")
    assert link.write_sizes == [1] * len("héllo\r".encode())


def test_start_chat_reports_the_flipper_refusing():
    with pytest.raises(RuntimeError, match="(?s)could not start subghz chat: .*application is open"):
        Flipper(link=FakeFlipperLink(chat_ok=False)).start_chat(433.92e6)


def test_a_silent_flipper_gives_a_clear_error():
    with pytest.raises(TimeoutError, match="unplug it and plug it back in"):
        Flipper(link=FakeFlipperLink(silent=True))


@pytest.mark.parametrize(
    "line, own_echo",
    [
        ("R1kori7h: hi", True),  # our own line, as the chat prints it
        ("R1kori7h joined chat.", True),
        ("hi there", True),  # the echo of what we typed
        ("Zorkly: hello", False),
        ("Zorkly joined chat.", False),
    ],
)
def test_is_own_echo(line, own_echo):
    assert is_own_echo(line, "R1kori7h", ["hi there"]) is own_echo


def test_is_own_echo_without_a_name_only_uses_sent_lines():
    assert not is_own_echo("R1kori7h: hi", None, [])
