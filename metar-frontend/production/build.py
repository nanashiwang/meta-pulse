#!/usr/bin/env python3
"""Build the production METAR shell with no third-party build dependency."""
from __future__ import annotations

import argparse
import json
import shutil
from pathlib import Path

from seo import build_seo

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
        "seo": (ROOT / "src/seo.js").read_text(encoding="utf-8"),
        "i18n": (ROOT / "src/i18n.js").read_text(encoding="utf-8"),
        "adapters": (ROOT / "src/adapters.js").read_text(encoding="utf-8"),
        "avatars": (ROOT / "src/avatars.js").read_text(encoding="utf-8"),
        "admin-periods": (ROOT / "src/admin-periods.js").read_text(encoding="utf-8"),
        "admin-pulse": (ROOT / "src/admin-pulse.js").read_text(encoding="utf-8"),
        "router": (ROOT / "src/router.js").read_text(encoding="utf-8"),
        "route-policy": (ROOT / "src/route-policy.js").read_text(encoding="utf-8"),
        "theme": (ROOT / "src/theme.js").read_text(encoding="utf-8"),
        "growth-presentation": (ROOT.parents[1] / "services/forum-plugin/user-center-pulse/growth-presentation.js").read_text(encoding="utf-8"),
        "growth": (ROOT / "src/growth.js").read_text(encoding="utf-8"),
        "app": (ROOT / "src/app.js").read_text(encoding="utf-8"),
        "pulse-core": (ROOT / "src/pulse-core.js").read_text(encoding="utf-8"),
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
    tokens = (PROTOTYPE_ROOT / "shared/theme-tokens.css").read_text(encoding="utf-8")
    (assets / "app.css").write_text(f"{base_css}\n{tokens}\n{production_css}\n", encoding="utf-8")
    (assets / "seo.js").write_text(sources["seo"], encoding="utf-8")
    (assets / "i18n.js").write_text(sources["i18n"], encoding="utf-8")
    (assets / "adapters.js").write_text(sources["adapters"], encoding="utf-8")
    (assets / "avatars.js").write_text(sources["avatars"], encoding="utf-8")
    (assets / "admin-periods.js").write_text(sources["admin-periods"], encoding="utf-8")
    (assets / "admin-pulse.js").write_text(sources["admin-pulse"], encoding="utf-8")
    (assets / "router.js").write_text(sources["router"], encoding="utf-8")
    (assets / "route-policy.js").write_text(sources["route-policy"], encoding="utf-8")
    (assets / "theme.js").write_text(sources["theme"], encoding="utf-8")
    (assets / "growth-presentation.js").write_text(sources["growth-presentation"], encoding="utf-8")
    (assets / "growth.js").write_text(sources["growth"], encoding="utf-8")
    (assets / "app.js").write_text(sources["app"], encoding="utf-8")
    (assets / "pulse-core.js").write_text(sources["pulse-core"], encoding="utf-8")
    shutil.copyfile(ROOT / "src/pulse-core.css", assets / "pulse-core.css")
    (assets / "favicon.svg").write_text(sources["favicon"], encoding="utf-8")
    build_seo(output, sources["index"])
    runtime = "window.__METAR_RUNTIME_CONFIG__ = Object.freeze(" + json.dumps(config, ensure_ascii=False, separators=(",", ":")) + ");\n"
    (output / "runtime-config.js").write_text(runtime, encoding="utf-8")
    (output / "BUILD_INFO.json").write_text(json.dumps({"kind": "production", "mockData": False, "config": config_path.name}, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")
    # These are public web assets. A private deployment umask must not make
    # them unreadable to nginx, which runs under a different UID.
    output.chmod(0o755)
    assets.chmod(0o755)
    for path in output.rglob("*"):
        path.chmod(0o755 if path.is_dir() else 0o644)
    print(f"Built production METAR frontend: {output}")


if __name__ == "__main__":
    args = parse_args()
    build(args.output.resolve(), args.config.resolve())
