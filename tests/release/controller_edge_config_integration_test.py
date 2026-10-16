#!/usr/bin/env python3
"""Render installer proxy heredocs and validate them with native daemons."""

import pathlib
import re
import shutil
import subprocess
import sys
import tempfile

root = pathlib.Path(sys.argv[1] if len(sys.argv) > 1 else pathlib.Path(__file__).parents[2])
setup = (root / "scripts/setup-controller-rocky.sh").read_text(encoding="utf-8")


def last_heredoc(marker):
    start = setup.rindex(marker) + len(marker)
    end = setup.index("\nEOF", start)
    return setup[start:end]


def render(template, replacements):
    rendered = template.replace("\\$", "$")
    for key, value in replacements.items():
        rendered = rendered.replace("${" + key + "}", value).replace("$" + key, value)
    return rendered


def check(command, config, label):
    result = subprocess.run(command, text=True, capture_output=True)
    if result.returncode:
        raise SystemExit(f"{label} failed:\n{result.stdout}\n{result.stderr}\n--- rendered config ---\n{config}")


for binary in ("openssl", "nginx", "httpd"):
    if shutil.which(binary) is None:
        raise SystemExit(f"{binary} is required; run this test in the Rocky integration container")

nginx_template = last_heredoc('cat >"$nginx_site" <<EOF')
apache_template = last_heredoc('cat >"$temporary_vhost" <<EOF')
with tempfile.TemporaryDirectory(prefix="nli-edge-integration-") as temporary:
    work = pathlib.Path(temporary)
    cert = work / "server.crt"
    key = work / "server.key"
    subprocess.run(
        ["openssl", "req", "-x509", "-newkey", "rsa:2048", "-nodes", "-days", "1",
         "-keyout", str(key), "-out", str(cert), "-subj", "/CN=nli-edge-test.example",
         "-addext", "subjectAltName=DNS:nli-edge-test.example", "-addext", "extendedKeyUsage=serverAuth"],
        check=True, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL,
    )
    cert_paths = {"/etc/letsencrypt/live/nli-edge-test.example/fullchain.pem": str(cert),
                  "/etc/letsencrypt/live/nli-edge-test.example/privkey.pem": str(key)}

    nginx_conf = render(nginx_template, {"PORT": "12382", "endpoint": "nli-edge-test.example"})
    for old, new in cert_paths.items():
        nginx_conf = nginx_conf.replace(old, new)
    required_nginx = ("proxy_buffering off;", "proxy_request_buffering off;", "proxy_read_timeout 300s;",
                      "proxy_send_timeout 300s;", "proxy_set_header Authorization", "proxy_set_header X-Forwarded-For")
    if any(token not in nginx_conf for token in required_nginx):
        raise SystemExit("rendered Nginx config lacks required streaming or forwarded-header directives")
    nginx_file = work / "nli-controller.conf"
    nginx_file.write_text(nginx_conf, encoding="utf-8")
    nginx_main = work / "nginx.conf"
    nginx_main.write_text(
        f"worker_processes 1;\npid {work}/nginx.pid;\nerror_log stderr;\nevents {{ worker_connections 64; }}\n"
        f"http {{ include {nginx_file}; }}\n", encoding="utf-8")
    check(["nginx", "-t", "-c", str(nginx_main), "-p", str(work)], nginx_conf, "nginx -t")

    websocket_modern = render(apache_template, {"PORT": "12382", "endpoint": "nli-edge-test.example",
                                               "apache_websocket_rules": "", "apache_websocket_proxy":
                                               "ProxyPass / http://127.0.0.1:12382/ upgrade=websocket timeout=300"})
    websocket_legacy = render(apache_template, {"PORT": "12382", "endpoint": "nli-edge-test.example",
                                               "apache_websocket_rules": "  RewriteEngine On\n  RewriteCond %{HTTP:Upgrade} =websocket [NC]\n  RewriteCond %{HTTP:Connection} upgrade [NC]\n  RewriteRule ^/(.*)$ ws://127.0.0.1:12382/$1 [P,L]",
                                               "apache_websocket_proxy": ""})
    for index, apache_conf in enumerate((websocket_modern, websocket_legacy), start=1):
        for old, new in cert_paths.items():
            apache_conf = apache_conf.replace(old, new)
        if "ProxyTimeout 300" not in apache_conf or "ProxyPassReverse /" not in apache_conf:
            raise SystemExit("rendered Apache config lacks proxy timeout/reverse-proxy directives")
        if "CacheEnable" in apache_conf or "CacheDisable" in apache_conf:
            raise SystemExit("Apache SSE path must not enable response caching")
        apache_file = work / f"httpd-{index}.conf"
        apache_file.write_text(apache_conf, encoding="utf-8")
        apache_main = work / f"httpd-main-{index}.conf"
        apache_main.write_text(
            f'ServerRoot "/etc/httpd"\nIncludeOptional conf.modules.d/*.conf\n'
            f'Listen 80\nListen 443\nServerName localhost\nUser apache\nGroup apache\n'
            f'PidFile "{work}/httpd-{index}.pid"\nErrorLog "{work}/httpd-{index}.error.log"\n'
            f'Include "{apache_file}"\n', encoding="utf-8")
        check(["httpd", "-t", "-f", str(apache_main)], apache_conf, f"httpd -t ({'modern' if index == 1 else 'legacy'} websocket)")

print("controller edge rendered Nginx/Apache config integration tests: PASS")
