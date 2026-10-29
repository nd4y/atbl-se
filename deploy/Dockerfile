FROM scratch
COPY --chown=65532:65532 autoblow-server /autoblow-server
COPY ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
USER 65532:65532
EXPOSE 20443 20880 2053/udp 2053/tcp
ENTRYPOINT ["/autoblow-server", "--config", "/config/server.json"]
