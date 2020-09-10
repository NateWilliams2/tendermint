FROM alpine:3.12

RUN apk update && \
    apk --no-cache upgrade && \
    apk --no-cache add curl jq file

# The test runner will copy the Tendermint binary into /tendermint/tendermint, and the configuration
# into /tendermint/config/. This allows fast iteration when testing locally, without having to build
# a new Docker image.
VOLUME /tendermint
WORKDIR /tendermint
ENV TMHOME=/tendermint

EXPOSE 26656 26657
ENTRYPOINT ["/tendermint/tendermint"]
CMD ["node"]
STOPSIGNAL SIGTERM
