#!/usr/bin/env python3
"""Run PPanel checkout against the upstream GMPay cashier and callback worker."""
import os
from pathlib import Path
import shutil
import subprocess
import tempfile

ROOT = Path(__file__).resolve().parents[2]
REV = "3506640084f9a6e911e37f7e9b6bf61f14723b33"
if not os.environ.get("PPANEL_LTS_TEST_POSTGRES_DSN"):
    raise SystemExit("PPANEL_LTS_TEST_POSTGRES_DSN must allow creation of isolated test databases")
with tempfile.TemporaryDirectory(prefix="ppanel-gmpay-accept-") as tmp:
    peer = Path(tmp, "epusdt")
    binary = Path(tmp, "merchant.test")
    subprocess.run(["go", "test", "-c", "-o", binary, "./internal/app"], cwd=ROOT, check=True)
    subprocess.run(["git", "init", "-q", peer], check=True)
    subprocess.run(["git", "fetch", "--quiet", "--depth=1", "https://github.com/GMWalletApp/epusdt.git", REV], cwd=peer, check=True)
    subprocess.run(["git", "checkout", "--quiet", "--detach", "FETCH_HEAD"], cwd=peer, check=True)
    shutil.copyfile(ROOT / "scripts/acceptance/testdata/gmpay_peer_test.go.txt", peer / "src/mq/ppanel_lts_test.go")
    subprocess.run(
        ["go", "test", "-count=1", "-run", "^TestPPanelMerchantInterop$", "-v", "./mq"],
        cwd=peer / "src", env={**os.environ, "PPANEL_ACCEPT_BIN": str(binary)}, check=True,
    )
