# SPDX-License-Identifier: AGPL-3.0-only
FROM node:22-alpine
WORKDIR /app
COPY package.json package-lock.json ./
RUN npm ci --omit=dev
COPY index.mjs ./
ENV ACTUAL_DATA_DIR=/data PORT=8091
VOLUME ["/data"]
EXPOSE 8091
HEALTHCHECK CMD wget -qO- http://127.0.0.1:8091/health >/dev/null || exit 1
CMD ["node", "index.mjs"]
