# check=skip=InvalidDefaultArgInFrom
# The backend, with ffmpeg and the sample tracks. The build context is the repository root.
#
# The file makes two images. The default is the server alone. `--target speech` adds espeak-ng, the voice of the AI
# presenter, and compose.ai.yaml builds it. It is a separate target, so a server without a presenter has no speech
# program.
ARG GO_ALPINE_IMAGE
ARG ALPINE_IMAGE

FROM ${GO_ALPINE_IMAGE} AS build

WORKDIR /src/apps/backend

COPY apps/backend/go.mod apps/backend/go.sum ./
RUN go mod download

COPY apps/backend/ ./

ARG VERSION=0.0.0-dev
RUN CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -X main.version=${VERSION}" -o /out/server ./cmd/server \
	&& CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o /out/loadgen ./cmd/loadgen

FROM ${ALPINE_IMAGE} AS base

ARG VERSION=0.0.0-dev
ARG SOURCE_URL=https://github.com/volodymyr-pavlov/go-webrtc-audio

LABEL org.opencontainers.image.title="go-webrtc-audio-backend" \
	org.opencontainers.image.version="${VERSION}" \
	org.opencontainers.image.source="${SOURCE_URL}"

RUN apk add --no-cache ffmpeg

WORKDIR /app

COPY --from=build /out/server /out/loadgen /usr/local/bin/
COPY scripts/samples.sh scripts/samples.sh
COPY audio audio
RUN sh scripts/samples.sh

EXPOSE 8080/tcp 5005/udp

ENTRYPOINT ["server"]
CMD ["-http", ":8080", "-audio", "/app/audio", "-ice-port", "5005", "-batch", "64"]

# The server with a voice for the AI presenter.
FROM base AS speech

RUN apk add --no-cache espeak-ng

USER nobody

# The default target: the server alone.
FROM base AS runtime

USER nobody
