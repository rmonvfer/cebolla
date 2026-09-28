FROM alpine:3.22
RUN apk add --no-cache tor
COPY torrc /etc/tor/torrc
USER tor
RUN tor --verify-config -f /etc/tor/torrc
ENTRYPOINT ["tor", "-f", "/etc/tor/torrc"]
