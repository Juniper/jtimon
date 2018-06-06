# Copyright (c) 2018, Juniper Networks, Inc.
# All rights reserved.

FROM alpine:3.7
RUN apk add --no-cache ethtool
RUN apk add --no-cache --virtual build-dependencies git go musl-dev \
  && mkdir -p /root/go/src/github.com/Juniper \
  && cd /root/go/src/github.com/Juniper \
  && git clone https://github.com/Juniper/jtimon.git \
  && cd jtimon && go build && strip jtimon && mv jtimon /usr/local/bin \
  && cd / \
  && rm -fr /root/go \
  && apk del build-dependencies

VOLUME /u
WORKDIR /u
ENTRYPOINT ["/usr/local/bin/jtimon"]
