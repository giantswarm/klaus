FROM gsoci.azurecr.io/giantswarm/alpine:3.21

RUN apk --no-cache add ca-certificates

RUN mkdir -p /opt
COPY ./klaus /opt/klaus
RUN chmod +x /opt/klaus

ENTRYPOINT ["/opt/klaus"]
