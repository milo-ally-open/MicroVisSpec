#!/bin/sh
set -eu

port="${WEB_HTTPS_REDIRECT_PORT:-443}"
case "$port" in
  ''|*[!0-9]*)
    echo "WEB_HTTPS_REDIRECT_PORT must be a numeric port" >&2
    exit 1
    ;;
esac

sed "s/__WEB_HTTPS_PORT__/${port}/g" /etc/nginx/nginx.conf.template > /tmp/nginx.conf
