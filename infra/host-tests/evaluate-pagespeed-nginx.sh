#!/usr/bin/env bash
# SPDX-License-Identifier: AGPL-3.0-or-later

set -euo pipefail

if [[ "$(id -u)" != 0 ]]; then
  printf 'Run this evaluation as root.\n' >&2
  exit 1
fi

for executable in /usr/sbin/nginx /usr/sbin/php-fpm8.4 /usr/bin/ab /usr/bin/curl /usr/sbin/vinyld; do
  if [[ ! -x "$executable" ]]; then
    printf 'Required evaluation executable is missing: %s\n' "$executable" >&2
    exit 1
  fi
done

if ! dpkg-query -W -f='${Version}' nginx-module-pagespeed 2>/dev/null | grep -Eq '^1\.15\.0-r[0-9]+~trixie$'; then
  printf 'The Debian 13 mod_pagespeed 1.15 evaluation package is not installed.\n' >&2
  exit 1
fi

evaluation_root=/var/lib/stackfort-pagespeed-evaluation
document_root="$evaluation_root/www"
cache_root="$evaluation_root/cyclone"
fastcgi_cache_root="$evaluation_root/fastcgi"
configuration=/etc/nginx/stackfort/global/90-pagespeed-evaluation.conf
php_service=php8.4-fpm.service
php_socket=/run/php/php8.4-fpm.sock
host=pagespeed-evaluation.stackfort.test
php_was_active=0

nginx_master="$(cat /run/nginx.pid 2>/dev/null || true)"
if [[ ! "$nginx_master" =~ ^[0-9]+$ ]] ||
  ! grep -q '/usr/lib/nginx/modules/ngx_pagespeed_module.so' "/proc/$nginx_master/maps" 2>/dev/null; then
  printf 'The running NGINX master has not loaded ngx_pagespeed_module.so; restart it before evaluation.\n' >&2
  exit 1
fi

if systemctl is-active --quiet "$php_service"; then
  php_was_active=1
else
  systemctl start "$php_service"
fi

cleanup() {
  rm -f -- "$configuration"
  rm -f -- /tmp/stackfort-pagespeed-evaluation.html /tmp/stackfort-pagespeed-direct.html
  if /usr/sbin/nginx -t -c /etc/nginx/stackfort/nginx.conf >/dev/null 2>&1; then
    systemctl reload nginx.service >/dev/null 2>&1 || true
  fi
  if [[ "$php_was_active" == 0 ]]; then
    systemctl stop "$php_service" >/dev/null 2>&1 || true
  fi
}
trap cleanup EXIT

install -d -o www-data -g www-data -m 0750 "$document_root" "$document_root/assets"
install -d -o www-data -g www-data -m 0750 "$cache_root" "$fastcgi_cache_root"

cat >"$document_root/index.php" <<'PHP'
<?php
header('Cache-Control: public, max-age=120');
header('Content-Type: text/html; charset=utf-8');
$host = htmlspecialchars($_SERVER['HTTP_HOST'], ENT_QUOTES | ENT_SUBSTITUTE, 'UTF-8');
$token = bin2hex(random_bytes(8));
?><!doctype html>
<html lang="en"><head><meta charset="utf-8"><title>Stackfort PageSpeed evaluation</title>
<link rel="stylesheet" href="https://<?= $host ?>/assets/base.css">
<link rel="stylesheet" href="https://<?= $host ?>/assets/theme.css">
</head><body><main><h1>Stackfort</h1><p><?= $token ?></p></main>
<script src="https://<?= $host ?>/assets/base.js"></script>
<script src="https://<?= $host ?>/assets/theme.js"></script></body></html>
PHP

cat >"$document_root/assets/base.css" <<'CSS'
/* deliberately verbose evaluation fixture */
html, body { margin: 0px; padding: 0px; }
body { color: #172033; background-color: #f6f8fb; font-family: system-ui, sans-serif; }
CSS
cat >"$document_root/assets/theme.css" <<'CSS'
main { width: min(70rem, 92vw); margin-left: auto; margin-right: auto; padding-top: 4rem; }
h1 { letter-spacing: -0.04em; font-size: 3rem; }
CSS
cat >"$document_root/assets/base.js" <<'JS'
window.stackfortEvaluation = window.stackfortEvaluation || {};
window.stackfortEvaluation.ready = function () { return true; };
JS
cat >"$document_root/assets/theme.js" <<'JS'
window.stackfortEvaluation.theme = function () { return "performance"; };
JS
chown -R www-data:www-data "$document_root"
chmod 0640 "$document_root/index.php" "$document_root"/assets/*

waf_directives() {
  local mode=$1
  case "$mode" in
    off) ;;
    detection_only)
      printf '%s\n' '    coraza on;' "    coraza_transaction_id \$request_id;" \
        '    coraza_rules_file "/etc/nginx/stackfort/coraza/profiles/detection-pl1.conf";'
      ;;
    blocking_pl1)
      printf '%s\n' '    coraza on;' "    coraza_transaction_id \$request_id;" \
        '    coraza_rules_file "/etc/nginx/stackfort/coraza/profiles/blocking-pl1.conf";'
      ;;
    *)
      printf 'Unsupported WAF mode: %s\n' "$mode" >&2
      exit 1
      ;;
  esac
}

write_configuration() {
  local mode=$1
  local waf
  waf="$(waf_directives "$mode")"
  cat >"$configuration" <<EOF
pagespeed off;
pagespeed UseNativeFetcher on;
pagespeed NumRewriteThreads 1;
pagespeed NumExpensiveRewriteThreads 1;
pagespeed FetchWithGzip off;
pagespeed FileCachePath $cache_root;
pagespeed FileCacheSizeKb 524288;
pagespeed CycloneRamCacheKb 0;
pagespeed CycloneZeroCopy on;
pagespeed FetcherTimeoutMs 500;
pagespeed RewriteDeadlinePerFlushMs 10;
pagespeed ImplicitCacheTtlMs 300000;
pagespeed HttpCacheCompressionLevel 6;
fastcgi_cache_path $fastcgi_cache_root levels=1:2 keys_zone=stackfort_pagespeed_eval:8m inactive=5m use_temp_path=off;

server {
    listen 127.0.0.1:9000;
    server_name $host;
    access_log off;
    root $document_root;
    index index.php;
    location / { try_files \$uri \$uri/ /index.php?\$query_string; }
    location ~ \.php\$ {
        try_files \$uri =404;
        include /etc/nginx/fastcgi_params;
        fastcgi_param SCRIPT_FILENAME \$document_root\$fastcgi_script_name;
        fastcgi_param HTTP_PROXY "";
        fastcgi_pass unix:$php_socket;
    }
}

server {
    listen 127.0.0.1:8101;
    server_name $host;
    access_log off;
    root $document_root;
    index index.php;
$waf
    location / { try_files \$uri \$uri/ /index.php?\$query_string; }
    location ~ \.php\$ {
        try_files \$uri =404;
        include /etc/nginx/fastcgi_params;
        fastcgi_param SCRIPT_FILENAME \$document_root\$fastcgi_script_name;
        fastcgi_param HTTP_PROXY "";
        fastcgi_pass unix:$php_socket;
        add_header X-Stackfort-Benchmark-WAF "$mode" always;
    }
}

server {
    listen 127.0.0.1:8102;
    server_name $host;
    access_log off;
$waf
    location / {
        proxy_http_version 1.1;
        proxy_set_header Connection "";
        proxy_set_header Host \$host;
        proxy_set_header Authorization \$http_authorization;
        proxy_set_header Cookie \$http_cookie;
        proxy_set_header X-Forwarded-Proto https;
        proxy_set_header X-Forwarded-For \$remote_addr;
        proxy_set_header X-Stackfort-Cache-Preset wordpress;
        proxy_pass http://127.0.0.1:6081;
        add_header X-Stackfort-Benchmark-WAF "$mode" always;
    }
}

server {
    listen 127.0.0.1:8103;
    server_name $host;
    access_log off;
    root $document_root;
    index index.php;
$waf
    location / { try_files \$uri \$uri/ /index.php?\$query_string; }
    location ~ \.php\$ {
        try_files \$uri =404;
        include /etc/nginx/fastcgi_params;
        fastcgi_param SCRIPT_FILENAME \$document_root\$fastcgi_script_name;
        fastcgi_param HTTP_PROXY "";
        fastcgi_pass unix:$php_socket;
        fastcgi_cache stackfort_pagespeed_eval;
        fastcgi_cache_key "\$scheme\$request_method\$host\$request_uri";
        fastcgi_cache_methods GET HEAD;
        fastcgi_cache_valid 200 2m;
        fastcgi_cache_bypass \$http_authorization \$http_cookie;
        fastcgi_no_cache \$http_authorization \$http_cookie \$upstream_http_set_cookie;
        add_header X-Stackfort-FastCGI-Cache \$upstream_cache_status always;
        add_header X-Stackfort-Benchmark-WAF "$mode" always;
    }
}

server {
    listen 127.0.0.1:8104;
    server_name $host;
    access_log off;
    root $document_root;
    index index.php;
$waf
    pagespeed on;
    pagespeed RewriteLevel CoreFilters;
    pagespeed FetchHttps disable;
    pagespeed MapOriginDomain "http://127.0.0.1:9000" "https://$host";
    pagespeed RespectXForwardedProto on;
    location / { try_files \$uri \$uri/ /index.php?\$query_string; }
    location ~ \.php\$ {
        try_files \$uri =404;
        include /etc/nginx/fastcgi_params;
        fastcgi_param SCRIPT_FILENAME \$document_root\$fastcgi_script_name;
        fastcgi_param HTTP_PROXY "";
        fastcgi_param HTTPS on;
        fastcgi_param HTTP_X_FORWARDED_PROTO https;
        fastcgi_pass unix:$php_socket;
        add_header X-Stackfort-Benchmark-WAF "$mode" always;
    }
}
EOF
}

measure() {
  local name=$1
  local port=$2
  local mode=$3
  local output
  output="$(ab -q -n 3000 -c 8 -H "Host: $host" "http://127.0.0.1:$port/index.php?benchmark=$name-$mode")"
  local rps p99
  rps="$(awk '/Requests per second:/ {print $4}' <<<"$output")"
  p99="$(awk '$1 == "99%" {print $2}' <<<"$output")"
  printf 'STACKFORT_PERFORMANCE {"name":"%s","wafMode":"%s","requests":3000,"concurrency":8,"requestsPerSecond":%s,"p99Milliseconds":%s}\n' \
    "$name" "$mode" "$rps" "$p99"
}

for mode in off detection_only blocking_pl1; do
  rm -rf -- "${cache_root:?}/"* "${fastcgi_cache_root:?}/"*
  write_configuration "$mode"
  /usr/sbin/nginx -t -c /etc/nginx/stackfort/nginx.conf
  systemctl reload nginx.service

  for port in 8101 8102 8103 8104; do
    consecutive=0
    for _ in {1..100}; do
      generation="$(/usr/bin/curl --fail --silent --dump-header - --output /dev/null \
        --header 'Connection: close' --header "Host: $host" \
        "http://127.0.0.1:$port/index.php?ready=$mode" | \
        awk -F': ' 'tolower($1) == "x-stackfort-benchmark-waf" {gsub("\\r", "", $2); print $2}' || true)"
      if [[ "$generation" == "$mode" ]]; then
        consecutive=$((consecutive + 1))
        if [[ "$consecutive" == 8 ]]; then break; fi
      else
        consecutive=0
      fi
      sleep 0.1
    done
    if [[ "$consecutive" != 8 ]]; then
      printf 'NGINX evaluation listener %s did not become ready in %s mode.\n' "$port" "$mode" >&2
      exit 1
    fi
  done

  for port in 8101 8102 8103 8104; do
    for _ in {1..12}; do
      /usr/bin/curl --fail --silent --show-error --header "Host: $host" \
        "http://127.0.0.1:$port/index.php?warm=$mode" >/dev/null
    done
  done

  headers="$(/usr/bin/curl --silent --dump-header - --output /tmp/stackfort-pagespeed-evaluation.html \
    --header "Host: $host" "http://127.0.0.1:8104/index.php?warm=$mode")"
  if ! grep -qi '^X-Page-Speed: 1\.15' <<<"$headers"; then
    printf 'PageSpeed response marker is absent in %s mode.\n' "$mode" >&2
    exit 1
  fi
  if ! grep -q '\.pagespeed\.' /tmp/stackfort-pagespeed-evaluation.html; then
    printf 'PageSpeed did not produce a rewritten resource URL in %s mode.\n' "$mode" >&2
    exit 1
  fi
  if [[ "$mode" == off ]]; then
    /usr/bin/curl --fail --silent --show-error --header "Host: $host" \
      "http://127.0.0.1:8101/index.php?transfer-shape=direct" \
      --output /tmp/stackfort-pagespeed-direct.html
    direct_bytes="$(wc -c < /tmp/stackfort-pagespeed-direct.html)"
    direct_requests=1
    for asset in "$document_root"/assets/*; do
      direct_bytes=$((direct_bytes + $(wc -c < "$asset")))
      direct_requests=$((direct_requests + 1))
    done
    optimized_bytes="$(wc -c < /tmp/stackfort-pagespeed-evaluation.html)"
    optimized_requests=1
    while IFS= read -r optimized_url; do
      optimized_path="${optimized_url#https://"$host"}"
      resource_bytes="$(/usr/bin/curl --fail --silent --show-error --header "Host: $host" \
        "http://127.0.0.1:8104$optimized_path" | wc -c)"
      optimized_bytes=$((optimized_bytes + resource_bytes))
      optimized_requests=$((optimized_requests + 1))
    done < <(grep -Eo "https://$host/[^\"']+" /tmp/stackfort-pagespeed-evaluation.html | sort -u)
    printf 'STACKFORT_PERFORMANCE {"name":"pagespeed-transfer-shape","wafMode":"off","directRequests":%s,"directBodyBytes":%s,"optimizedRequests":%s,"optimizedBodyBytes":%s}\n' \
      "$direct_requests" "$direct_bytes" "$optimized_requests" "$optimized_bytes"
  fi
  /usr/bin/curl --silent --output /dev/null --header "Host: $host" \
    "http://127.0.0.1:8102/index.php?cache-proof=$mode"
  vinyl_decision="$(/usr/bin/curl --silent --dump-header - --output /dev/null --header "Host: $host" \
    "http://127.0.0.1:8102/index.php?cache-proof=$mode" | \
    awk -F': ' 'tolower($1) == "x-stackfort-cache" {gsub("\\r", "", $2); print $2}')"
  /usr/bin/curl --silent --output /dev/null --header "Host: $host" \
    "http://127.0.0.1:8103/index.php?cache-proof=$mode"
  fastcgi_decision="$(/usr/bin/curl --silent --dump-header - --output /dev/null --header "Host: $host" \
    "http://127.0.0.1:8103/index.php?cache-proof=$mode" | \
    awk -F': ' 'tolower($1) == "x-stackfort-fastcgi-cache" {gsub("\\r", "", $2); print $2}')"
  if [[ "$vinyl_decision" != HIT || "$fastcgi_decision" != HIT ]]; then
    printf 'Warm cache decisions are Vinyl=%s and FastCGI=%s in %s mode.\n' \
      "$vinyl_decision" "$fastcgi_decision" "$mode" >&2
    printf '%s\n' '--- origin headers ---' >&2
    /usr/bin/curl --silent --show-error --head --header "Host: $host" \
      "http://127.0.0.1:9000/index.php?cache-proof=$mode" >&2
    printf '%s\n' '--- Vinyl headers ---' >&2
    /usr/bin/curl --silent --show-error --head --header "Host: $host" \
      "http://127.0.0.1:8102/index.php?cache-proof=$mode" >&2
    exit 1
  fi

  expected_attack=200
  if [[ "$mode" == blocking_pl1 ]]; then expected_attack=403; fi
  for port in 8101 8102 8103 8104; do
    actual_attack="$(/usr/bin/curl --silent --output /dev/null --write-out '%{http_code}' \
      --header "Host: $host" "http://127.0.0.1:$port/index.php?lookup=1%20OR%201=1")"
    if [[ "$actual_attack" != "$expected_attack" ]]; then
      printf 'WAF probe on port %s returned %s, expected %s in %s mode.\n' \
        "$port" "$actual_attack" "$expected_attack" "$mode" >&2
      exit 1
    fi
  done

  measure direct-php 8101 "$mode"
  measure vinyl-full-page 8102 "$mode"
  measure nginx-fastcgi-full-page 8103 "$mode"
  measure pagespeed-cyclone-resource-optimizer 8104 "$mode"
done

printf 'STACKFORT_QUALIFICATION pagespeed-approach-2-rewrite=passed\n'
printf 'STACKFORT_QUALIFICATION pagespeed-waf-order=passed\n'
