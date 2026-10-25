"""Runs every example against a throwaway Toskar Core and checks what each
prints, so an example that breaks fails CI (.github/workflows/examples.yml).

    python3 examples/test.py --core <toskar daemon> --sdks <toskar-sdks checkout> \\
        --python <python with the toskar package>

Core chats with toskar-sdks' scripted model (conformance/run.py), which
answers "You said: <message>", so no model is downloaded. The JavaScript
examples use the SDK built in the checkout (npm run build in typescript/);
the Go examples use the SDK version in examples/go/go.mod, or the
checkout's with --go-sdk-from-checkout. Standard library only.
"""

from __future__ import annotations

import argparse
import os
import re
import shutil
import subprocess
import sys
import tempfile
import threading
import time
from http.server import ThreadingHTTPServer
from pathlib import Path
from typing import Callable

HERE = Path(__file__).resolve().parent
MESSAGE = "Plan a three-day trip to Juneau"
ANSWER = "You said: " + MESSAGE


class Failed(Exception):
    pass


def main() -> int:
    ap = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument("--core", required=True, help="the Toskar Core daemon to run")
    ap.add_argument("--sdks", required=True, help="a toskar-sdks checkout, with typescript/ built")
    ap.add_argument("--python", default=sys.executable, help="a Python with the toskar package installed")
    ap.add_argument("--go-sdk-from-checkout", action="store_true", help="build the Go examples with the checkout's SDK")
    args = ap.parse_args()

    # The examples run from other folders, so paths are made absolute.
    sdks = Path(args.sdks).resolve()
    python = str(Path(args.python).absolute()) if os.sep in args.python else args.python
    sys.path.insert(0, str(sdks / "conformance"))
    import run  # toskar-sdks' scripted model and throwaway Core

    with tempfile.TemporaryDirectory(prefix="toskar-examples-") as tmp_name:
        tmp = Path(tmp_name)
        bin_dir = build_go(tmp, sdks / "go" if args.go_sdk_from_checkout else None)
        js = link_javascript(tmp, sdks / "typescript")

        model = ThreadingHTTPServer(("127.0.0.1", 0), run.ScriptedModel)
        threading.Thread(target=model.serve_forever, daemon=True).start()
        core = run.Core(args.core, tmp)
        failed = 0
        try:
            core.api("PUT", "/api/v1/external-server", {"base_url": f"http://127.0.0.1:{model.server_address[1]}/v1"})
            # The default profile answers with the scripted model, as with a model installed.
            profile = core.api("GET", "/api/v1/profiles/general-assistant")
            for role in profile["roles"]:
                role["model_id"] = "ext:echo"
            core.api("PATCH", "/api/v1/profiles/general-assistant", profile)

            env = {k: v for k, v in os.environ.items() if not k.startswith("TOSKAR_")}
            env["TOSKAR_URL"] = core.url
            runner = Runner(env)
            languages = {
                "python": [python, str(HERE / "python" / "{}.py")],
                "javascript": ["node", str(js / "{}.mjs")],
                "go": [str(bin_dir / "{}")],
            }
            cases: list[tuple[str, Callable[[], None]]] = [
                ("curl models", lambda: curl_models(runner)),
                ("curl chat", lambda: curl_chat(runner)),
            ]
            for lang, cmd in languages.items():
                # Pairing saves device.json where it runs.
                cwd = tmp / f"{lang}-work"
                cwd.mkdir()
                cases += [
                    (f"{lang} chat", lambda cmd=cmd: chat(runner, cmd)),
                    (f"{lang} events", lambda cmd=cmd: events(runner, core, cmd)),
                    (f"{lang} pair", lambda cmd=cmd, cwd=cwd: pair(runner, core, cmd, cwd)),
                ]
            for name, case in cases:
                started = time.time()
                try:
                    case()
                    print(f"ok   {name:18} {time.time() - started:5.1f}s", flush=True)
                except Exception as e:
                    failed += 1
                    print(f"FAIL {name:18} {e if isinstance(e, Failed) else repr(e)}", flush=True)
        finally:
            core.stop()
            model.shutdown()
        if failed:
            print(f"Core's log ends:\n{(tmp / 'core.log').read_text()[-3000:]}", file=sys.stderr)
    print(f"{failed} failed" if failed else "every example works")
    return 1 if failed else 0


def build_go(tmp: Path, sdk: Path | None) -> Path:
    """Builds the Go examples, so stopping one stops it (go run leaves its
    program running)."""
    src = tmp / "go-src"
    shutil.copytree(HERE / "go", src)
    if sdk is not None:
        subprocess.run(["go", "mod", "edit", f"-replace=github.com/yeixio/toskar-sdks/go={sdk}"], cwd=src, check=True)
        subprocess.run(["go", "mod", "tidy"], cwd=src, check=True)
    out = tmp / "go-bin"
    for example in ("chat", "events", "pair"):
        subprocess.run(["go", "build", "-o", str(out / example), f"./{example}"], cwd=src, check=True)
    return out


def link_javascript(tmp: Path, sdk: Path) -> Path:
    """The JavaScript examples beside the SDK built in the checkout, as npm link leaves them."""
    js = tmp / "javascript"
    shutil.copytree(HERE / "javascript", js, ignore=shutil.ignore_patterns("node_modules", "device.json"))
    (js / "node_modules").mkdir()
    (js / "node_modules" / "toskar").symlink_to(sdk.resolve(), target_is_directory=True)
    return js


class Runner:
    def __init__(self, env: dict[str, str]) -> None:
        self.env = env

    def __call__(self, cmd: list[str], *, cwd: Path = HERE, stdin: str | None = None, env: dict[str, str] | None = None,
                 code: int = 0) -> str:
        try:
            r = subprocess.run(cmd, cwd=cwd, env={**self.env, **(env or {})}, input=stdin, capture_output=True,
                               text=True, timeout=60)
        except subprocess.TimeoutExpired:
            raise Failed(f"{' '.join(cmd)} didn't finish in 60s") from None
        out = r.stdout + r.stderr
        if r.returncode != code:
            raise Failed(f"{' '.join(cmd)} exited {r.returncode}, not {code}:\n{out[-1500:]}")
        return out


def expect(ok: bool, what: str, out: str) -> None:
    if not ok:
        raise Failed(f"{what}; it printed:\n{out[-1500:]}")


def fill(cmd: list[str], example: str) -> list[str]:
    return [part.replace("{}", example) for part in cmd]


def curl_models(run: Runner) -> None:
    out = run(["sh", "curl/models.sh"])
    expect('"auto"' in out and "profile:general-assistant" in out, "auto and the profiles are listed", out)


def curl_chat(run: Runner) -> None:
    # auto never picks an external model, so ask for the profile.
    out = run(["sh", "curl/chat.sh", MESSAGE], env={"TOSKAR_MODEL": "profile:general-assistant"})
    pieces = "".join(re.findall(r'"content":"([^"]*)"', out))
    expect(pieces == ANSWER and "data: [DONE]" in out, "the answer streams, ending with [DONE]", out)


def chat(run: Runner, cmd: list[str]) -> None:
    out = run(fill(cmd, "chat") + [MESSAGE])
    expect(ANSWER in out and re.search(r"\d+ tokens", out) is not None, "the streamed answer, then its token count", out)


def events(run: Runner, core, cmd: list[str]) -> None:  # type: ignore[no-untyped-def]
    with tempfile.TemporaryFile("w+") as log:
        p = subprocess.Popen(fill(cmd, "events"), cwd=HERE, env=run.env, stdout=log, stderr=subprocess.STDOUT, text=True)
        try:
            # Ask until the example shows a chat: it may still be starting,
            # and a chat before it has subscribed goes unseen.
            deadline = time.time() + 30
            asked = 0.0
            while time.time() < deadline:
                if time.time() - asked > 3:
                    core.api("POST", "/api/v1/chat", {"message": "ping"})
                    asked = time.time()
                log.seek(0)
                if "chat.complete" in log.read():
                    break
                time.sleep(0.3)
        finally:
            p.terminate()
            p.wait(10)
        log.seek(0)
        out = log.read()
    expect("chat.token" in out and "chat.complete" in out and "You said: ping" in out, "the chat's events, as they happen", out)


def pair(run: Runner, core, cmd: list[str], cwd: Path) -> None:  # type: ignore[no-untyped-def]
    address = core.url.removeprefix("http://")
    started = core.api("POST", "/api/v1/devices/pairing", {})
    out = run(fill(cmd, "pair") + [address, started["code"]], cwd=cwd, stdin="y\n")
    expect(started["tls_short"] in out, "the short pin the computer shows", out)
    expect("Paired." in out and re.search(r"Reached Toskar \S+ home\.", out) is not None, "paired, then reached at home", out)
    out = run(fill(cmd, "pair"), cwd=cwd)
    expect(re.search(r"Reached Toskar \S+ home\.", out) is not None, "reached again from the saved device", out)
    (cwd / "device.json").unlink()
    started = core.api("POST", "/api/v1/devices/pairing", {})
    out = run(fill(cmd, "pair") + [address, started["code"]], cwd=cwd, stdin="n\n", code=1)
    expect("Not paired" in out and not (cwd / "device.json").exists(), "a short pin not confirmed pairs nothing", out)


if __name__ == "__main__":
    sys.exit(main())
