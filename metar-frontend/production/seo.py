"""Public HTML and discovery files; never reads sessions or production data."""
import html
import json
import re
from pathlib import Path
from xml.etree import ElementTree as ET

ORIGIN = "https://metar.uk"
PAGES = json.loads((Path(__file__).parent / "src/seo-pages.json").read_text())


def build_seo(output, shell, site_name="METAR"):
    source = Path(__file__).parent / "src"
    view_template = (source / "loading-view.html").read_text()
    chrome = (source / "loading-shell.html").read_text().replace("__METAR_BRAND__", html.escape(site_name.lower()))
    shell = shell.replace("<!-- METAR_LOADING_TEMPLATE -->", '<template id="metar-loading-template">' + view_template + '</template>')

    def initial_document(document, route="", body=""):
        titles = {"/": ("最新话题", "Latest topics"), "/latest": ("最新话题", "Latest topics"),
                  "/topics": ("全部标签", "All tags"), "/knowledge": ("知识库", "Knowledge"),
                  "/support": ("帮助中心", "Help center"), "/guidelines": ("社区规范", "Guidelines")}
        title, english = titles.get(route, ("正在加载…", "Loading…"))
        layout = "feed" if route in ("/", "/latest") else "grid" if route in ("/topics", "/knowledge") else "panel"
        view = (view_template.replace("__METAR_LOADING_LAYOUT__", layout)
                .replace("__METAR_LOADING_TITLE__", html.escape(title))
                .replace("__METAR_LOADING_LABEL__", "正在读取社区实时数据…")
                .replace("data-loading-title", 'data-loading-title data-boot-en="' + english + '"'))
        fallback = '<noscript><section class="page-inner seo-fallback">' + body + '</section></noscript>' if body else ''
        initial = chrome.replace("__METAR_LOADING_VIEW__", view).replace("__METAR_SEO_FALLBACK__", fallback)
        return document.replace('<div id="app" aria-busy="true"></div>', '<div id="app" aria-busy="true">' + initial + '</div>')

    (output / "shell.html").write_text(initial_document(shell))
    directory = output / "seo"
    directory.mkdir()
    for route, page in PAGES.items():
        document = re.sub(r"<title>.*?</title>", "<title>" + html.escape(page["title"]) + "</title>", shell)
        description = html.escape(page["description"], quote=True)
        document = re.sub(r'<meta name="description" content="[^"]*">', '<meta name="description" content="' + description + '">', document)
        document = document.replace('</head>', '<link rel="canonical" href="' + ORIGIN + route + '">\n<meta property="og:url" content="' + ORIGIN + route + '">\n<meta property="og:title" content="' + html.escape(page["title"], quote=True) + '">\n<meta property="og:description" content="' + description + '">\n<meta property="og:type" content="website">\n</head>')
        document = initial_document(document, route, page["body"])
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
