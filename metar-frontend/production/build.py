#!/usr/bin/env python3
"""Build the production METAR shell with no third-party build dependency."""
from __future__ import annotations

import argparse
import json
import shutil
from pathlib import Path

ROOT = Path(__file__).resolve().parent
PROTOTYPE_ROOT = ROOT.parent
BANNED_PRODUCTION_MARKERS = (
    "metar-prototype",
    "SEED_POSTS",
    "CANDIDATES",
    "demo-content-",
    "演示身份",
    "未发送到线上",
    "X-Pulse-Signature",
    "New-Api-User",
    "PULSE_REWARD_RANDOM_SECRET",
)


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser()
    parser.add_argument("--output", type=Path, default=ROOT / "dist")
    parser.add_argument("--config", type=Path, default=ROOT / "config.production.json")
    return parser.parse_args()


def load_config(path: Path) -> dict:
    config = json.loads(path.read_text(encoding="utf-8"))
    required = ("siteName", "answerApiBase", "answerLoginPath", "answerRegisterPath", "answerAskPath", "answerBindingPath", "blogBasePath")
    missing = [key for key in required if not config.get(key)]
    if missing:
        raise ValueError(f"production config missing: {', '.join(missing)}")
    for key in ("answerApiBase", "answerLoginPath", "answerRegisterPath", "answerPasswordResetPath", "answerAskPath", "answerSettingsPath", "answerBindingPath", "blogBasePath"):
        value = config.get(key, "")
        if value and (not value.startswith("/") or value.startswith("//") or "://" in value):
            raise ValueError(f"{key} must be a same-origin absolute path")
    for key in ("consoleUrl", "docsUrl", "statusUrl"):
        value = config.get(key, "")
        if value and not value.startswith("https://"):
            raise ValueError(f"{key} must be empty or HTTPS")
    return config


def build(output: Path, config_path: Path) -> None:
    config = load_config(config_path)
    sources = {
        "index": (ROOT / "src/index.html").read_text(encoding="utf-8"),
        "adapters": (ROOT / "src/adapters.js").read_text(encoding="utf-8"),
        "app": (ROOT / "src/app.js").read_text(encoding="utf-8"),
        "favicon": (ROOT / "src/favicon.svg").read_text(encoding="utf-8"),
    }
    production_text = "\n".join(sources.values())
    for marker in BANNED_PRODUCTION_MARKERS:
        if marker.lower() in production_text.lower():
            raise ValueError(f"production source contains banned marker: {marker}")

    if output.exists():
        shutil.rmtree(output)
    assets = output / "assets"
    assets.mkdir(parents=True)

    base_css = (PROTOTYPE_ROOT / "src/styles.css").read_text(encoding="utf-8")
    production_css = (ROOT / "src/styles.css").read_text(encoding="utf-8")
    (assets / "app.css").write_text(f"{base_css}\n{production_css}\n", encoding="utf-8")
    (assets / "adapters.js").write_text(sources["adapters"], encoding="utf-8")
    (assets / "app.js").write_text(sources["app"], encoding="utf-8")
    (assets / "favicon.svg").write_text(sources["favicon"], encoding="utf-8")
    (output / "index.html").write_text(sources["index"], encoding="utf-8")
    runtime = "window.__METAR_RUNTIME_CONFIG__ = Object.freeze(" + json.dumps(config, ensure_ascii=False, separators=(",", ":")) + ");\n"
    (output / "runtime-config.js").write_text(runtime, encoding="utf-8")
    (output / "BUILD_INFO.json").write_text(json.dumps({"kind": "production", "mockData": False, "config": config_path.name}, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")
    print(f"Built production METAR frontend: {output}")


if __name__ == "__main__":
    args = parse_args()
    build(args.output.resolve(), args.config.resolve())
