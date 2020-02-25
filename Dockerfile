FROM centurylink/ca-certs
WORKDIR /app
COPY ./wfs-ls /app
COPY ./icons /app/icons

CMD ["/app/wfs-ls"]