"""Read the text of scanned PDF pages for Yggdrasil's Mimir.

Usage: python ocr_pdf.py config.json

config.json: {"pdf": path, "pages": [1, 3, ...] (empty for all), "dpi": 200}

Writes one JSON object per line on stdout:
  {"event": "page", "page": 3, "text": "...", "done": 1, "total": 2}
  {"event": "done"}
Logs and errors go to stderr.
"""

import json
import logging
import sys


def emit(**fields):
    print(json.dumps(fields), flush=True)


def page_text(result):
    """Join recognized boxes into lines in reading order.

    RapidOCR returns one box per run of text, so a table row can come back as
    several boxes. Boxes whose vertical centers are close are one line,
    ordered left to right.
    """
    if not result.txts:
        return ""
    items = []
    for box, text in zip(result.boxes, result.txts):
        ys = [p[1] for p in box]
        xs = [p[0] for p in box]
        items.append((sum(ys) / len(ys), min(xs), max(ys) - min(ys), text))
    items.sort()
    lines, current, last_y, last_h = [], [], None, 0
    for y, x, h, text in items:
        if last_y is not None and abs(y - last_y) > max(h, last_h) * 0.5:
            lines.append(current)
            current = []
        current.append((x, text))
        last_y, last_h = y, h
    if current:
        lines.append(current)
    return "\n".join(" ".join(t for _, t in sorted(line)) for line in lines)


def main():
    with open(sys.argv[1]) as f:
        cfg = json.load(f)
    logging.getLogger("RapidOCR").setLevel(logging.WARNING)

    import pypdfium2 as pdfium
    from rapidocr import RapidOCR

    engine = RapidOCR()
    pdf = pdfium.PdfDocument(cfg["pdf"])
    pages = cfg.get("pages") or list(range(1, len(pdf) + 1))
    scale = cfg.get("dpi", 200) / 72
    for n, number in enumerate(pages):
        page = pdf[number - 1]
        width, height = page.get_size()
        # Large pages render smaller, so memory stays bounded.
        s = min(scale, 4000 / max(width, height, 1))
        image = page.render(scale=s).to_numpy()
        emit(event="page", page=number, text=page_text(engine(image)), done=n + 1, total=len(pages))
    emit(event="done")


if __name__ == "__main__":
    main()
