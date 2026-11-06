#!/usr/bin/env python3
"""Reference implementation of a "Type A" Sign-in-with-X site connector.

Type A = the site's OAuth callback sets its own session cookies directly on
GET. `agentx oauth approve` + `agentx oauth callback` finish the whole login —
no site login, no glue tool, just cookies from X.

Generic flow (endpoints differ per site; find them via the site's JS bundle):

    1. START        GET <site>/api/.../auth/x/start
                    -> 302 Location: x.com/i/oauth2/authorize?...  (the
                       authorize_url) AND a state cookie the callback must
                       echo back (e.g. fanszn_oauth={state,verifier})
    2. CONSENT      `agentx oauth approve <authorize_url>`
                    -> data.redirect_uri = the site's callback with the code
    3. CALLBACK     `agentx oauth callback <redirect_uri> --cookie <state jar>`
                    -> data.sessionCookies = the site's session
    4. VERIFY       GET the site's /me endpoint with the session cookie

Requires: `agentx` >= v0.2.1 on PATH with an X account stored
(`agentx account add`), plus `requests`.

Usage:
    python3 fanszn_connect_x.py            # full chain, verify /me
    python3 fanszn_connect_x.py --account NAME   # pick a stored X account
"""
import argparse
import json
import subprocess
import sys
from http.cookiejar import CookieJar

import requests

SITE = "https://fanszn.ipx.exchange"          # verified Type A site
START = "/api/fanszn/auth/x/start?returnTo=/fanszn/connect&ref=FAN-KQ4Z"
STATE_COOKIE = "fanszn_oauth"                  # site's state/verifier jar
ME_PATH = "/api/fanszn/me"                     # session verification endpoint

UA = ("Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 "
      "Chrome/126.0 Safari/537.36")


def start(authorize_out):
    """START (step 1) — capture the authorize_url AND the state cookie.

    Gotcha: the state cookie returned by the start endpoint MUST ride along
    on the callback, or the site rejects it with `state_mismatch`. The jar
    value is already URL-encoded by the server — pass it through verbatim.
    """
    s = requests.Session()
    s.headers.update({"User-Agent": UA, "Cookie": "fanszn_ref=FAN-KQ4Z"})
    r = s.get(SITE + START, allow_redirects=False, timeout=45)
    if r.status_code not in (301, 302, 303, 307):
        raise SystemExit(f"start did not redirect: {r.status_code} {r.text[:200]}")
    authorize_out["authorize_url"] = r.headers["Location"]
    return s.cookies


def state_cookie_value(jar):
    for c in jar:
        if c.name == STATE_COOKIE:
            return c.value
    raise SystemExit(f"no {STATE_COOKIE} cookie from start endpoint")


def agentx_approve(authorize_url, account=None):
    """CONSENT (step 2) — approve X's consent screen via agentx."""
    cmd = ["agentx", "oauth", "approve", authorize_url]
    if account:
        cmd += ["-a", account]
    r = subprocess.run(cmd, capture_output=True, text=True, timeout=120)
    env = json.loads(r.stdout)
    if not env.get("ok"):
        raise SystemExit(f"agentx approve failed: {env.get('error')}")
    return env["data"]["redirect_uri"].replace("&amp;", "&")


def agentx_callback(callback_url, state_cookie, account=None):
    """CALLBACK (step 3) — GET the site callback WITH the state cookie.

    agentx runs the GET in a cookie jar and returns every Set-Cookie the
    site issues on login in `data.sessionCookies`. `--cookie` is repeatable.
    """
    cmd = ["agentx", "oauth", "callback", callback_url,
           "--cookie", f"{STATE_COOKIE}={state_cookie}"]
    if account:
        cmd += ["-a", account]
    r = subprocess.run(cmd, capture_output=True, text=True, timeout=120)
    env = json.loads(r.stdout)
    if not env.get("ok"):
        raise SystemExit(f"agentx callback failed: {env.get('error')}")
    return env["data"]


def main():
    p = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    p.add_argument("--account", help="stored agentx account (-a NAME)")
    authorize_out = {}
    p.set_defaults(func=lambda a: None)
    a = p.parse_args()

    # 1) START — authorize_url + state cookie in one redirect
    jar = start(authorize_out)
    state = state_cookie_value(jar)
    print(f"[start] authorize_url={authorize_out['authorize_url'][:80]}...")

    # 2) CONSENT
    callback = agentx_approve(authorize_out["authorize_url"], a.account)
    print("[approve] got redirect_uri")

    # 3) CALLBACK — capture the site session
    data = agentx_callback(callback, state, a.account)
    cookies = data["sessionCookies"]
    print(f"[callback] status={data['status']} final={data['finalUrl']}")
    print(f"[callback] sessionCookies: {sorted(cookies.keys())}")
    session = next((v for k, v in cookies.items() if "session" in k.lower()), None)
    if not session:
        raise SystemExit(f"no session cookie captured: {list(cookies.keys())}")

    # 4) VERIFY — prove the login actually landed
    me = requests.get(SITE + ME_PATH,
                      headers={"User-Agent": UA, "Cookie": f"{next(k for k in cookies if 'session' in k.lower())}={session}"},
                      timeout=45).json()
    print(f"[verify] me: {json.dumps(me)[:300]}")


if __name__ == "__main__":
    main()
