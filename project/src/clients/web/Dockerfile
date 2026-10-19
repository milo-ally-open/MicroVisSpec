FROM node:24.15.0-alpine3.23 AS web-build
WORKDIR /app
COPY package.json package-lock.json ./
RUN npm ci
COPY . ./
RUN npm run build

FROM nginx:1.31.7-alpine3.24
COPY nginx/nginx.conf /etc/nginx/nginx.conf
COPY nginx/snippets/ /etc/nginx/snippets/
COPY --from=web-build /app/dist /usr/share/nginx/html
