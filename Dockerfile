FROM node:22-alpine as node-builder
# ENV NODE_ENV production
RUN mkdir -p /usr/src/app
RUN mkdir -p /usr/src/app-publish
RUN mkdir -p /usr/src/app-jwt
RUN corepack enable

WORKDIR /usr/src/app/
COPY ./frontend/package.json ./frontend/pnpm-lock.yaml ./
RUN pnpm install --frozen-lockfile
COPY ./frontend/. .
RUN pnpm build

WORKDIR /usr/src/app-publish/
COPY ./frontend-publish/package.json ./frontend-publish/pnpm-lock.yaml ./
RUN pnpm install --frozen-lockfile
COPY ./frontend-publish/. .
RUN pnpm build

WORKDIR /usr/src/app-jwt/
COPY ./frontend-jwt-publish/package.json ./frontend-jwt-publish/pnpm-lock.yaml ./
RUN pnpm install --frozen-lockfile
COPY ./frontend-jwt-publish/. .
RUN pnpm build

FROM golang:1.25-alpine3.22
RUN apk update
RUN apk add make
RUN mkdir /download
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . /app
RUN make

COPY --from=node-builder /usr/src/app/build/. ./frontend/build/.
COPY --from=node-builder /usr/src/app-publish/build/. ./frontend-publish/build/.
COPY --from=node-builder /usr/src/app-jwt/build/. ./frontend-jwt-publish/build/.

RUN ./scepserver-opt ca -init

CMD ["./scepserver-opt"]
