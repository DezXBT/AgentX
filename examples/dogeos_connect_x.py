#!/usr/bin/env python3
"""Reference implementation of a "Type B" Sign-in-with-X site connector.

Type B = the target site has its own primary login (here: SIWE wallet), and
the X OAuth callback is bound to a server-side session created by an
`initiate` endpoint. `agentx oauth approve` alone gets the X code but cannot
finish the login — you need this thin glue tool.

Generic flow (works for any Type B site, endpoints differ per site):

    1. PRIMARY LOGIN    get a bearer token from the site's own auth flow
    2. INITIATE         POST <site>/.../initiate  -> {sessionId, launchUrl}
    3. CONSENT          `agentx oauth approve <launchUrl>` -> callback URL w/ code
    4. CALLBACK         GET the callback (the site's backend validates it)
    5. POLL             GET the site's connection status until connected

The concrete endpoints below are from quest.dogeos.com (verified live) —
swap them for your target site's equivalents (find them in its JS bundle).
Requires: `agentx` >= v0.2.1 on PATH with an X account stored
(`agentx account add`), plus `requests` and `eth_account`.

Usage:
    export PRIVKEY=0x...                      # wallet private key
    python3 dogeos_connect_x.py status  --wallet 0x...
    python3 dogeos_connect_x.py connect --wallet 0x...
    python3 dogeos_connect_x.py connect --token <bearer jwt>   # skip step 1
"""
import argparse
import json
import os
import subprocess
import sys
import time

import requests

API = "https://quest-api.dogeos.com"  # DogeOS backend; replace per site
UA = ("Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 "
      "Chrome/126.0 Safari/537.36")
PROVIDER = "twitter"  # site enum: twitter (NOT "x"), discord, telegram, ...

S = requests.Session()
S.headers.update({"User-Agent": UA, "Origin": "https://quest.dogeos.com",
                  "Referer": "https://quest.dogeos.com/",
                  "Content-Type": "application/json"})


def api(method, path, token=None, **kw):
    """Authenticated JSON request against the site backend."""
    headers = {"Authorization": f"Bearer {token}"} if token else {}
    r = S.request(method, API + path, headers=headers, timeout=45, **kw)
    try:
        return r.status_code, r.json()
    except Exception:
        return r.status_code, {"raw": r.text[:400]}


def site_login(address, private_key):
    """PRIMARY LOGIN (step 1) — SIWE wallet challenge/sign/session.

    Gotchas found by reversing the site's JS bundle:
      - the signed field is `signedMessage`, NOT `signature` (422 otherwise)
      - the token is `data.jwt`
    """
    from eth_account import Account
    from eth_account.messages import encode_defunct

    st, body = api("POST", "/v2/auth/wallet/challenges", json={"wallet": address})
    if st != 200:
        raise SystemExit(f"challenge failed: {st} {body}")
    d = body["data"]
    sig = Account.sign_message(encode_defunct(text=d["message"]),
                               private_key).signature.hex()
    if not sig.startswith("0x"):
        sig = "0x" + sig
    st, body = api("POST", "/v2/auth/wallet/sessions",
                   json={"wallet": address, "nonce": d["nonce"],
                         "signedMessage": sig})
    if st != 200:
        raise SystemExit(f"session failed: {st} {body}")
    tok = body["data"].get("jwt")
    if not tok:
        raise SystemExit(f"no jwt in response: {json.dumps(body)[:300]}")
    return tok


def connection_status(token):
    """Current state of the X connection on the site."""
    st, body = api("GET", "/v2/me/social-connections", token=token)
    conns = body.get("data", {}).get("connections", [])
    for c in conns:
        if c.get("provider") == PROVIDER:
            return c
    return {}


def connect(token, wait_secs=90):
    """Full Type B chain (steps 2-5)."""
    # A stuck `pending` session blocks a new initiate (launchUrl comes back
    # null until it expires) — clear it first if present.
    if connection_status(token).get("status") == "pending":
        api("DELETE", f"/v2/me/social-connections/{PROVIDER}", token=token)

    # 2) INITIATE — the server pairs sessionId with the X authorize state
    st, body = api("POST", f"/v2/me/social-connections/{PROVIDER}/initiate",
                   token=token,
                   json={"returnUrl": "https://quest.dogeos.com/oauth-callback"})
    if st != 200:
        raise SystemExit(f"initiate failed: {st} {json.dumps(body)[:300]}")
    launch = body.get("data", {}).get("launchUrl")
    if not launch:
        raise SystemExit(f"launchUrl null (stuck pending session?): {json.dumps(body)[:400]}")

    # 3) CONSENT — agentx approves X's consent screen, no browser needed
    r = subprocess.run(["agentx", "oauth", "approve", launch],
                       capture_output=True, text=True, timeout=120)
    env = json.loads(r.stdout)
    if not env.get("ok"):
        raise SystemExit(f"agentx approve failed: {env.get('error')}")
    callback = env["data"]["redirect_uri"].replace("&amp;", "&")
    print(f"[approve] scopes={env['data'].get('scopes')}")

    # 4) CALLBACK — the SITE'S BACKEND validates the state/code pair. Plain
    #    GET is enough; do NOT call the site's /confirm endpoint (DogeOS
    #    returns 400 confirm_not_supported — the backend does it itself).
    out = subprocess.run(["curl", "-s", "-L", "-A", UA, "-o", "/dev/null",
                          "-w", "%{http_code} %{url_effective}", callback],
                         capture_output=True, text=True, timeout=60)
    print(f"[callback] {out.stdout}")

    # 5) POLL — the backend resolves asynchronously (30-60s is normal).
    #    "pending" here is SUCCESS-IN-PROGRESS, not a failure: just keep
    #    polling, do not re-initiate.
    deadline = time.time() + wait_secs
    while time.time() < deadline:
        time.sleep(5)
        cur = connection_status(token)
        s = cur.get("status")
        if s == "connected":
            acct = (cur.get("account") or {}).get("username")
            print(f"[verify] {PROVIDER}: connected -> @{acct}")
            return cur
        if s != "pending":
            print(f"[verify] status={s} lastError={cur.get('lastError')}")
            return cur
    print("[verify] timeout — still pending")
    return connection_status(token)


def main():
    p = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    p.add_argument("cmd", choices=["status", "connect"])
    p.add_argument("--wallet", help="wallet address (0x..)")
    p.add_argument("--key", help="wallet private key hex, or env PRIVKEY")
    p.add_argument("--token", help="existing bearer token (skip primary login)")
    p.add_argument("--wait", type=int, default=90)
    a = p.parse_args()

    tok = a.token
    if not tok:
        key = a.key or os.environ.get("PRIVKEY")
        if not (a.wallet and key):
            raise SystemExit("need --wallet + --key (or env PRIVKEY), or --token")
        tok = site_login(a.wallet, key)
        print("[auth] site session OK")

    if a.cmd == "status":
        print(json.dumps(connection_status(tok), indent=2)[:1200])
        return

    if connect(tok, a.wait).get("status") != "connected":
        sys.exit(1)


if __name__ == "__main__":
    main()
