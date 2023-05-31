FROM alpine
RUN apk add --no-cache tini
COPY warpdl /bin/warpdl
ENTRYPOINT ["/sbin/tini", "--", "/bin/warpdl"]

LABEL org.opencontainers.image.authors="Divanshu Chauhan <divkix@divkix.me>"
LABEL org.opencontainers.image.url="https://warpdl.org"
LABEL org.opencontainers.image.source="https://github.com/warpdl/warp-releases"
LABEL org.opencontainers.image.title="Warpdl"
LABEL org.opencontainers.image.description="Official Warpdl Docker Image"
LABEL org.opencontainers.image.vendor="Warpdl"
