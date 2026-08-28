#!/usr/bin/env python3
"""Render terminal frames from captured CLI output for portfolio media."""

import json
import hashlib
from pathlib import Path
from PIL import Image, ImageDraw, ImageFont

# Terminal dimensions
WIDTH = 1280
HEIGHT = 720
PADDING = 40
LINE_HEIGHT = 28

# Dark theme colors
BG_COLOR = (30, 30, 46)  # Dark navy
TEXT_COLOR = (205, 214, 244)  # Light text
PROMPT_COLOR = (137, 180, 250)  # Blue prompt
COMMENT_COLOR = (108, 112, 134)  # Gray comments
SUCCESS_COLOR = (166, 227, 161)  # Green success
ERROR_COLOR = (243, 139, 168)  # Red error
HEADER_COLOR = (203, 166, 247)  # Purple headers

def get_font(size=16):
    """Get monospace font with fallback."""
    font_candidates = [
        "/System/Library/Fonts/Menlo.ttc",
        "/Library/Fonts/MonoLisa.ttf",
        "/System/Library/Fonts/Monaco.dfont",
        "/usr/share/fonts/truetype/dejavu/DejaVuSansMono.ttf",
    ]
    for candidate in font_candidates:
        if Path(candidate).exists():
            try:
                return ImageFont.truetype(candidate, size)
            except Exception:
                continue
    return ImageFont.load_default()

def sanitize_path(text):
    """Replace absolute paths with neutral placeholders."""
    return text.replace("/Users/openclaw/Code/eth-rpc-monitor", "<repo>")

def render_frame(title, lines, output_path):
    """Render a single terminal frame."""
    img = Image.new("RGB", (WIDTH, HEIGHT), BG_COLOR)
    draw = ImageDraw.Draw(img)
    font = get_font(16)
    font_small = get_font(14)
    
    # Title bar
    draw.rectangle([(0, 0), (WIDTH, 60)], fill=(17, 17, 27))
    draw.text((PADDING, 20), title, fill=TEXT_COLOR, font=font_small)
    
    # Terminal content
    y = 80
    for line in lines:
        if y > HEIGHT - PADDING:
            break
        
        # Colorize based on content
        color = TEXT_COLOR
        if line.startswith("$"):
            color = PROMPT_COLOR
        elif line.startswith("#"):
            color = COMMENT_COLOR
        elif "✓" in line or "ok" in line.lower():
            color = SUCCESS_COLOR
        elif "ERROR" in line or "error" in line.lower():
            color = ERROR_COLOR
        elif line.startswith("═") or line.startswith("─"):
            color = HEADER_COLOR
        
        draw.text((PADDING, y), line, fill=color, font=font)
        y += LINE_HEIGHT
    
    img.save(output_path, "PNG")
    return output_path

def compute_sha256(file_path):
    """Compute SHA-256 hash of a file."""
    h = hashlib.sha256()
    with open(file_path, "rb") as f:
        for chunk in iter(lambda: f.read(8192), b""):
            h.update(chunk)
    return h.hexdigest()

def main():
    output_dir = Path("public/screenshots")
    output_dir.mkdir(parents=True, exist_ok=True)
    
    # Frame 1: Usage
    usage_lines = [
        "$ ./bin/ethrpc",
        "",
        "eth-rpc-monitor measures Ethereum JSON-RPC over raw HTTP: block inspection,",
        "tail-latency health checks, cross-provider snapshots, and a live monitoring dashboard.",
        "",
        "Configure providers in config/providers.yaml (see config/providers.yaml.example).",
        "",
        "Usage:",
        "  ethrpc [command]",
        "",
        "Available Commands:",
        "  block       Inspect a single block from one provider",
        "  completion  Generate the autocompletion script for the specified shell",
        "  help        Help about any command",
        "  monitor     Live dashboard — refresh provider block heights and latency",
        "  snapshot    Compare the same block across all providers (fork detection)",
        "  test        Health check — latency samples and tail percentiles per provider",
        "",
        "Flags:",
        "  -c, --config string   Config file path (default \"config/providers.yaml\")",
        "  -h, --help            help for ethrpc",
        "",
        "Use \"ethrpc [command] --help\" for more information about a command.",
    ]
    render_frame("ethrpc — usage", usage_lines, output_dir / "01-usage.png")
    
    # Frame 2: Block inspect
    block_lines = [
        "$ ./bin/ethrpc block latest",
        "",
        "Auto-selected: publicnode",
        "",
        "",
        "Block #25,856,916",
        "══════════════════════════════════════════════════",
        "  Hash:     0xaad98265bb8cdd77c53c51129ccddb9f5fa29292800d1d16ff13e36c1d24d6b7",
        "  Parent:   0x229144d33ff3fa1b08ecac18184f0e1f888f491749ec734b8c675a849ca15f12",
        "  Timestamp: 2026-08-28 23:06:23 UTC (14s ago)",
        "  Gas:      20,424,935 / 59,941,408 (34.1%)",
        "  Base Fee: 0.04 gwei",
        "  Transactions: 233",
        "",
        "  Provider: publicnode (71ms)",
    ]
    render_frame("ethrpc block latest", block_lines, output_dir / "02-block-inspect.png")
    
    # Frame 3: Health check
    health_lines = [
        "$ ./bin/ethrpc test --samples 5",
        "",
        "Testing 2 providers with 5 samples each...",
        "",
        "",
        "[publicnode] Testing with 5 samples...",
        "",
        "[llamanodes] Testing with 5 samples...",
        "  publicnode 1/5: 52ms",
        "  llamanodes 1/5: ERROR - rpc http status 521: error code: 521",
        "  publicnode 2/5: 45ms",
        "  llamanodes 2/5: ERROR - rpc http status 521: error code: 521",
        "  publicnode 3/5: 42ms",
        "  publicnode 4/5: 44ms",
        "  llamanodes 3/5: ERROR - rpc http status 521: error code: 521",
        "  publicnode 5/5: 44ms",
        "[publicnode] Calculated percentiles:",
        "  P50: 44ms, P95: 52ms, P99: 52ms, Max: 52ms",
        "  llamanodes 4/5: ERROR - rpc http status 521: error code: 521",
        "  llamanodes 5/5: ERROR - rpc http status 521: error code: 521",
        "[llamanodes] Calculated percentiles:",
        "  P50: 0ms, P95: 0ms, P99: 0ms, Max: 0ms",
        "Provider       Type   Success P50  P95  P99  Max   Block",
        "──────────────────────────────────────────────────────────────────────────────────────────",
        "llamanodes     public 0%    0ms  0ms  0ms  0ms       0",
        "publicnode     public 100%  44ms  52ms  52ms  52ms 25856917",
    ]
    render_frame("ethrpc test --samples 5", health_lines, output_dir / "03-health-check.png")
    
    # Frame 4: Snapshot
    snapshot_lines = [
        "$ ./bin/ethrpc snapshot latest",
        "",
        "Fetching block latest from 2 providers...",
        "",
        "Provider       Latency        Block Height   Block Hash",
        "──────────────────────────────────────────────────────────────────────────────────────────",
        "llamanodes     —            —            ERROR: rpc http status 521: error code: 521",
        "publicnode     69ms               25856917   0x6d79e8f2446c1c0509341671e65f5a39f5b62c226c46b604ca60fce4b2c8e6d5",
        "",
        "✓ All providers agree on block hash",
    ]
    render_frame("ethrpc snapshot latest", snapshot_lines, output_dir / "04-snapshot.png")
    
    # Frame 5: JSON report
    json_lines = [
        "$ ./bin/ethrpc block latest --json",
        "",
        "Auto-selected: publicnode",
        "",
        "JSON report written to: reports/block-20260828-190640.json",
        "",
        "# Report structure:",
        "{",
        '  "number": 25856917,',
        '  "hash": "0x6d79e8f2446c1c0509341671e65f5a39f5b62c226c46b604ca60fce4b2c8e6d5",',
        '  "parentHash": "0xaad98265bb8cdd77c53c51129ccddb9f5fa29292800d1d16ff13e36c1d24d6b7",',
        '  "timestamp": "2026-08-28T23:06:35Z",',
        '  "gasUsed": 26121400,',
        '  "gasLimit": 59999943,',
        '  "baseFeePerGas": 0.042847831,',
        '  "transactions": [',
        '    "0x87fe968487e8fded9c32dca682bbffb6bd38a09d4e01153df89eb4225f067b96",',
        '    "0x5dfa26410faa031f6f160b79063a162f318fd5c67f88423c3e3daa1d2c661533",',
        "    ...",
        "  ]",
        "}",
    ]
    render_frame("ethrpc block latest --json", json_lines, output_dir / "05-json-report.png")
    
    # Frame 6: Block help
    help_lines = [
        "$ ./bin/ethrpc block --help",
        "",
        "Fetch and display a single Ethereum block. With no --provider, all providers",
        "are raced via eth_blockNumber and the fastest one on the highest head is auto-selected.",
        "",
        "The block argument accepts a decimal number, hex (0x...), or a tag (latest, pending,",
        "earliest). Defaults to \"latest\".",
        "",
        "Usage:",
        "  ethrpc block [block] [flags]",
        "",
        "Flags:",
        "  -h, --help              help for block",
        "  -j, --json              Output JSON report to reports directory",
        "      --provider string   Use specific provider (empty = auto-select fastest)",
        "",
        "Global Flags:",
        "  -c, --config string   Config file path (default \"config/providers.yaml\")",
    ]
    render_frame("ethrpc block --help", help_lines, output_dir / "06-block-help.png")
    
    # Compute hashes
    screenshots = sorted(output_dir.glob("*.png"))
    hashes = {}
    for ss in screenshots:
        hashes[ss.name] = compute_sha256(ss)
    
    # Build transcript
    transcript = "\n".join([
        "./bin/ethrpc",
        "./bin/ethrpc block latest",
        "./bin/ethrpc test --samples 5",
        "./bin/ethrpc snapshot latest",
        "./bin/ethrpc block latest --json",
        "./bin/ethrpc block --help",
    ])
    transcript_hash = hashlib.sha256(transcript.encode()).hexdigest()
    
    # Write manifest
    manifest = {
        "project": "eth-rpc-monitor",
        "captureMethod": "terminal-replay",
        "provenance": "Deterministic pseudo-terminal frames rendered from actual CLI stdout/stderr. No live desktop recording.",
        "commands": [
            "./bin/ethrpc",
            "./bin/ethrpc block latest",
            "./bin/ethrpc test --samples 5",
            "./bin/ethrpc snapshot latest",
            "./bin/ethrpc block latest --json",
            "./bin/ethrpc block --help",
        ],
        "screenshots": [
            {"file": name, "artifactSha256": h}
            for name, h in sorted(hashes.items())
        ],
        "transcriptSha256": transcript_hash,
    }
    
    manifest_path = Path("public/media.json")
    with open(manifest_path, "w") as f:
        json.dump(manifest, f, indent=2)
        f.write("\n")
    
    print(f"Rendered {len(screenshots)} screenshots to {output_dir}")
    print(f"Manifest written to {manifest_path}")
    for name, h in sorted(hashes.items()):
        print(f"  {name}: {h}")

if __name__ == "__main__":
    main()
