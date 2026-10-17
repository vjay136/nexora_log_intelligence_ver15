from __future__ import annotations

from pathlib import Path
import math
import textwrap

from reportlab.graphics import renderSVG
from reportlab.graphics.shapes import Circle, Drawing, Line, Path as RLPath, Polygon, Rect, String
from reportlab.lib.colors import HexColor
from reportlab.pdfbase import pdfmetrics
from reportlab.pdfbase.ttfonts import TTFont


WIDTH = 7680
HEIGHT = 4320

ROOT = Path(__file__).resolve().parent
SVG_OUT = ROOT / "NLI_V14_COMPLETE_FLOW_AND_ARCHITECTURE.svg"
PNG_OUT = ROOT / "NLI_V14_COMPLETE_FLOW_AND_ARCHITECTURE_8K.png"


FONT_REGULAR = r"C:\Windows\Fonts\segoeui.ttf"
FONT_SEMIBOLD = r"C:\Windows\Fonts\seguisb.ttf"
FONT_BOLD = r"C:\Windows\Fonts\segoeuib.ttf"
pdfmetrics.registerFont(TTFont("NLI-Regular", FONT_REGULAR))
pdfmetrics.registerFont(TTFont("NLI-Semibold", FONT_SEMIBOLD))
pdfmetrics.registerFont(TTFont("NLI-Bold", FONT_BOLD))


C = {
    "bg": HexColor("#07111F"),
    "panel": HexColor("#0C1A2D"),
    "panel2": HexColor("#10243B"),
    "stroke": HexColor("#29415F"),
    "muted": HexColor("#91A7C2"),
    "text": HexColor("#F4F8FF"),
    "cyan": HexColor("#29D3F2"),
    "blue": HexColor("#4E8CFF"),
    "violet": HexColor("#A879FF"),
    "green": HexColor("#37D99B"),
    "amber": HexColor("#FFBF4A"),
    "red": HexColor("#FF657A"),
    "pink": HexColor("#F371D0"),
    "slate": HexColor("#182B45"),
    "white": HexColor("#FFFFFF"),
}


d = Drawing(WIDTH, HEIGHT)
d.add(Rect(0, 0, WIDTH, HEIGHT, fillColor=C["bg"], strokeColor=None))


def by(top_y: float, height: float = 0) -> float:
    return HEIGHT - top_y - height


def add_text(x, top_y, value, size=48, color=None, font="NLI-Regular", anchor="start"):
    d.add(String(x, HEIGHT - top_y - size * 0.78, value, fontName=font,
                 fontSize=size, fillColor=color or C["text"], textAnchor=anchor))


def wrapped_lines(text: str, max_chars: int) -> list[str]:
    lines: list[str] = []
    for para in text.split("\n"):
        if not para:
            lines.append("")
            continue
        lines.extend(textwrap.wrap(para, width=max_chars, break_long_words=False, break_on_hyphens=False))
    return lines


def paragraph(x, top_y, text, width, size=36, leading=None, color=None,
              font="NLI-Regular", max_lines=None, bullet=False):
    leading = leading or int(size * 1.32)
    avg = max(1, int(width / (size * 0.56)))
    lines = wrapped_lines(text, avg)
    if max_lines:
        lines = lines[:max_lines]
    yy = top_y
    for line in lines:
        if bullet and line:
            d.add(Circle(x + size * 0.18, HEIGHT - yy - size * 0.46, size * 0.09,
                         fillColor=color or C["muted"], strokeColor=None))
            tx = x + size * 0.62
        else:
            tx = x
        add_text(tx, yy, line, size=size, color=color or C["muted"], font=font)
        yy += leading
    return yy


def rounded_rect(x, top_y, w, h, fill, stroke=None, radius=28, stroke_width=3):
    d.add(Rect(x + 10, by(top_y, h) - 10, w, h, rx=radius, ry=radius,
               fillColor=HexColor("#040A12"), strokeColor=None))
    d.add(Rect(x, by(top_y, h), w, h, rx=radius, ry=radius,
               fillColor=fill, strokeColor=stroke or C["stroke"], strokeWidth=stroke_width))


def card(x, top_y, w, h, title, body, accent, tag=None, title_size=46,
         body_size=32, fill=None, icon=None):
    rounded_rect(x, top_y, w, h, fill or C["panel2"], accent, radius=26, stroke_width=3)
    d.add(Rect(x, by(top_y, h), 14, h, rx=7, ry=7, fillColor=accent, strokeColor=None))
    tx = x + 42
    if icon:
        icon(x + 62, top_y + 60, 52, accent)
        tx = x + 116
    add_text(tx, top_y + 28, title, size=title_size, color=C["text"], font="NLI-Semibold")
    if tag:
        badge_w = max(125, len(tag) * 22 + 54)
        d.add(Rect(x + w - badge_w - 26, by(top_y + 25, 54), badge_w, 54, rx=27, ry=27,
                   fillColor=accent, strokeColor=None))
        add_text(x + w - badge_w / 2 - 26, top_y + 36, tag, size=27, color=C["bg"],
                 font="NLI-Bold", anchor="middle")
    paragraph(x + 42, top_y + 100, body, w - 84, size=body_size, leading=int(body_size * 1.35),
              color=C["muted"], max_lines=max(1, int((h - 110) / (body_size * 1.35))))


def section(x, top_y, w, h, label, subtitle, accent):
    rounded_rect(x, top_y, w, h, C["panel"], C["stroke"], radius=38, stroke_width=4)
    d.add(Rect(x, by(top_y, 118), w, 118, rx=38, ry=38, fillColor=accent, strokeColor=None))
    d.add(Rect(x, by(top_y + 65, 53), w, 53, fillColor=accent, strokeColor=None))
    add_text(x + 42, top_y + 24, label, size=56, color=C["bg"], font="NLI-Bold")
    add_text(x + w - 42, top_y + 40, subtitle, size=29, color=C["bg"],
             font="NLI-Semibold", anchor="end")


def arrow(points, color, width=12, dashed=False, label=None, label_xy=None, label_color=None):
    p = RLPath()
    p.moveTo(points[0][0], HEIGHT - points[0][1])
    for px, py in points[1:]:
        p.lineTo(px, HEIGHT - py)
    p.strokeColor = color
    p.strokeWidth = width
    p.fillColor = None
    if dashed:
        p.strokeDashArray = [30, 22]
    d.add(p)
    x1, y1 = points[-2]
    x2, y2 = points[-1]
    angle = math.atan2(y2 - y1, x2 - x1)
    length = 34 + width
    wing = 20 + width * 0.35
    bx = x2 - length * math.cos(angle)
    byy = y2 - length * math.sin(angle)
    left = (bx + wing * math.cos(angle + math.pi / 2), byy + wing * math.sin(angle + math.pi / 2))
    right = (bx + wing * math.cos(angle - math.pi / 2), byy + wing * math.sin(angle - math.pi / 2))
    d.add(Polygon([x2, HEIGHT - y2, left[0], HEIGHT - left[1], right[0], HEIGHT - right[1]],
                  fillColor=color, strokeColor=color))
    if label and label_xy:
        lw = len(label) * 19 + 64
        lx, ly = label_xy
        d.add(Rect(lx - lw / 2, by(ly, 54), lw, 54, rx=27, ry=27,
                   fillColor=C["bg"], strokeColor=color, strokeWidth=2))
        add_text(lx, ly + 10, label, size=28, color=label_color or color,
                 font="NLI-Semibold", anchor="middle")


def icon_file(cx, top_y, size, color):
    x, y = cx - size * .38, by(top_y + size * .46, size * .82)
    d.add(Rect(x, y, size * .68, size * .82, rx=5, ry=5, fillColor=None, strokeColor=color, strokeWidth=5))
    for i in range(3):
        d.add(Line(x + size * .12, y + size * (.58 - i * .16), x + size * .53,
                   y + size * (.58 - i * .16), strokeColor=color, strokeWidth=4))


def icon_shield(cx, top_y, size, color):
    cy = HEIGHT - top_y
    pts = [cx, cy + size * .45, cx + size * .42, cy + size * .28,
           cx + size * .32, cy - size * .22, cx, cy - size * .48,
           cx - size * .32, cy - size * .22, cx - size * .42, cy + size * .28]
    d.add(Polygon(pts, fillColor=None, strokeColor=color, strokeWidth=6))
    d.add(Line(cx - size * .18, cy, cx - size * .02, cy - size * .16, strokeColor=color, strokeWidth=6))
    d.add(Line(cx - size * .02, cy - size * .16, cx + size * .23, cy + size * .17, strokeColor=color, strokeWidth=6))


def icon_brain(cx, top_y, size, color):
    cy = HEIGHT - top_y
    for ox, oy, r in [(-.22, .15, .24), (.12, .2, .27), (-.15, -.14, .29), (.2, -.12, .25)]:
        d.add(Circle(cx + ox * size, cy + oy * size, r * size, fillColor=None, strokeColor=color, strokeWidth=5))
    d.add(Line(cx, cy + size * .4, cx, cy - size * .42, strokeColor=color, strokeWidth=4))


def icon_db(cx, top_y, size, color):
    cy = HEIGHT - top_y
    w, h = size * .78, size * .72
    d.add(Rect(cx - w / 2, cy - h / 2, w, h, fillColor=None, strokeColor=color, strokeWidth=5))
    d.add(Circle(cx, cy + h / 2, w / 2, fillColor=C["panel2"], strokeColor=color, strokeWidth=5))
    d.add(Circle(cx, cy - h / 2, w / 2, fillColor=None, strokeColor=color, strokeWidth=5))


def icon_browser(cx, top_y, size, color):
    cy = HEIGHT - top_y
    d.add(Rect(cx - size * .48, cy - size * .36, size * .96, size * .72,
               rx=8, ry=8, fillColor=None, strokeColor=color, strokeWidth=5))
    d.add(Line(cx - size * .47, cy + size * .16, cx + size * .47, cy + size * .16,
               strokeColor=color, strokeWidth=5))
    for i in range(3):
        d.add(Circle(cx - size * (.32 - i * .14), cy + size * .26, size * .035,
                     fillColor=color, strokeColor=None))


def icon_bell(cx, top_y, size, color):
    cy = HEIGHT - top_y
    p = RLPath()
    p.moveTo(cx - size * .34, cy - size * .18)
    p.curveTo(cx - size * .28, cy + size * .06, cx - size * .25, cy + size * .34, cx, cy + size * .38)
    p.curveTo(cx + size * .25, cy + size * .34, cx + size * .28, cy + size * .06, cx + size * .34, cy - size * .18)
    p.lineTo(cx + size * .42, cy - size * .28)
    p.lineTo(cx - size * .42, cy - size * .28)
    p.closePath()
    p.fillColor = None
    p.strokeColor = color
    p.strokeWidth = 5
    d.add(p)
    d.add(Circle(cx, cy - size * .39, size * .08, fillColor=color, strokeColor=None))


# Subtle background grid.
for gx in range(80, WIDTH, 160):
    for gy in range(80, HEIGHT, 160):
        d.add(Circle(gx, HEIGHT - gy, 2.4, fillColor=HexColor("#18304C"), strokeColor=None))


# Header and brand mark.
d.add(Polygon([110, by(116), 190, by(76), 270, by(116), 252, by(216),
               190, by(268), 128, by(216)], fillColor=C["cyan"], strokeColor=C["cyan"]))
d.add(Polygon([142, by(132), 190, by(108), 238, by(132), 226, by(198),
               190, by(228), 154, by(198)], fillColor=C["bg"], strokeColor=None))
add_text(190, 151, "NLI", size=41, color=C["cyan"], font="NLI-Bold", anchor="middle")
add_text(315, 74, "Nexora Log Intelligence Ver14", size=132, color=C["text"], font="NLI-Bold")
add_text(320, 222, "COMPLETE FLOW + DEPLOYMENT ARCHITECTURE", size=44, color=C["cyan"], font="NLI-Semibold")
add_text(320, 300, "Infinite Logs  →  One Engine (NLI)  →  Intelligent Parsing  →  Multiple Endpoints  →  Fully Secured",
         size=45, color=C["muted"], font="NLI-Regular")

# Legend.
legend_x = 5200
legend_y = 88
rounded_rect(legend_x, legend_y, 2350, 270, C["panel"], C["stroke"], radius=32, stroke_width=3)
add_text(legend_x + 36, legend_y + 24, "FLOW LEGEND", size=39, color=C["text"], font="NLI-Bold")
legend = [(C["cyan"], "LOG / DATA"), (C["violet"], "CONTROL / LIVE SYNC"),
          (C["green"], "APPROVED PARSER"), (C["amber"], "HUMAN / SECURITY GATE")]
for i, (col, label) in enumerate(legend):
    lx = legend_x + 40 + (i % 2) * 1140
    ly = legend_y + 92 + (i // 2) * 78
    d.add(Line(lx, HEIGHT - ly - 20, lx + 110, HEIGHT - ly - 20, strokeColor=col, strokeWidth=12))
    d.add(Polygon([lx + 110, HEIGHT - ly - 20, lx + 82, HEIGHT - ly - 3, lx + 82, HEIGHT - ly - 37],
                  fillColor=col, strokeColor=col))
    add_text(lx + 138, ly, label, size=31, color=C["muted"], font="NLI-Semibold")


# Major zones.
section(120, 500, 2160, 2935, "1  EDGE + MANAGED CLIENT", "Rocky Linux 9 / 10 • client RPM", C["cyan"])
section(2440, 500, 2860, 2935, "2  NLI CONTROL PLANE", "Controller RPM • TLS • NGINX / Apache / tunnel edge • 12382", C["violet"])
section(5460, 500, 2100, 2935, "3  EXPERIENCE + CLOUD", "WebView • Vercel / Cloudflare • Supabase", C["green"])


# Zone 1: sources.
add_text(172, 655, "LOG SOURCES", size=39, color=C["cyan"], font="NLI-Bold")
source_cards = [
    ("MAIL", "IMAP • POP • MTA"), ("SMTP", "IN • OUT"),
    ("WEB", "WebTop • Webmail"), ("SECURITY", "PSS/PABD • MSS/CAPD"),
    ("CUSTOM", "Syslog • JSON • logfmt • mixed KV")
]
for i, (title, body) in enumerate(source_cards):
    sx = 170 + (i % 3) * 680
    sy = 720 + (i // 3) * 230
    card(sx, sy, 610, 185, title, body, C["cyan"], title_size=36, body_size=28, icon=icon_file)

card(170, 1195, 2010, 190, "Configured Input Paths + Sample Log",
     "The RPM creates ten family inputs (auto, IMAP, POP, WebTop, MTA, SMTP in/out, PABD, CAPD, PSS). Hourly normalised files are optional; hourly exact-raw quarantine files are mandatory.",
     C["blue"], tag="INPUT", title_size=43, body_size=30, icon=icon_file)

add_text(172, 1435, "CLIENT RUNTIME", size=39, color=C["cyan"], font="NLI-Bold")
card(170, 1495, 960, 235, "Install + Auto-Enroll",
     "Validates RPM/SHA-256, trusts corporate or generated private CA TLS, consumes one one-use bootstrap token, receives a protected per-node token, then deletes the bootstrap copy.",
     C["amber"], tag="SECURE", title_size=39, body_size=27, icon=icon_shield)
card(1220, 1495, 960, 235, "nli-client-control",
     "Registers the node, sends heartbeat + parser inventory, leases commands, and applies hash-checked parser/config rollouts with rollback.",
     C["violet"], tag="CONTROL", title_size=39, body_size=27)

card(170, 1780, 2010, 250, "nli-agent — Reliable Ingestion",
     "Tails large files through bounded parallel source workers, groups multiline records, preserves per-source order, applies backpressure, and waits at EOF for production appends.",
     C["cyan"], tag="DATA PATH", title_size=44, body_size=31)

# Parsing pipeline container.
rounded_rect(170, 2080, 2010, 770, C["slate"], C["cyan"], radius=30, stroke_width=4)
add_text(210, 2115, "ONE ENGINE: DETERMINISTIC PARSER PIPELINE", size=40, color=C["cyan"], font="NLI-Bold")
pipeline = [
    ("1. Detect + Filter", "Exact service/event/method/host gates + reviewed parser tier", C["blue"]),
    ("2. Extract", "Static, KV, regex, template, JSON, logfmt + Grok", C["cyan"]),
    ("3. Enrich", "IPv4/IPv6, GeoIP MMDB, email/network buckets, URL + Subject", C["violet"]),
    ("4. Validate", "Source parity, evidence, spans, coverage + confidence", C["green"]),
]
for i, (title, body, col) in enumerate(pipeline):
    px = 210 + (i % 2) * 965
    py = 2200 + (i // 2) * 265
    card(px, py, 900, 220, title, body, col, title_size=37, body_size=29)
    if i in (0, 2):
        arrow([(px + 900, py + 110), (px + 955, py + 110)], col, width=8)

card(170, 2900, 960, 235, "Normal / Warning Route",
     "Deliver validated EventPayload records to Loki, ClickHouse, controller and/or optional hourly local JSONL.", C["green"], tag="SUCCESS",
     title_size=38, body_size=30)
card(1220, 2900, 960, 235, "Unknown / Quarantine",
     "Keep failures locally with diagnostics for replay, Assurance investigation, and parser improvement.", C["red"], tag="REPLAY",
     title_size=38, body_size=29)

add_text(172, 3190, "OUTPUTS", size=36, color=C["cyan"], font="NLI-Bold")
outputs = [("NLI CONTROLLER", C["violet"]), ("LOKI HTTP", C["pink"]),
           ("CLICKHOUSE", C["amber"]), ("LOCAL JSONL", C["green"])]
for i, (name, col) in enumerate(outputs):
    ox = 170 + i * 505
    d.add(Rect(ox, by(3245, 120), 455, 120, rx=24, ry=24, fillColor=C["panel2"], strokeColor=col, strokeWidth=3))
    add_text(ox + 227, 3280, name, size=28, color=col, font="NLI-Bold", anchor="middle")


# Zone 2: controller layers.
card(2490, 655, 2760, 260, "TLS Gateway + Authentication + RBAC",
     "A matching trusted server certificate is auto-discovered; otherwise a unique private CA is generated. Sessions, RBAC, limits, audit context and node tokens protect every API path.",
     C["amber"], tag="TRUST BOUNDARY", title_size=47, body_size=31, icon=icon_shield)

add_text(2490, 970, "nli-controller-agent", size=42, color=C["violet"], font="NLI-Bold")
controller_modules = [
    ("Fleet + Heartbeats", "Node status, resources, paths, outputs, parser inventory and command leases.", C["cyan"]),
    ("Parser Fleet Sync", "Regex, patterns, logic, layouts, templates, mappings and keys: desired vs applied revision/hash on every node.", C["green"]),
    ("Metrics + Telemetry", "One bounded SSE stream + snapshot: input/output lines per second, bytes, queues, phase timers and errors.", C["blue"]),
    ("Users, RBAC + Audit", "Controller-owned login accounts, roles, audit trail and safe metadata mirror.", C["amber"]),
    ("Input + Output Engines", "Version paths and destinations; approve targeted config-patch rollouts without overwriting unrelated node settings.", C["pink"]),
    ("Streams + Performance", "Live read, parse, normalized, quarantine, delivery, EOF/tailing, threshold and elapsed-time views.", C["red"]),
]
for i, (title, body, col) in enumerate(controller_modules):
    mx = 2490 + (i % 3) * 900
    my = 1030 + (i // 3) * 260
    card(mx, my, 830, 225, title, body, col, title_size=33, body_size=26,
         icon=icon_bell if title.startswith("Notification") else None)

# Assurance lab.
rounded_rect(2490, 1605, 2760, 1045, C["slate"], C["green"], radius=34, stroke_width=5)
add_text(2540, 1645, "ASSURANCE LAB — THE HEARTBEAT OF NLI", size=48, color=C["green"], font="NLI-Bold")
add_text(2540, 1710, "Uses the same production adapter as every managed client; results are explainable and approval-gated.",
         size=30, color=C["muted"], font="NLI-Regular")

assurance_steps = [
    ("A. Submit", "Paste/upload sample logs or use a retained encrypted fixture.", C["blue"]),
    ("B. Execute", "Run ParserPack stages; capture attempts, spans and extracted values.", C["cyan"]),
    ("C. Score", "Measure field coverage, validation, confidence and regressions.", C["violet"]),
    ("D. Candidate", "Create declarative ParserPack JSON; never executable code.", C["pink"]),
    ("E. Gate", "Draft → review → independent approval → publish.", C["amber"]),
    ("F. Activate", "Version, audit and distribute approved packs to clients.", C["green"]),
]
for i, (title, body, col) in enumerate(assurance_steps):
    ax = 2540 + (i % 3) * 885
    ay = 1800 + (i // 3) * 330
    card(ax, ay, 815, 270, title, body, col, title_size=35, body_size=27)
    if i % 3 < 2:
        arrow([(ax + 815, ay + 135), (ax + 865, ay + 135)], col, width=7)

# AI assist.
card(2490, 2700, 1340, 580, "AI API Key Integration",
     "Optional OpenAI, Anthropic, or custom OpenAI-compatible provider. The controller encrypts the API key server-side, sends masked failed samples, and accepts only declarative ParserDef JSON proposals. AI cannot publish or activate a parser.",
     C["pink"], tag="OPTIONAL", title_size=42, body_size=30, icon=icon_brain)
card(3910, 2700, 1340, 580, "Safety + Human Decision",
     "Schema validation and regression cases run first. Failed proposals enter quarantine. Passing proposals return to Assurance as Draft and still require a different reviewer, explicit approval, publication and rollout.",
     C["amber"], tag="NO AUTO-PUBLISH", title_size=42, body_size=29, icon=icon_shield)

card(2490, 3280, 2760, 150, "Embedded WebView",
     "Same-origin UI served by the controller for private administration.", C["violet"], tag="LIVE WORKSPACES",
     title_size=35, body_size=22, icon=icon_browser)


# Zone 3: experience and cloud.
card(5510, 655, 2000, 300, "Administrator / Operator Browser",
     "Authenticated live dashboards for time series, fleet, parser studios, Assurance, AI settings, users/RBAC, audit, notifications, exports and downloads.",
     C["green"], tag="HUMAN CONTROL", title_size=43, body_size=31, icon=icon_browser)

add_text(5510, 1015, "ACCESS CHANNELS", size=38, color=C["green"], font="NLI-Bold")
card(5510, 1075, 960, 340, "Private WebView",
     "Direct controller HTTPS or SSH tunnel. Corporate or generated private CA TLS; no separate sync script is required.",
     C["violet"], tag="LOCAL", title_size=39, body_size=29)
card(6550, 1075, 960, 340, "Vercel / Cloudflare Webapp",
     "Static UI + server proxy. NLI_CONTROLLER_ORIGIN uses a verified public HTTPS NGINX, Apache or tunnel edge; SSE reconnects and polling fallback preserve live state.",
     C["blue"], tag="PUBLIC HTTPS", title_size=36, body_size=27)

card(5510, 1480, 2000, 820, "Supabase — Protected Metadata Plane",
     "Stores bounded control metadata through protected RPCs, including drafts and approved revisions for parser assets. Private Storage is limited to encrypted Assurance fixtures, approved rollout artifacts, and opt-in licensed GeoIP candidates with source/version/hash/attribution metadata.\n\nClients enrich from validated local MMDB files; there is no per-event cloud lookup. Production raw logs, parsed events, credentials and API secrets are never mirrored.",
     C["green"], tag="LIVE METADATA", title_size=43, body_size=29, icon=icon_db)

card(5510, 2365, 2000, 415, "Live Sync + API Behavior",
     "Web drafts persist immediately. Independent approval publishes hash-addressed assets or bounded config patches. A single aggregate snapshot reduces API round trips; SSE reports live parser rates, timings, drift and errors.",
     C["cyan"], tag="REAL TIME", title_size=40, body_size=29)
card(5510, 2845, 2000, 490, "Security Invariants",
     "• One-use enrollment bundle per client\n• Verified server TLS or generated private CA\n• Server-side secrets, restrictive file modes\n• RBAC + audit + explicit approval gates\n• RLS + private Storage in Supabase\n• Atomic MMDB activation + rollback",
     C["amber"], tag="ZERO TRUST", title_size=41, body_size=29, icon=icon_shield)


# Cross-zone arrows and labels.
arrow([(2280, 1670), (2385, 1670), (2385, 780), (2440, 780)], C["violet"], width=12, dashed=True,
      label="REGISTER • HEARTBEAT • COMMANDS", label_xy=(3370, 590))
arrow([(2280, 3298), (2340, 3298), (2340, 1420), (2440, 1420)], C["cyan"], width=12,
      label="NORMALIZED EVENTS", label_xy=(2360, 2350))
arrow([(5300, 3380), (5380, 3380), (5380, 1260), (5460, 1260)], C["violet"], width=12, dashed=True,
      label="AUTHENTICATED API", label_xy=(5380, 2070))
arrow([(5460, 1830), (5375, 1830), (5375, 1920), (5300, 1920)], C["green"], width=12,
      label="PROTECTED RPC", label_xy=(5390, 1880))
arrow([(3830, 2990), (3870, 2990), (3870, 2335), (3910, 2335)], C["pink"], width=10, dashed=True,
      label="PROPOSAL", label_xy=(3870, 2640))
arrow([(4580, 2700), (4580, 2595)], C["amber"], width=10, label="VALIDATED DRAFT", label_xy=(4580, 2645))
arrow([(4720, 2460), (4720, 1450), (4270, 1450)], C["green"], width=10,
      label="PUBLISH", label_xy=(4740, 1510))
arrow([(4270, 1450), (4270, 1515), (2280, 1515)], C["green"], width=10, dashed=True,
      label="APPROVED PACK SYNC", label_xy=(3320, 1515))
arrow([(6510, 955), (6510, 1035)], C["amber"], width=10, label="SELECT CHANNEL", label_xy=(6510, 1000))
arrow([(6550, 1245), (6465, 1245)], C["violet"], width=8, dashed=True)


# Bottom lifecycle.
rounded_rect(120, 3570, 7440, 620, C["panel"], C["stroke"], radius=38, stroke_width=4)
add_text(170, 3610, "END-TO-END OPERATING FLOW", size=48, color=C["text"], font="NLI-Bold")
add_text(7510, 3625, "INSTALL → INGEST → PARSE → ASSURE → APPROVE → DISTRIBUTE → OBSERVE",
         size=29, color=C["muted"], font="NLI-Semibold", anchor="end")

lifecycle = [
    ("1", "Controller Setup", "Install RPM; choose private TLS; create CA, admin and Supabase bootstrap.", C["violet"]),
    ("2", "One-Use Bundle", "Generate root-only client bootstrap with controller URL, public CA and token.", C["amber"]),
    ("3", "Client Setup", "Install RPM; ask log paths + simultaneous outputs; auto-enroll and start services.", C["cyan"]),
    ("4", "Parse + Route", "Tail → detect → extract → normalize → validate → output or quarantine.", C["blue"]),
    ("5", "Live Operations", "Heartbeats, fleet health, dashboards, audit and notification bell.", C["green"]),
    ("6", "Assurance", "Replay samples; inspect fields, byte spans, attempts, coverage and confidence.", C["pink"]),
    ("7", "Approve", "Regression pass; independent review; approve and publish ParserPack.", C["amber"]),
    ("8", "Fleet Rollout", "Distribute verified revision; monitor; rollback safely when required.", C["green"]),
]
for i, (num, title, body, col) in enumerate(lifecycle):
    lx = 170 + i * 915
    ly = 3710
    d.add(Circle(lx + 45, HEIGHT - ly - 45, 45, fillColor=col, strokeColor=None))
    add_text(lx + 45, ly + 20, num, size=38, color=C["bg"], font="NLI-Bold", anchor="middle")
    add_text(lx + 105, ly + 2, title, size=34, color=C["text"], font="NLI-Semibold")
    paragraph(lx + 105, ly + 56, body, 770, size=25, leading=34, color=C["muted"], max_lines=4)
    if i < len(lifecycle) - 1:
        arrow([(lx + 835, ly + 45), (lx + 895, ly + 45)], col, width=7)

add_text(170, 4132, "NLI V14 principle: deterministic first, explainable always, AI-assisted only when needed, and human-approved before production.",
         size=31, color=C["cyan"], font="NLI-Semibold")
add_text(7510, 4132, "Architecture reference • Release v14.0.0-1 • revision recorded in release manifest",
         size=28, color=C["muted"], font="NLI-Regular", anchor="end")


renderSVG.drawToFile(d, str(SVG_OUT))

# SVG renderers do not know ReportLab's registered aliases. Replace them with
# the installed font family and explicit weights so browsers preserve the
# intended sans-serif hierarchy when exporting the 8K PNG.
svg = SVG_OUT.read_text(encoding="utf-8")
svg = svg.replace("font-family: NLI-Bold;", "font-family: 'Segoe UI'; font-weight: 700;")
svg = svg.replace("font-family: NLI-Semibold;", "font-family: 'Segoe UI'; font-weight: 600;")
svg = svg.replace("font-family: NLI-Regular;", "font-family: 'Segoe UI'; font-weight: 400;")
SVG_OUT.write_text(svg, encoding="utf-8")
print(SVG_OUT)

