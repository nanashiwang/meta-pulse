"""Public HTML and discovery files; never reads sessions or production data."""
import html
import json
import re
from pathlib import Path
from xml.etree import ElementTree as ET

ORIGIN = "https://metar.uk"
PAGES = json.loads((Path(__file__).parent / "src/seo-pages.json").read_text())


def build_seo(output, shell):
    (output / "shell.html").write_text(shell)
    directory = output / "seo"
    directory.mkdir()
    for route, page in PAGES.items():
        document = re.sub(r"<title>.*?</title>", "<title>" + html.escape(page["title"]) + "</title>", shell)
        description = html.escape(page["description"], quote=True)
        document = re.sub(r'<meta name="description" content="[^"]*">', '<meta name="description" content="' + description + '">', document)
        document = document.replace('</head>', '<link rel="canonical" href="' + ORIGIN + route + '">\n<meta property="og:url" content="' + ORIGIN + route + '">\n<meta property="og:title" content="' + html.escape(page["title"], quote=True) + '">\n<meta property="og:description" content="' + description + '">\n<meta property="og:type" content="website">\n</head>')
        document = document.replace('<div id="app" aria-busy="true"></div>', '<div id="app" aria-busy="true"><main id="main" class="page"><div class="page-inner">' + page["body"] + '</div></main></div>')
        if route == "/latest":
            document = re.sub(r'<link rel="canonical"[^>]*>', "", document)
        target = output / "index.html" if route == "/" else directory / (route.strip("/") + ".html")
        target.write_text(document)
    ET.register_namespace("", "http://www.sitemaps.org/schemas/sitemap/0.9")
    root = ET.Element("{http://www.sitemaps.org/schemas/sitemap/0.9}urlset")
    for route in [*PAGES, "/questions", "/tags"]:
        entry = ET.SubElement(root, "url")
        ET.SubElement(entry, "loc").text = ORIGIN + route
    ET.ElementTree(root).write(output / "sitemap-site.xml", encoding="utf-8", xml_declaration=True)
    (output / "robots.txt").write_text((Path(__file__).parent / "src/robots.txt").read_text())
