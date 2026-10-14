# SPDX-License-Identifier: AGPL-3.0-only
FROM python:3.12-alpine

LABEL org.opencontainers.image.title="openwebui-tools seafile-bridge"
LABEL org.opencontainers.image.source="https://github.com/ananthb/openwebui-tools"
LABEL org.opencontainers.image.licenses="AGPL-3.0-only"

WORKDIR /app
COPY bridge.py /app/bridge.py

# Required at runtime: SEAHUB, ADMIN_EMAIL, ADMIN_PASSWORD (or ADMIN_TOKEN), BRIDGE_SECRET.
ENV HOST=0.0.0.0 PORT=3090
EXPOSE 3090

HEALTHCHECK --interval=30s --timeout=3s --start-period=5s \
  CMD python3 -c "import urllib.request; urllib.request.urlopen('http://127.0.0.1:3090/health')" || exit 1

ENTRYPOINT ["python3", "/app/bridge.py"]
