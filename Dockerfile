FROM centurylink/ca-certs
WORKDIR /app
ADD docker/ca-certificates.crt /etc/ssl/certs/
COPY ./wfs-ls /app
COPY ./icons /app/icons

CMD ["/app/wfs-ls"]